package tunnel

import (
	"context"
	"errors"
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
	for _, cancelParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "cancel_parent"}[cancelParent], func(t *testing.T) {
			testSSHForwardingAndStop(t, cancelParent)
		})
	}
}

func testSSHForwardingAndStop(t *testing.T, cancelParent bool) {
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
	m := &Manager{}
	defer m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx, sshManager, "127.0.0.1", port, echo.Addr().String()); err == nil {
		t.Fatal("startup succeeded with an occupied local port")
	}
	if state := m.State(); state.Running || state.Connecting {
		t.Fatalf("failed startup retained its reservation: %#v", state)
	}
	_ = free.Close()
	if err = m.Start(ctx, sshManager, "127.0.0.1", port, echo.Addr().String()); err != nil {
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
	if cancelParent {
		cancel()
		deadline := time.Now().Add(2 * time.Second)
		for m.State().Running && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	} else {
		m.Stop()
	}
	if m.State().Running {
		t.Fatal("tunnel still marked running")
	}
	if conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("listener remains open after stop")
	}
}

func TestPendingConnectionDoesNotBlockStateStopOrClose(t *testing.T) {
	for _, closing := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "close"}[closing], func(t *testing.T) {
			identity := testssh.Start(t)
			sshPort, accepted := testssh.Stall(t)
			sshManager, err := sshclient.New(&config.Config{User: "tester", KeyPath: identity.KeyPath, SSHPort: sshPort, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(sshManager.Close)
			free, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := free.Addr().(*net.TCPAddr).Port
			_ = free.Close()
			manager := &Manager{}
			t.Cleanup(manager.Close)
			result := make(chan error, 1)
			go func() { result <- manager.Start(context.Background(), sshManager, "127.0.0.1", port, "127.0.0.1:3128") }()
			select {
			case <-accepted:
			case <-time.After(3 * time.Second):
				t.Fatal("SSH connection did not start")
			}
			state := make(chan State, 1)
			go func() { state <- manager.State() }()
			var address string
			select {
			case value := <-state:
				if !value.Connecting || value.Running {
					t.Fatalf("unexpected pending state: %#v", value)
				}
				address = value.LocalAddress
			case <-time.After(time.Second):
				t.Fatal("State is blocked by the SSH handshake")
			}
			if err := manager.Start(context.Background(), sshManager, "127.0.0.1", port, "127.0.0.1:3128"); err == nil {
				t.Fatal("a second startup was allowed while one is pending")
			}
			stopped := make(chan struct{})
			go func() {
				if closing {
					manager.Close()
				} else {
					manager.Stop()
				}
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("Stop/Close is blocked by the SSH handshake")
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("startup did not report cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled startup did not finish")
			}
			if state := manager.State(); state.Running || state.Connecting {
				t.Fatalf("cancelled attempt remained active: %#v", state)
			}
			if conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond); err == nil {
				_ = conn.Close()
				t.Fatal("cancelled startup leaked its local listener")
			}
		})
	}
}
