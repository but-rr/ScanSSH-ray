package scanner

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
)

func waitForScan(t *testing.T, e *Engine) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot := e.Snapshot()
		if !snapshot.Scanning {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatal("scan hung")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func singlePCEngine(t *testing.T, port int) (*Engine, *testssh.Server) {
	t.Helper()
	server := testssh.Start(t)
	if port == 0 {
		port = server.Port
	}
	base := t.TempDir()
	cfg := &config.Config{
		BaseDir: base, EnvPath: filepath.Join(base, ".env"), User: "tester", SSHPort: port,
		KeyPath: server.KeyPath, KnownHosts: filepath.Join(base, "known_hosts"),
		IPs: []string{"127.0.0.1", "127.0.0.2"}, ProxyHost: "127.0.0.1", ProxyPort: 3128,
		ProxyAuth: "none", TestURL: "http://example.com", ConnectTimeout: 2 * time.Second,
		CommandTimeout: time.Second, SSHInterval: time.Hour, InternetInterval: time.Hour, Concurrency: 2,
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, server
}

func TestScanHostOnlyChecksSelectedPCAndForcesInternet(t *testing.T) {
	e, server := singlePCEngine(t, 0)
	originalOther := e.Snapshot().Hosts[1]
	for _, ip := range []string{"", "127.0.0.3", "127.0.0.1:22"} {
		if err := e.ScanHost(ip); err == nil {
			t.Fatalf("unexpectedly accepted target %q", ip)
		}
	}
	if e.Snapshot().Scanning || server.Connections.Load() != 0 {
		t.Fatal("invalid target started network activity")
	}
	if err := e.ScanHost("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	first := waitForScan(t, e)
	if h := first.Hosts[0]; !h.SSH || !h.Internet || h.Status != "online" || h.HTTPCode != 204 {
		t.Fatalf("single-PC check failed: %+v", h)
	}
	if first.LastScan != nil || first.ScanIP != "" || first.Auto || !reflect.DeepEqual(first.Hosts[1], originalOther) {
		t.Fatal("single-PC check changed the other PC, scan schedule, or auto setting")
	}
	if len(server.Executions) != 1 {
		t.Fatalf("expected exactly one proxy check, got %d", len(server.Executions))
	}
	// The next automatic full scan is not due; a manual check must preserve this schedule.
	lastFull := time.Now().Add(-time.Minute)
	e.mu.Lock()
	e.lastScan = &lastFull
	e.mu.Unlock()
	e.SetAuto(true)
	server.ReplyCode.Store(407)
	if err := e.ScanHost("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	second := waitForScan(t, e)
	if h := second.Hosts[0]; !h.SSH || h.Internet || h.Status != "limited" || h.HTTPCode != 407 || !h.InternetCheckedAt.After(*first.Hosts[0].InternetCheckedAt) || !h.CheckedAt.After(*first.Hosts[0].CheckedAt) {
		t.Fatalf("fresh cached Internet result was not rechecked: %+v", h)
	}
	if !second.Auto || second.LastScan == nil || !second.LastScan.Equal(lastFull) || !reflect.DeepEqual(second.Hosts[1], originalOther) {
		t.Fatal("manual check affected automatic scheduling or the other PC")
	}
	if len(server.Executions) != 2 || server.Connections.Load() != 1 {
		t.Fatal("manual check must probe SSH and execute another proxy check on the cached connection")
	}
	if event := second.Events[0]; event.Kind != "warn" || !strings.Contains(event.Message, "127.0.0.1") {
		t.Fatalf("failed check completion missing from journal: %+v", event)
	}
	e.SetAuto(false)
	if !e.Scan(true) {
		t.Fatal("full scan cannot start after a single-PC check")
	}
	full := waitForScan(t, e)
	if full.Hosts[1].CheckedAt == nil || full.LastScan == nil || !full.LastScan.After(lastFull) || full.ScanIP != "" {
		t.Fatal("full scan no longer checks all PCs or updates its schedule")
	}
}

func TestScanHostBusyCancellationAndShutdown(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn // Hold the SSH handshake until cancellation.
		}
	}()
	e, _ := singlePCEngine(t, listener.Addr().(*net.TCPAddr).Port)
	before := e.Snapshot()
	if err := e.ScanHost("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("SSH connection did not start")
	}
	if snapshot := e.Snapshot(); !snapshot.Scanning || snapshot.ScanIP != "127.0.0.1" {
		t.Fatal("single-PC progress is missing")
	}
	if e.Scan(false) || e.ScanHost("127.0.0.1") == nil || e.ScanHost("127.0.0.2") == nil {
		t.Fatal("overlapping automatic/manual checks were accepted")
	}
	e.Cancel()
	after := waitForScan(t, e)
	if after.ScanIP != "" || !reflect.DeepEqual(after.Hosts, before.Hosts) || after.LastScan != nil {
		t.Fatal("cancelled check left stale progress or modified results")
	}
	// Conversely, a manual single-PC request must not overlap a full scan.
	if !e.Scan(false) {
		t.Fatal("full scan cannot start after cancellation")
	}
	if e.ScanHost("127.0.0.1") == nil {
		t.Fatal("single-PC request overlapped the full scan")
	}
	e.Close()
	if e.ScanHost("127.0.0.1") == nil || e.Scan(true) {
		t.Fatal("closed engine accepted a scan")
	}
}

func TestRealSSHMonitoringLifecycle(t *testing.T) {
	server := testssh.Start(t)
	base := t.TempDir()
	c := &config.Config{BaseDir: base, EnvPath: filepath.Join(base, ".env"), User: "tester", SSHPort: server.Port, KeyPath: server.KeyPath, KnownHosts: filepath.Join(base, "known_hosts"), IPs: []string{"127.0.0.1"}, Pools: []config.Pool{{Label: "loopback", IPs: []string{"127.0.0.1"}}}, ProxyHost: "127.0.0.1", ProxyPort: 3128, ProxyAuth: "basic", ProxyUser: "integration-user", ProxyPassword: "secret-proxy-123", TestURL: "http://example.com", ConnectTimeout: time.Second, CommandTimeout: time.Second, SSHInterval: time.Hour, InternetInterval: time.Hour, Concurrency: 2}
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
	if snapshot.Hosts[0].Status != "online" || len(snapshot.Pending) != 0 {
		t.Fatalf("first scan should accept the server automatically: %+v pending=%v", snapshot.Hosts, snapshot.Pending)
	}
	if h := e.Snapshot().Hosts[0]; h.Status != "online" || !h.SSH || !h.Internet || h.HTTPCode != 204 {
		t.Fatalf("online: %+v", h)
	}
	if !e.CanTunnel("127.0.0.1") {
		t.Fatal("online host cannot tunnel")
	}
	var execution testssh.Execution
	select {
	case execution = <-server.Executions:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy command did not reach the SSH server")
	}
	if strings.Contains(execution.Command, c.ProxyPassword) || strings.Contains(execution.Command, c.ProxyUser) {
		t.Fatal("credentials leaked to command line")
	}
	input, err := base64.StdEncoding.DecodeString(execution.Input)
	if err != nil || !strings.Contains(string(input), c.ProxyPassword) {
		t.Fatal("proxy configuration was not sent via SSH stdin")
	}
	server.ReplyCode.Store(301)
	scan()
	if h := e.Snapshot().Hosts[0]; h.Status != "online" || !h.Internet || h.HTTPCode != 301 {
		t.Fatalf("HTTP redirect should confirm reachability without HTTPS: %+v", h)
	}
	server.ReplyCode.Store(407)
	scan()
	if h := e.Snapshot().Hosts[0]; h.Status != "limited" || h.Internet || h.HTTPCode != 407 {
		t.Fatalf("proxy auth failure: %+v", h)
	}
	if e.CanTunnel("127.0.0.1") {
		t.Fatal("failed internet check allowed tunnel")
	}
	server.ReplyCode.Store(403)
	scan()
	if h := e.Snapshot().Hosts[0]; h.Status != "limited" || h.Internet || h.HTTPCode != 403 {
		t.Fatalf("HTTP 403 must not confirm Internet access: %+v", h)
	}
	if server.Connections.Load() != 1 {
		t.Fatalf("connection cache not reused: %d", server.Connections.Load())
	}
	raw, err := json.Marshal(e.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(base, "data", "events.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), c.ProxyPassword) || strings.Contains(string(log), c.ProxyPassword) {
		t.Fatal("password in snapshot/log")
	}
}
