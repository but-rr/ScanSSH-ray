package proxy

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"scanssh-ray/backend/config"
)

func TestCurlConfigurationEscapesCredentials(t *testing.T) {
	c := &config.Config{ProxyHost: "proxy.local", ProxyPort: 3128, ProxyAuth: "basic", ProxyUser: `domain\user`, ProxyPassword: `a"b\c$!&`, TestURL: "http://example.com/", ConnectTimeout: 2 * time.Second, CommandTimeout: 6 * time.Second}
	text := CurlConfig(c)
	if !strings.Contains(text, `proxy-user = "domain\\user:a\"b\\c$!&"`) || !strings.Contains(text, `noproxy = ""`) {
		t.Fatalf("incorrect escaping or proxy bypass: %s", text)
	}
	if strings.Contains(text, "insecure") {
		t.Fatal("TLS verification disabled")
	}
	if !strings.Contains(text, "\nhead\n") || strings.Contains(text, "\nlocation\n") {
		t.Fatal("probe must use HEAD without following redirects")
	}
}

func TestPowerShellUsesUTF16LE(t *testing.T) {
	input := "Write-Output 'проверка'"
	command := EncodedPowerShell(input)
	raw, err := base64.StdEncoding.DecodeString(command[strings.LastIndex(command, " ")+1:])
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	if string(utf16.Decode(units)) != input {
		t.Fatal("incorrect PowerShell encoding")
	}
}
