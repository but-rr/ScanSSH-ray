package scanner

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
)

func TestRealSSHMonitoringLifecycle(t *testing.T) {
	server := testssh.Start(t)
	base := t.TempDir()
	c := &config.Config{BaseDir: base, EnvPath: filepath.Join(base, ".env"), User: "tester", SSHPort: server.Port, KeyPath: server.KeyPath, KnownHosts: filepath.Join(base, "known_hosts"), IPs: []string{"127.0.0.1"}, Pools: []config.Pool{{Label: "loopback", IPs: []string{"127.0.0.1"}}}, ProxyHost: "127.0.0.1", ProxyPort: 3128, ProxyAuth: "basic", ProxyUser: "integration-user", ProxyPassword: "secret-proxy-123", TestURL: "https://example.com", ConnectTimeout: time.Second, CommandTimeout: time.Second, SSHInterval: time.Hour, InternetInterval: time.Hour, Concurrency: 2}
	e, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	scan := func() {
		t.Helper()
		if !e.Scan(true) {
			t.Fatal("scan did not start")
		}
		deadline := time.Now().Add(5 * time.Second)
		for e.Snapshot().Scanning {
			if time.Now().After(deadline) {
				t.Fatal("scan hung")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	scan()
	snapshot := e.Snapshot()
	if snapshot.Hosts[0].Status != "host_key_unknown" || len(snapshot.Pending) != 1 {
		t.Fatalf("unknown host: %+v pending=%v", snapshot.Hosts, snapshot.Pending)
	}
	if err = e.SSH.Trust("127.0.0.1", "SHA256:wrong"); err == nil {
		t.Fatal("trusted wrong fingerprint")
	}
	if err = e.SSH.Trust("127.0.0.1", snapshot.Pending[0].Fingerprint); err != nil {
		t.Fatal(err)
	}
	scan()
	if h := e.Snapshot().Hosts[0]; h.Status != "online" || !h.SSH || !h.Internet || h.HTTPCode != 204 {
		t.Fatalf("online: %+v", h)
	}
	if !e.CanTunnel("127.0.0.1") {
		t.Fatal("online host cannot tunnel")
	}
	execution := <-server.Executions
	if strings.Contains(execution.Command, c.ProxyPassword) || strings.Contains(execution.Command, c.ProxyUser) {
		t.Fatal("credentials leaked to command line")
	}
	input, err := base64.StdEncoding.DecodeString(execution.Input)
	if err != nil || !strings.Contains(string(input), c.ProxyPassword) {
		t.Fatal("proxy configuration was not sent via SSH stdin")
	}
	server.ReplyCode.Store(407)
	scan()
	if h := e.Snapshot().Hosts[0]; h.Status != "limited" || h.Internet || h.HTTPCode != 407 {
		t.Fatalf("proxy auth failure: %+v", h)
	}
	if e.CanTunnel("127.0.0.1") {
		t.Fatal("failed internet check allowed tunnel")
	}
	if server.Connections.Load() != 1 {
		t.Fatalf("connection cache not reused: %d", server.Connections.Load())
	}
	raw, _ := json.Marshal(e.Snapshot())
	log, _ := os.ReadFile(filepath.Join(base, "data", "events.log"))
	if strings.Contains(string(raw), c.ProxyPassword) || strings.Contains(string(log), c.ProxyPassword) {
		t.Fatal("password in snapshot/log")
	}
}
