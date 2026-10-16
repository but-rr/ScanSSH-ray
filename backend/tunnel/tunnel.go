package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	sshclient "scanssh-ray/backend/ssh"
)

type State struct {
	Running      bool       `json:"running"`
	IP           string     `json:"ip"`
	LocalAddress string     `json:"localAddress"`
	Target       string     `json:"target"`
	StartedAt    *time.Time `json:"startedAt"`
	Error        string     `json:"error"`
}

type session struct {
	listener    net.Listener
	closeSSH    func() error
	cancel      context.CancelFunc
	connections sync.Map
	once        sync.Once
}

func (s *session) close() {
	s.once.Do(func() {
		s.cancel()
		_ = s.listener.Close()
		_ = s.closeSSH()
		s.connections.Range(func(key, value any) bool { _ = key.(net.Conn).Close(); return true })
	})
}

type Manager struct {
	mu     sync.Mutex
	active *session
	state  State
	closed bool
}

func (m *Manager) State() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }

func (m *Manager) Start(ctx context.Context, sshManager *sshclient.Manager, ip string, port int, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("приложение завершает работу")
	}
	if m.active != nil {
		return fmt.Errorf("сначала остановите действующий туннель")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("порт должен быть от 1 до 65535")
	}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("локальный порт %d занят или недоступен", port)
	}
	client, err := sshManager.Connect(ctx, ip, false)
	if err != nil {
		_ = listener.Close()
		return err
	}
	// Confirm the SSH server permits direct-tcpip to the actual proxy before reporting success.
	probeCtx, probeCancel := context.WithTimeout(ctx, 8*time.Second)
	probe, err := client.DialContext(probeCtx, "tcp", target)
	probeCancel()
	if err != nil {
		_ = client.Close()
		_ = listener.Close()
		return fmt.Errorf("SSH-сервер не открыл соединение к прокси: проверьте AllowTcpForwarding и адрес прокси")
	}
	_ = probe.Close()
	forwardCtx, cancel := context.WithCancel(context.Background())
	s := &session{listener: listener, closeSSH: client.Close, cancel: cancel}
	m.active = s
	now := time.Now()
	m.state = State{Running: true, IP: ip, LocalAddress: address, Target: target, StartedAt: &now}
	go func() {
		limit := make(chan struct{}, 64)
		for {
			local, err := listener.Accept()
			if err != nil {
				return
			}
			s.connections.Store(local, true)
			select {
			case limit <- struct{}{}:
			default:
				s.connections.Delete(local)
				_ = local.Close()
				continue
			}
			go func() {
				defer func() { <-limit; s.connections.Delete(local); _ = local.Close() }()
				dialCtx, done := context.WithTimeout(forwardCtx, 10*time.Second)
				remote, err := client.DialContext(dialCtx, "tcp", target)
				done()
				if err != nil {
					return
				}
				defer remote.Close()
				copied := make(chan struct{})
				go func() {
					_, _ = io.Copy(remote, local)
					if c, ok := remote.(interface{ CloseWrite() error }); ok {
						_ = c.CloseWrite()
					}
					close(copied)
				}()
				_, _ = io.Copy(local, remote)
				_ = local.Close()
				_ = remote.Close()
				<-copied
			}()
		}
	}()
	go func() {
		err := client.Wait()
		s.close()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.active == s {
			m.active = nil
			m.state.Running = false
			if err != nil {
				m.state.Error = "SSH-туннель разорван"
			}
		}
	}()
	return nil
}

func (m *Manager) Stop() {
	m.mu.Lock()
	s := m.active
	m.active = nil
	m.state = State{}
	m.mu.Unlock()
	if s != nil {
		s.close()
	}
}
func (m *Manager) Close() { m.mu.Lock(); m.closed = true; m.mu.Unlock(); m.Stop() }
