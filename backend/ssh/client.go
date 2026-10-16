package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"scanssh-ray/backend/config"
)

type Failure struct{ Code, Message string }

func (e *Failure) Error() string { return e.Message }

type Pending struct {
	IP          string `json:"ip"`
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
	Changed     bool   `json:"changed"`
	key         ssh.PublicKey
}

type Manager struct {
	cfg     *config.Config
	mu      sync.Mutex
	clients map[string]*ssh.Client
	pending map[string]Pending
	closed  bool
}

func New(cfg *config.Config) (*Manager, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.KnownHosts), 0700); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог known_hosts")
	}
	f, err := os.OpenFile(cfg.KnownHosts, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть known_hosts")
	}
	_ = f.Close()
	if _, err = knownhosts.New(cfg.KnownHosts); err != nil {
		return nil, fmt.Errorf("не удалось прочитать known_hosts: проверьте формат")
	}
	return &Manager{cfg: cfg, clients: map[string]*ssh.Client{}, pending: map[string]Pending{}}, nil
}

func (m *Manager) verify(ip string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		callback, err := knownhosts.New(m.cfg.KnownHosts)
		if err != nil {
			return &Failure{"config_error", "Не удалось прочитать known_hosts"}
		}
		err = callback(hostname, remote, key)
		if err == nil {
			delete(m.pending, ip)
			return nil
		}
		var keyError *knownhosts.KeyError
		if errors.As(err, &keyError) {
			changed := len(keyError.Want) > 0
			m.pending[ip] = Pending{IP: ip, Fingerprint: ssh.FingerprintSHA256(key), Algorithm: key.Type(), Changed: changed, key: key}
			if changed {
				return &Failure{"host_key_changed", "Ключ SSH-сервера изменился. Проверьте known_hosts"}
			}
			return &Failure{"host_key_unknown", "Подтвердите отпечаток SSH-сервера"}
		}
		return &Failure{"host_key_error", "Ключ SSH-сервера отклонён"}
	}
}

func (m *Manager) Pending() []Pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Pending{}
	for _, p := range m.pending {
		out = append(out, p)
	}
	return out
}

func (m *Manager) Trust(ip, fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[ip]
	if !ok || p.Fingerprint != fingerprint {
		return fmt.Errorf("отпечаток устарел: повторите проверку")
	}
	if p.Changed {
		return fmt.Errorf("замена известного ключа запрещена: сверьте ключ на ПК и исправьте known_hosts вручную")
	}
	callback, err := knownhosts.New(m.cfg.KnownHosts)
	if err != nil {
		return fmt.Errorf("не удалось прочитать known_hosts")
	}
	address := m.cfg.Address(ip)
	if err = callback(address, &net.TCPAddr{IP: net.ParseIP(ip), Port: m.cfg.SSHPort}, p.key); err == nil {
		delete(m.pending, ip)
		return nil
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) > 0 {
		return fmt.Errorf("known_hosts изменён: повторите проверку")
	}
	f, err := os.OpenFile(m.cfg.KnownHosts, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("не удалось записать known_hosts")
	}
	// Leading newline also handles existing OpenSSH files without a trailing newline.
	_, err = fmt.Fprintln(f, "\n"+knownhosts.Line([]string{knownhosts.Normalize(address)}, p.key))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("ошибка сохранения known_hosts")
	}
	delete(m.pending, ip)
	return nil
}

func (m *Manager) Connect(ctx context.Context, ip string, cached bool) (*ssh.Client, error) {
	m.mu.Lock()
	client := m.clients[ip]
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return nil, &Failure{"cancelled", "Соединение закрыто"}
	}
	if cached && client != nil {
		probeCtx, cancel := context.WithTimeout(ctx, m.cfg.ConnectTimeout)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				return client, nil
			}
			m.Drop(ip)
		case <-probeCtx.Done():
			m.Drop(ip)
			return nil, &Failure{"unreachable", "SSH-соединение не отвечает"}
		}
	}
	path, passphrase, user := m.cfg.Identity(ip)
	if path == "" {
		return nil, &Failure{"config_error", "Укажите SSH_KEY_PATH или правило ключа в .env"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Failure{"config_error", "Не удалось прочитать файл SSH-ключа"}
	}
	signer, err := ssh.ParsePrivateKey(data)
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) && passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(data, []byte(passphrase))
	}
	if err != nil {
		return nil, &Failure{"config_error", "Не удалось открыть OpenSSH-ключ: проверьте формат и парольную фразу"}
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, m.cfg.ConnectTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(deadlineCtx, "tcp", m.cfg.Address(ip))
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, &Failure{"unreachable", "ПК недоступен или пакеты фильтруются"}
		}
		return nil, &Failure{"ssh_closed", "TCP-порт SSH недоступен"}
	}
	deadline, _ := deadlineCtx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(deadlineCtx, func() { _ = conn.Close() })
	cc, chans, reqs, err := ssh.NewClientConn(conn, m.cfg.Address(ip), &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: m.verify(ip), Timeout: m.cfg.ConnectTimeout})
	stopped := stop()
	if err != nil || !stopped {
		_ = conn.Close()
		if deadlineCtx.Err() != nil {
			return nil, &Failure{"unreachable", "Истёк таймаут SSH-соединения"}
		}
		var failure *Failure
		if errors.As(err, &failure) {
			return nil, failure
		}
		if err != nil && strings.Contains(err.Error(), "unable to authenticate") {
			return nil, &Failure{"auth_error", "Сервер отклонил SSH-ключ или пользователя"}
		}
		return nil, &Failure{"ssh_error", "Ошибка согласования SSH-соединения"}
	}
	_ = conn.SetDeadline(time.Time{})
	client = ssh.NewClient(cc, chans, reqs)
	if cached {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			_ = client.Close()
			return nil, &Failure{"cancelled", "Соединение закрыто"}
		}
		if old := m.clients[ip]; old != nil {
			_ = old.Close()
		}
		m.clients[ip] = client
		m.mu.Unlock()
	}
	return client, nil
}

func (m *Manager) Drop(ip string) {
	m.mu.Lock()
	client := m.clients[ip]
	delete(m.clients, ip)
	m.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	m.clients = map[string]*ssh.Client{}
	m.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

// Run keeps secrets out of the remote command line by sending input on the SSH channel.
func Run(ctx context.Context, client *ssh.Client, command, input string) (string, error) {
	var output cappedBuffer
	done := make(chan error, 1)
	go func() {
		session, err := client.NewSession()
		if err != nil {
			done <- err
			return
		}
		defer session.Close()
		session.Stdin = strings.NewReader(input)
		session.Stdout = &output
		session.Stderr = io.Discard
		done <- session.Run(command)
	}()
	select {
	case err := <-done:
		return output.String(), err
	case <-ctx.Done():
		_ = client.Close()
		<-done
		return "", ctx.Err()
	}
}

type cappedBuffer struct{ data []byte }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if left := 4096 - len(b.data); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *cappedBuffer) String() string { return string(b.data) }
