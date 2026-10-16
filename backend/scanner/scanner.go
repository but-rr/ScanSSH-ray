package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"scanssh-ray/backend/config"
	"scanssh-ray/backend/proxy"
	sshclient "scanssh-ray/backend/ssh"
)

type Host struct {
	IP                string     `json:"ip"`
	Status            string     `json:"status"`
	Message           string     `json:"message"`
	SSH               bool       `json:"ssh"`
	Internet          bool       `json:"internet"`
	HTTPCode          int        `json:"httpCode"`
	CheckedAt         *time.Time `json:"checkedAt"`
	InternetCheckedAt *time.Time `json:"internetCheckedAt"`
	LatencyMS         int64      `json:"latencyMs"`
	KeyPath           string     `json:"keyPath"`
	User              string     `json:"user"`
}

type Event struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Kind    string    `json:"kind"`
}

type Snapshot struct {
	Config   config.Summary      `json:"config"`
	Pools    []config.Pool       `json:"pools"`
	Hosts    []Host              `json:"hosts"`
	Events   []Event             `json:"events"`
	Pending  []sshclient.Pending `json:"pending"`
	Scanning bool                `json:"scanning"`
	Auto     bool                `json:"auto"`
	LastScan *time.Time          `json:"lastScan"`
}

type Engine struct {
	Config                 *config.Config
	SSH                    *sshclient.Manager
	mu                     sync.RWMutex
	hosts                  []Host
	events                 []Event
	scanning, auto, closed bool
	lastScan               *time.Time
	ctx                    context.Context
	cancel                 context.CancelFunc
	scanCancel             context.CancelFunc
	wg                     sync.WaitGroup
	log                    *os.File
}

func New(cfg *config.Config) (*Engine, error) {
	manager, err := sshclient.New(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{Config: cfg, SSH: manager, auto: cfg.AutoScan, ctx: ctx, cancel: cancel, events: []Event{}}
	for _, ip := range cfg.IPs {
		path, _, user := cfg.Identity(ip)
		e.hosts = append(e.hosts, Host{IP: ip, Status: "unknown", Message: "Ещё не проверен", KeyPath: path, User: user})
	}
	logDir := filepath.Join(cfg.BaseDir, "data")
	if err = os.MkdirAll(logDir, 0700); err != nil {
		manager.Close()
		cancel()
		return nil, fmt.Errorf("не удалось создать каталог журнала")
	}
	logPath := filepath.Join(logDir, "events.log")
	if info, statErr := os.Stat(logPath); statErr == nil && info.Size() > 5*1024*1024 {
		_ = os.Rename(logPath, logPath+".1")
	}
	e.log, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		manager.Close()
		cancel()
		return nil, fmt.Errorf("не удалось открыть журнал событий")
	}
	e.AddEvent("Приложение запущено. Загружено устройств: "+fmt.Sprint(len(e.hosts)), "info")
	e.wg.Add(1)
	go e.loop()
	return e, nil
}

func (e *Engine) loop() {
	defer e.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.mu.RLock()
			due := e.auto && (e.lastScan == nil || time.Since(*e.lastScan) >= e.Config.SSHInterval)
			e.mu.RUnlock()
			if due {
				e.Scan(false)
			}
		}
	}
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return Snapshot{e.Config.Summary(), e.Config.Pools, append([]Host{}, e.hosts...), append([]Event{}, e.events...), e.SSH.Pending(), e.scanning, e.auto, e.lastScan}
}

func (e *Engine) AddEvent(message, kind string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.addEventLocked(message, kind)
}

func (e *Engine) addEventLocked(message, kind string) {
	event := Event{time.Now(), message, kind}
	e.events = append([]Event{event}, e.events...)
	if len(e.events) > 200 {
		e.events = e.events[:200]
	}
	if e.log != nil {
		_, _ = fmt.Fprintf(e.log, "%s [%s] %s\r\n", event.At.Format(time.RFC3339), kind, message)
	}
}

func (e *Engine) SetAuto(enabled bool) { e.mu.Lock(); e.auto = enabled; e.mu.Unlock() }

func (e *Engine) Scan(forceInternet bool) bool {
	e.mu.Lock()
	if e.closed || e.scanning {
		e.mu.Unlock()
		return false
	}
	e.scanning = true
	ctx, cancel := context.WithCancel(e.ctx)
	e.scanCancel = cancel
	e.addEventLocked("Начата проверка устройств", "info")
	e.wg.Add(1)
	e.mu.Unlock()
	go func() {
		defer e.wg.Done()
		defer cancel()
		sem := make(chan struct{}, e.Config.Concurrency)
		var jobs sync.WaitGroup
	outer:
		for i := range e.Config.IPs {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break outer
			}
			jobs.Add(1)
			go func(index int) { defer jobs.Done(); defer func() { <-sem }(); e.check(ctx, index, forceInternet) }(i)
		}
		jobs.Wait()
		e.mu.Lock()
		e.scanning = false
		e.scanCancel = nil
		now := time.Now()
		e.lastScan = &now
		if ctx.Err() != nil {
			e.addEventLocked("Проверка остановлена", "info")
		} else {
			e.addEventLocked("Проверка завершена", "ok")
		}
		e.mu.Unlock()
	}()
	return true
}

func (e *Engine) Cancel() {
	e.mu.RLock()
	cancel := e.scanCancel
	e.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) check(ctx context.Context, index int, force bool) {
	e.mu.RLock()
	h := e.hosts[index]
	e.mu.RUnlock()
	if ctx.Err() != nil {
		return
	}
	started := time.Now()
	client, err := e.SSH.Connect(ctx, h.IP, true)
	if ctx.Err() != nil {
		return
	}
	now := time.Now()
	h.CheckedAt = &now
	h.LatencyMS = time.Since(started).Milliseconds()
	if err != nil {
		h.SSH = false
		h.Internet = false
		h.HTTPCode = 0
		h.InternetCheckedAt = nil
		var failure *sshclient.Failure
		if errors.As(err, &failure) {
			h.Status, h.Message = failure.Code, failure.Message
		} else {
			h.Status, h.Message = "ssh_error", "Ошибка SSH"
		}
	} else {
		h.SSH = true
		if force || h.InternetCheckedAt == nil || time.Since(*h.InternetCheckedAt) >= e.Config.InternetInterval {
			commandCtx, cancel := context.WithTimeout(ctx, e.Config.CommandTimeout+time.Second)
			result := proxy.Check(commandCtx, client, e.Config)
			cancel()
			if ctx.Err() != nil {
				e.SSH.Drop(h.IP)
				return
			}
			checked := time.Now()
			h.InternetCheckedAt = &checked
			h.Internet = result.OK
			h.HTTPCode = result.HTTPCode
			h.Message = result.Message
			if result.OK {
				h.Status = "online"
			} else {
				h.Status = "limited"
			}
			if e.Config.ProxyAuth == "basic" && (e.Config.ProxyUser == "" || e.Config.ProxyPassword == "") {
				h.Status = "config_error"
			}
		}
	}
	e.mu.Lock()
	previous := e.hosts[index].Status
	e.hosts[index] = h
	if previous != h.Status {
		kind := "warn"
		if h.Status == "online" {
			kind = "ok"
		}
		e.addEventLocked(h.IP+": "+h.Message, kind)
	}
	e.mu.Unlock()
}

func (e *Engine) CanTunnel(ip string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, h := range e.hosts {
		if h.IP == ip {
			return h.SSH && h.Internet && h.InternetCheckedAt != nil && time.Since(*h.InternetCheckedAt) <= e.Config.InternetInterval+e.Config.CommandTimeout
		}
	}
	return false
}

func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	e.cancel()
	e.SSH.Close()
	e.wg.Wait()
	e.mu.Lock()
	if e.log != nil {
		_ = e.log.Close()
		e.log = nil
	}
	e.mu.Unlock()
}
