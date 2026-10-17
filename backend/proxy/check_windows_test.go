package proxy

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
)

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
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
			cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(CurlConfig(c))))
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "SCANSSH:"+strconv.Itoa(status)) {
				t.Fatalf("PowerShell/curl transport failed: %v %s", err, output)
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
