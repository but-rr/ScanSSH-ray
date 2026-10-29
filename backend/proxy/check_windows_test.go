package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
)

// The test binary inspects the exact stdin bytes in place of curl.exe. Real
// curl versions differ in whether they accept a BOM, so HTTP success alone
// cannot detect this Windows PowerShell regression.
func TestMain(m *testing.M) {
	if os.Getenv("SCANSSH_TEST_CURL_STDIN") == "1" {
		expected, decodeErr := base64.StdEncoding.DecodeString(os.Getenv("SCANSSH_TEST_CURL_CONFIG"))
		input, readErr := io.ReadAll(os.Stdin)
		if decodeErr != nil || readErr != nil || !bytes.Equal(input, append(expected, '\r', '\n')) {
			fmt.Fprintln(os.Stderr, "curl configuration must arrive as exact UTF-8 bytes without BOM")
			os.Exit(2)
		}
		fmt.Print("SCANSSH:200")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestWindowsPowerShellCurlConfigWithoutBOM(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Reuse the running test executable rather than creating another EXE in
	// Temp. The alias changes only command resolution, not the production pipe.
	alias := "Set-Alias -Name curl.exe -Value '" + strings.ReplaceAll(executable, "'", "''") + "'; "
	c := &config.Config{ProxyHost: "proxy.invalid", ProxyPort: 3128, ProxyAuth: "basic", ProxyUser: `domain\tester`, ProxyPassword: `p"a\ss$!&пароль`, TestURL: "http://origin.invalid/", ConnectTimeout: 2 * time.Second, CommandTimeout: 5 * time.Second}
	configInput := base64.StdEncoding.EncodeToString([]byte(CurlConfig(c)))
	for _, initialEncoding := range []string{"default", "UTF-8 with BOM"} {
		t.Run(initialEncoding, func(t *testing.T) {
			script := alias + checkScript
			if initialEncoding == "UTF-8 with BOM" {
				// Reproduce the runner's console setting even on PCs whose
				// default code page would hide the bug.
				script = "[Console]::InputEncoding=[Text.Encoding]::UTF8; " + script
			}
			command := EncodedPowerShell(script)
			encoded := command[strings.LastIndex(command, " ")+1:]
			// Allow for cold Windows PowerShell startup on hosted runners.
			// This is a test harness limit; curl's own timeouts are unchanged.
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
			cmd.WaitDelay = 5 * time.Second
			cmd.Env = append(os.Environ(), "SCANSSH_TEST_CURL_STDIN=1", "SCANSSH_TEST_CURL_CONFIG="+configInput)
			cmd.Stdin = strings.NewReader(configInput)
			started := time.Now()
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "SCANSSH:200") {
				t.Fatalf("PowerShell/curl stdin encoding failed after %s (context: %v): %v\n%s", time.Since(started), ctx.Err(), err, output)
			}
		})
	}
}

// Executes the actual Windows PowerShell/curl pipeline against a Basic-auth HTTP
// proxy. The origin is intentionally unresolvable: only the proxy is contacted.
func TestWindowsPowerShellCurlThroughBasicProxy(t *testing.T) {
	if _, err := exec.LookPath("curl.exe"); err != nil {
		t.Skip("curl.exe not installed")
	}
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	user, password := `domain\tester`, `p"a\ss$!&`
	for _, status := range []int{200, 301, 403, 407} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			const target = "http://origin.invalid/internet-test"
			requests := make(chan bool, 8)
			proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ok := r.Method == http.MethodHead && r.URL.String() == target && r.Header.Get("Proxy-Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password))
				requests <- ok
				if !ok {
					w.WriteHeader(400)
					return
				}
				if status == 301 {
					w.Header().Set("Location", "https://must-not-contact.invalid/")
				}
				if status == 407 {
					w.Header().Set("Proxy-Authenticate", `Basic realm="integration"`)
				}
				w.WriteHeader(status)
			}))
			defer proxyServer.Close()
			u, _ := url.Parse(proxyServer.URL)
			port, _ := strconv.Atoi(u.Port())
			c := &config.Config{ProxyHost: u.Hostname(), ProxyPort: port, ProxyAuth: "basic", ProxyUser: user, ProxyPassword: password, TestURL: target, ConnectTimeout: 2 * time.Second, CommandTimeout: 5 * time.Second}
			command := EncodedPowerShell(checkScript)
			encoded := command[strings.LastIndex(command, " ")+1:]
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
			cmd.WaitDelay = 5 * time.Second
			cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(CurlConfig(c))))
			started := time.Now()
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "SCANSSH:"+strconv.Itoa(status)) {
				t.Fatalf("PowerShell/curl transport failed after %s (context: %v): %v\n%s", time.Since(started), ctx.Err(), err, output)
			}
			select {
			case ok := <-requests:
				if !ok {
					t.Fatal("wrong method, URL, or proxy credentials")
				}
			default:
				t.Fatal("proxy was bypassed")
			}
			if len(requests) != 0 {
				t.Fatal("probe followed a redirect or repeated the request")
			}
		})
	}
}
