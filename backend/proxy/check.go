package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/ssh"
	"scanssh-ray/backend/config"
	sshclient "scanssh-ray/backend/ssh"
)

type Result struct {
	OK       bool
	HTTPCode int
	Message  string
}

func quote(value string) string {
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\r", "\\r", "\n", "\\n", "\t", "\\t")
	return "\"" + r.Replace(value) + "\""
}

func EncodedPowerShell(script string) string {
	encoded := utf16.Encode([]rune(script))
	raw := make([]byte, len(encoded)*2)
	for i, n := range encoded {
		raw[2*i] = byte(n)
		raw[2*i+1] = byte(n >> 8)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(raw)
}

func CurlConfig(c *config.Config) string {
	text := "silent\nshow-error\nproxy = " + quote(fmt.Sprintf("http://%s:%d", c.ProxyHost, c.ProxyPort)) + "\nnoproxy = \"\"\nconnect-timeout = " + strconv.Itoa(int(c.ConnectTimeout.Seconds())) + "\nmax-time = " + strconv.Itoa(int(c.CommandTimeout.Seconds())) + "\noutput = \"NUL\"\nwrite-out = \"SCANSSH:%{http_code}\"\nurl = " + quote(c.TestURL) + "\n"
	if c.ProxyAuth == "basic" {
		text += "proxy-basic\nproxy-user = " + quote(c.ProxyUser+":"+c.ProxyPassword) + "\n"
	}
	return text
}

var httpCode = regexp.MustCompile(`SCANSSH:(\d{3})`)

const checkScript = `$ErrorActionPreference='Stop'; $OutputEncoding=New-Object System.Text.UTF8Encoding($false); [Console]::OutputEncoding=$OutputEncoding; try { $cfg=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd())); $cfg | & curl.exe --disable --config -; exit $LASTEXITCODE } catch { exit 99 }`

func Check(ctx context.Context, client *ssh.Client, c *config.Config) Result {
	if c.ProxyAuth == "basic" && (c.ProxyUser == "" || c.ProxyPassword == "") {
		return Result{Message: "Заполните PROXY_USER и PROXY_PASSWORD в .env"}
	}
	// PowerShell reads base64 from standard input; curl reads its configuration from a pipe.
	// Credentials are absent from process arguments, logs and on-disk remote files.
	output, err := sshclient.Run(ctx, client, EncodedPowerShell(checkScript), base64.StdEncoding.EncodeToString([]byte(CurlConfig(c))))
	match := httpCode.FindStringSubmatch(output)
	code := 0
	if len(match) == 2 {
		code, _ = strconv.Atoi(match[1])
	}
	if err == nil && code >= 200 && code < 300 {
		return Result{OK: true, HTTPCode: code, Message: "HTTPS через прокси подтверждён"}
	}
	if ctx.Err() != nil {
		return Result{Message: "Истёк таймаут удалённой проверки"}
	}
	if code == 407 {
		return Result{HTTPCode: code, Message: "Прокси отклонил авторизацию (407)"}
	}
	if code != 0 {
		return Result{HTTPCode: code, Message: fmt.Sprintf("Проверка через прокси: HTTP %d", code)}
	}
	var exit *ssh.ExitError
	if err != nil {
		if e, ok := err.(*ssh.ExitError); ok {
			exit = e
		}
	}
	if exit != nil {
		switch exit.ExitStatus() {
		case 5, 6:
			return Result{Message: "Не удалось разрешить имя прокси или сайта"}
		case 7:
			return Result{Message: "Не удалось подключиться к прокси"}
		case 28:
			return Result{Message: "Истёк таймаут curl"}
		case 35, 60:
			return Result{Message: "Ошибка TLS: проверьте сертификаты на удалённом ПК"}
		case 56:
			return Result{Message: "Прокси отклонил HTTPS CONNECT: проверьте профиль авторизации"}
		}
	}
	return Result{Message: "Удалённая проверка не выполнена: проверьте PowerShell и curl.exe на ПК"}
}
