package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"scanssh-ray/backend/config"
	"scanssh-ray/backend/scanner"
	"scanssh-ray/backend/tray"
	"scanssh-ray/backend/tunnel"
)

type View struct {
	Monitor *scanner.Snapshot `json:"monitor"`
	Tunnel  tunnel.State      `json:"tunnel"`
	Error   string            `json:"error"`
	EnvPath string            `json:"envPath"`
}

type App struct {
	mu        sync.Mutex
	ctx       context.Context
	engine    *scanner.Engine
	tunnels   tunnel.Manager
	envPath   string
	loadError string
	tray      atomic.Pointer[tray.Tray]
	exiting   atomic.Bool
}

func NewApp() *App { return &App{envPath: config.ResolveEnv()} }
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	icon, err := tray.New(trayIcon, a.quitFromTray)
	if err != nil {
		// Never start background work or hide a window without a working tray.
		log.Printf("Трей: %v", err)
		_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.ErrorDialog, Title: "ScanSSH-ray", Message: "Не удалось создать значок в трее. Приложение будет закрыто.\n" + err.Error()})
		a.exiting.Store(true)
		runtime.Quit(ctx)
		return
	}
	a.tray.Store(icon)
	_ = a.ReloadConfig()
}

// beforeClose also intercepts Alt+F4 and native runtime.Quit requests.
func (a *App) beforeClose(_ context.Context) bool {
	if a.exiting.Load() {
		return false
	}
	_ = a.HideToTray()
	return true
}

func (a *App) HideToTray() error { return a.tray.Load().Hide() }

// The tray menu is the only ordinary application action allowed to exit.
func (a *App) quitFromTray() {
	if a.exiting.CompareAndSwap(false, true) {
		runtime.Quit(a.ctx)
	}
}
func (a *App) domReady(ctx context.Context) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil {
		return
	}
	width, height := runtime.WindowGetSize(ctx)
	for _, screen := range screens {
		if screen.IsCurrent {
			if screen.Size.Width > 0 && screen.Size.Height > 0 && (width > screen.Size.Width || height > screen.Size.Height) {
				runtime.WindowMaximise(ctx)
			}
			return
		}
	}
}
func (a *App) shutdown(ctx context.Context) {
	if icon := a.tray.Swap(nil); icon != nil {
		icon.Close()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tunnels.Close()
	if a.engine != nil {
		a.engine.Close()
	}
}

func (a *App) GetState() View {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := View{Tunnel: a.tunnels.State(), Error: a.loadError, EnvPath: a.envPath}
	if a.engine != nil {
		snapshot := a.engine.Snapshot()
		v.Monitor = &snapshot
	}
	return v
}

func (a *App) ReloadConfig() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, err := config.Load(a.envPath)
	if err != nil {
		a.loadError = err.Error()
		return err
	}
	engine, err := scanner.New(cfg)
	if err != nil {
		a.loadError = err.Error()
		return err
	}
	a.tunnels.Stop()
	if a.engine != nil {
		a.engine.Close()
	}
	a.engine = engine
	a.loadError = ""
	return nil
}

func (a *App) Scan() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine == nil {
		return fmt.Errorf("сначала исправьте .env")
	}
	if !a.engine.Scan(true) {
		return fmt.Errorf("проверка уже выполняется")
	}
	return nil
}

func (a *App) CancelScan() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine != nil {
		a.engine.Cancel()
	}
}

func (a *App) ScanHost(ip string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine == nil {
		return fmt.Errorf("сначала исправьте .env")
	}
	return a.engine.ScanHost(ip)
}

func (a *App) SetAuto(enabled bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine != nil {
		a.engine.SetAuto(enabled)
	}
}

func (a *App) StartTunnel(ip string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.engine == nil || !a.engine.CanTunnel(ip) {
		return fmt.Errorf("выполните свежую проверку SSH и интернета выбранного ПК")
	}
	cfg := a.engine.Config
	if err := a.tunnels.Start(a.ctx, a.engine.SSH, ip, port, net.JoinHostPort(cfg.ProxyHost, strconv.Itoa(cfg.ProxyPort))); err != nil {
		return err
	}
	a.engine.AddEvent(ip+": запущен туннель на 127.0.0.1:"+strconv.Itoa(port), "ok")
	return nil
}

func (a *App) StopTunnel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tunnels.Stop()
	if a.engine != nil {
		a.engine.AddEvent("SSH-туннель остановлен", "info")
	}
}

func (a *App) OpenConfig() error {
	cmd := exec.Command("notepad.exe", a.envPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось открыть .env в Блокноте")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (a *App) ExportReport() (string, error) {
	view := a.GetState()
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{DefaultFilename: "scanssh-report.json", Filters: []runtime.FileFilter{{DisplayName: "JSON report", Pattern: "*.json"}}})
	if err != nil {
		return "", fmt.Errorf("не удалось открыть диалог сохранения")
	}
	if path == "" {
		return "", nil
	}
	data, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return "", fmt.Errorf("не удалось сформировать отчёт")
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("не удалось сохранить отчёт")
	}
	return path, nil
}
