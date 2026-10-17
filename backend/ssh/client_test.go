package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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
			data, err := os.ReadFile(c.KnownHosts)
			if err != nil || !strings.Contains(string(data), strings.TrimSpace(knownhosts.Line([]string{knownhosts.Normalize(c.Address("127.0.0.1"))}, key))) {
				t.Fatal("changed key overwrote the trusted host")
			}
		}
		m.Close()
	}
}

func TestAutomaticallyAcceptsAndPersistsNewHost(t *testing.T) {
	server := testssh.Start(t)
	c := &config.Config{User: "tester", KeyPath: server.KeyPath, SSHPort: server.Port, KnownHosts: filepath.Join(t.TempDir(), "known_hosts"), ConnectTimeout: time.Second}
	var saved []byte
	for attempt := 0; attempt < 2; attempt++ {
		m, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		client, err := m.Connect(context.Background(), "127.0.0.1", true)
		if err != nil {
			m.Close()
			t.Fatal(err)
		}
		if client == nil || len(m.Pending()) != 0 {
			m.Close()
			t.Fatal("new host required manual confirmation")
		}
		m.Close()
		data, err := os.ReadFile(c.KnownHosts)
		if err != nil || !strings.Contains(string(data), knownhosts.Normalize(c.Address("127.0.0.1"))) {
			t.Fatal("new host key was not saved with its nonstandard port")
		}
		if attempt == 0 {
			saved = data
		} else if string(saved) != string(data) {
			t.Fatal("restart duplicated the existing host key")
		}
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
