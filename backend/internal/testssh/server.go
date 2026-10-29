// Package testssh provides a real loopback SSH server for integration tests only.
package testssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

type Execution struct{ Command, Input string }
type Server struct {
	Port        int
	KeyPath     string
	HostKey     ssh.PublicKey
	ReplyCode   atomic.Int32
	Connections atomic.Int32
	Executions  chan Execution
}

func Start(t *testing.T, execute ...func(Execution) (string, uint32)) *Server {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	_, identity, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(identity)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(identity)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identity")
	if err = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Port: listener.Addr().(*net.TCPAddr).Port, KeyPath: path, HostKey: key.PublicKey(), Executions: make(chan Execution, 100)}
	s.ReplyCode.Store(204)
	conf := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "tester" && bytes.Equal(k.Marshal(), signer.PublicKey().Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("unauthorized")
	}}
	conf.AddHostKey(key)
	var sockets sync.Map
	t.Cleanup(func() {
		_ = listener.Close()
		sockets.Range(func(k, v any) bool { _ = k.(net.Conn).Close(); return true })
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			sockets.Store(conn, true)
			go func() {
				defer sockets.Delete(conn)
				defer conn.Close()
				server, channels, requests, err := ssh.NewServerConn(conn, conf)
				if err != nil {
					return
				}
				defer server.Close()
				s.Connections.Add(1)
				go func() {
					for req := range requests {
						_ = req.Reply(req.Type == "keepalive@openssh.com", nil)
					}
				}()
				for incoming := range channels {
					switch incoming.ChannelType() {
					case "session":
						channel, reqs, err := incoming.Accept()
						if err != nil {
							continue
						}
						go func() {
							defer channel.Close()
							for req := range reqs {
								if req.Type != "exec" {
									_ = req.Reply(false, nil)
									continue
								}
								var payload struct{ Command string }
								if ssh.Unmarshal(req.Payload, &payload) != nil {
									_ = req.Reply(false, nil)
									return
								}
								_ = req.Reply(true, nil)
								input, _ := io.ReadAll(channel)
								execution := Execution{payload.Command, string(input)}
								s.Executions <- execution
								output, status := fmt.Sprintf("SCANSSH:%03d", s.ReplyCode.Load()), uint32(0)
								if len(execute) > 0 {
									output, status = execute[0](execution)
								}
								_, _ = io.WriteString(channel, output)
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
								return
							}
						}()
					case "direct-tcpip":
						var payload struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						if ssh.Unmarshal(incoming.ExtraData(), &payload) != nil || payload.Host != "127.0.0.1" {
							_ = incoming.Reject(ssh.Prohibited, "loopback only")
							continue
						}
						remote, err := net.Dial("tcp", net.JoinHostPort(payload.Host, strconv.Itoa(int(payload.Port))))
						if err != nil {
							_ = incoming.Reject(ssh.ConnectionFailed, "refused")
							continue
						}
						channel, reqs, err := incoming.Accept()
						if err != nil {
							_ = remote.Close()
							continue
						}
						go ssh.DiscardRequests(reqs)
						go func() {
							defer remote.Close()
							defer channel.Close()
							done := make(chan struct{})
							go func() {
								_, _ = io.Copy(remote, channel)
								if tcp, ok := remote.(*net.TCPConn); ok {
									_ = tcp.CloseWrite()
								}
								close(done)
							}()
							_, _ = io.Copy(channel, remote)
							_ = channel.CloseWrite()
							<-done
						}()
					default:
						_ = incoming.Reject(ssh.UnknownChannelType, "unsupported")
					}
				}
			}()
		}
	}()
	return s
}
