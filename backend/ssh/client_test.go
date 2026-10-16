package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
)

func TestChangedKeyAndRejectedIdentity(t *testing.T) {
	server := testssh.Start(t)
	for _, changed := range []bool{true, false} {
		c := &config.Config{User: "wrong-user", KeyPath: server.KeyPath, SSHPort: server.Port, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: time.Second}
		c.KeyPassphrase = "global-passphrase-must-not-break-unencrypted-keys"
		key := server.HostKey
		if changed {
			_, private, _ := ed25519.GenerateKey(rand.Reader)
			signer, _ := ssh.NewSignerFromKey(private)
			key = signer.PublicKey()
		}
		_ = os.WriteFile(c.KnownHosts, []byte(knownhosts.Line([]string{knownhosts.Normalize(c.Address("127.0.0.1"))}, key)+"\n"), 0600)
		m, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Connect(context.Background(), "127.0.0.1", true)
		var failure *Failure
		if !errors.As(err, &failure) {
			t.Fatalf("expected classified error, got %v", err)
		}
		expected := "auth_error"
		if changed {
			expected = "host_key_changed"
		}
		if failure.Code != expected {
			t.Fatalf("want %s got %s", expected, failure.Code)
		}
		if changed {
			p := m.Pending()
			if len(p) != 1 || !p[0].Changed {
				t.Fatal("missing changed key warning")
			}
			if m.Trust(p[0].IP, p[0].Fingerprint) == nil {
				t.Fatal("allowed changed key replacement")
			}
		}
		m.Close()
	}
}

func TestHandshakeTimeout(t *testing.T) {
	server := testssh.Start(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 100)
			for {
				if _, err = conn.Read(buf); err != nil {
					return
				}
			}
		}
	}()
	c := &config.Config{User: "tester", KeyPath: server.KeyPath, SSHPort: listener.Addr().(*net.TCPAddr).Port, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: 100 * time.Millisecond}
	m, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	started := time.Now()
	_, err = m.Connect(context.Background(), "127.0.0.1", true)
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("handshake timeout not enforced: %v", err)
	}
}
