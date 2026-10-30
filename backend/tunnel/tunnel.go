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
	Connecting   bool       `json:"connecting"`
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
	mu      sync.Mutex
	active  *session
	pending *connectionAttempt
	state   State
	closed  bool
}

type connectionAttempt struct{ cancel context.CancelFunc }

func (m *Manager) State() State { m.mu.Lock(); defer m.mu.Unlock(); return m.state }

func (m *Manager) Start(ctx context.Context, sshManager *sshclient.Manager, ip string, port int, target string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("порт должен быть от 1 до 65535")
	}
	startCtx, startCancel := context.WithCancel(ctx)
	defer startCancel()
	attempt := &connectionAttempt{cancel: startCancel}
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("приложение завершает работу")
	}
	if m.active != nil || m.pending != nil {
		m.mu.Unlock()
		return fmt.Errorf("сначала остановите действующий туннель")
	}
	m.pending = attempt
	m.state = State{Connecting: true, IP: ip, LocalAddress: address, Target: target}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.pending == attempt {
			m.pending = nil
			m.state = State{}
		}
		m.mu.Unlock()
	}()
	// Reserve the attempt under the lock, but perform all I/O outside it.
	// Stop can cancel this attempt without waiting for SSH or the proxy probe.
	activated := false
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("локальный порт %d занят или недоступен", port)
	}
	defer func() {
		if !activated {
			_ = listener.Close()
		}
	}()
	client, err := sshManager.Connect(startCtx, ip, false)
	if err != nil {
		if startCtx.Err() != nil {
			return fmt.Errorf("подключение туннеля отменено: %w", startCtx.Err())
		}
		return err
	}
	defer func() {
		if !activated {
			_ = client.Close()
		}
	}()
	stopCloseOnCancel := context.AfterFunc(startCtx, func() { _ = client.Close() })
	defer stopCloseOnCancel()
	// Confirm the SSH server permits direct-tcpip to the actual proxy before reporting success.
	probeCtx, probeCancel := context.WithTimeout(startCtx, 8*time.Second)
	probe, err := client.DialContext(probeCtx, "tcp", target)
	probeCancel()
	if err != nil {
		if startCtx.Err() != nil {
			return fmt.Errorf("подключение туннеля отменено: %w", startCtx.Err())
		}
		return fmt.Errorf("SSH-сервер не открыл соединение к прокси: проверьте AllowTcpForwarding и адрес прокси")
	}
	_ = probe.Close()
	m.mu.Lock()
	if m.closed || m.pending != attempt || startCtx.Err() != nil {
		m.mu.Unlock()
		return fmt.Errorf("подключение туннеля отменено: %w", context.Canceled)
	}
	forwardCtx, cancel := context.WithCancel(ctx)
	s := &session{listener: listener, closeSSH: client.Close, cancel: cancel}
	m.pending = nil
	m.active = s
	now := time.Now()
	m.state = State{Running: true, IP: ip, LocalAddress: address, Target: target, StartedAt: &now}
	activated = true
	m.mu.Unlock()
	go func() { <-forwardCtx.Done(); s.close() }()
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
			if ctx.Err() != nil {
				m.state = State{}
				return
			}
			m.state.Running = false
			if err != nil {
				m.state.Error = "SSH-туннель разорван"
			}
		}
	}()
	return nil
}

func (m *Manager) stop(closing bool) {
	m.mu.Lock()
	if closing {
		m.closed = true
	}
	pending := m.pending
	m.pending = nil
	s := m.active
	m.active = nil
	m.state = State{}
	m.mu.Unlock()
	if pending != nil {
		pending.cancel()
	}
	if s != nil {
		s.close()
	}
}

func (m *Manager) Stop()  { m.stop(false) }
func (m *Manager) Close() { m.stop(true) }
