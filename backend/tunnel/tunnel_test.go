package tunnel

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"
	"os"
	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
	sshclient "scanssh-ray/backend/ssh"
)

func TestSSHForwardingAndStop(t *testing.T) {
	server := testssh.Start(t)
	c := &config.Config{User: "tester", KeyPath: server.KeyPath, SSHPort: server.Port, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: time.Second}
	if err := os.WriteFile(c.KnownHosts, []byte(knownhosts.Line([]string{knownhosts.Normalize(c.Address("127.0.0.1"))}, server.HostKey)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sshManager, err := sshclient.New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer sshManager.Close()
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	free, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	m := &Manager{}
	defer m.Close()
	if err = m.Start(context.Background(), sshManager, "127.0.0.1", port, echo.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if !m.State().Running {
		t.Fatal("tunnel did not report running")
	}
	local, err := net.DialTimeout("tcp", m.State().LocalAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	_ = local.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = local.Write([]byte("real-ssh-forward")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("real-ssh-forward"))
	if _, err = io.ReadFull(local, response); err != nil || string(response) != "real-ssh-forward" {
		t.Fatalf("forwarded data: %q %v", response, err)
	}
	address := m.State().LocalAddress
	m.Stop()
	if m.State().Running {
		t.Fatal("tunnel still marked running")
	}
	if conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("listener remains open after stop")
	}
}
