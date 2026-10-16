package proxy

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
)

// Executes the real Windows PowerShell/curl pipeline against a local TLS endpoint
// through an actual Basic-auth CONNECT proxy. No corporate network is contacted.
func TestWindowsPowerShellCurlThroughBasicProxy(t *testing.T) {
	if _, err := exec.LookPath("curl.exe"); err != nil {
		t.Skip("curl.exe not installed")
	}
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer target.Close()
	ca := filepath.Join(t.TempDir(), "test-ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	user, password := `domain\tester`, `p"a\ss$!&`
	authorized := make(chan bool, 5)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := r.Method == http.MethodConnect && r.Header.Get("Proxy-Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		authorized <- ok
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm="integration"`)
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		remote, err := net.Dial("tcp", target.Listener.Addr().String())
		if err != nil {
			w.WriteHeader(502)
			return
		}
		local, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = remote.Close()
			return
		}
		defer local.Close()
		defer remote.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		done := make(chan struct{})
		go func() { _, _ = io.Copy(remote, buffer); _ = remote.(*net.TCPConn).CloseWrite(); close(done) }()
		_, _ = io.Copy(local, remote)
		_ = local.Close()
		<-done
	}))
	defer proxyServer.Close()
	u, _ := url.Parse(proxyServer.URL)
	port, _ := strconv.Atoi(u.Port())
	c := &config.Config{ProxyHost: u.Hostname(), ProxyPort: port, ProxyAuth: "basic", ProxyUser: user, ProxyPassword: password, TestURL: target.URL, ConnectTimeout: 2 * time.Second, CommandTimeout: 5 * time.Second}
	// Trust only this test's ephemeral certificate without changing system trust or disabling TLS verification.
	curlConfig := CurlConfig(c) + "cacert = " + quote(ca) + "\n"
	command := EncodedPowerShell(checkScript)
	encoded := command[strings.LastIndex(command, " ")+1:]
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(curlConfig)))
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "SCANSSH:204") {
		t.Fatalf("PowerShell/curl transport failed: %v %s", err, output)
	}
	select {
	case ok := <-authorized:
		if !ok {
			t.Fatal("incorrect proxy credentials")
		}
	default:
		t.Fatal("proxy was bypassed")
	}
}
