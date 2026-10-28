//go:build windows

package tray

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"scanssh-ray/backend/config"
	"scanssh-ray/backend/internal/testssh"
	"scanssh-ray/backend/scanner"
	"scanssh-ray/backend/tunnel"
)

type testUIBlock struct{ entered, release chan struct{} }

func testIdentity(t *testing.T) windows.GUID {
	t.Helper()
	// Windows remembers GUID/path associations after a test binary exits.
	// A PID may be reused by a later test binary at another temporary path.
	identity, err := windows.GenerateGUID()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func nativeTestWindow(t *testing.T, pause ...*testUIBlock) uintptr {
	t.Helper()
	shellClass := windows.StringToUTF16Ptr("Shell_TrayWnd")
	if shell, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(shellClass)), 0); shell == 0 {
		t.Skip("Windows notification area requires an interactive Explorer session")
	}
	started := make(chan uintptr, 1)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		instance, _, _ := getModule.Call(0)
		className := windows.StringToUTF16Ptr("wailsWindow")
		callback := windows.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
			if msg == 0x8050 && len(pause) > 0 {
				close(pause[0].entered)
				<-pause[0].release
				return 0
			}
			if msg == wmClose {
				destroyWindow.Call(hwnd)
				postQuitMessage.Call(0)
				return 0
			}
			result, _, _ := defWindowProc.Call(hwnd, msg, wp, lp)
			return result
		})
		class := windowClass{Procedure: callback, Instance: instance, ClassName: className}
		class.Size = uint32(unsafe.Sizeof(class))
		if registered, _, _ := registerClass.Call(uintptr(unsafe.Pointer(&class))); registered == 0 {
			started <- 0
			return
		}
		defer unregisterClass.Call(uintptr(unsafe.Pointer(className)), instance)
		hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(className)), 0, 0x00CF0000, 0, 0, 500, 350, 0, 0, instance, 0)
		started <- hwnd
		if hwnd == 0 {
			return
		}
		var msg message
		for {
			result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(result) <= 0 {
				return
			}
			translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
	hwnd := <-started
	if hwnd == 0 {
		t.Fatal("cannot create native test window")
	}
	t.Cleanup(func() {
		postMessage.Call(hwnd, wmClose, 0, 0)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("test window failed to stop")
		}
	})
	return hwnd
}

func TestShellClickNeverWaitsForTheMainUIThread(t *testing.T) {
	block := &testUIBlock{entered: make(chan struct{}), release: make(chan struct{})}
	hwnd := nativeTestWindow(t, block)
	ico, err := os.ReadFile(filepath.Join("..", "..", "build", "windows", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	identity := testIdentity(t)
	icon, err := newTray(ico, nil, identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(icon.Close)
	postMessage.Call(hwnd, 0x8050, 0, 0)
	select {
	case <-block.entered:
	case <-time.After(time.Second):
		t.Fatal("UI pause did not start")
	}
	var release sync.Once
	unblock := func() { release.Do(func() { close(block.release) }) }
	defer unblock()
	callbackDone := make(chan struct{})
	go func() {
		user32.NewProc("SendMessageW").Call(icon.window, wmTray, 0, ninSelect|(1<<16))
		close(callbackDone)
	}()
	select {
	case <-callbackDone:
	case <-time.After(200 * time.Millisecond):
		unblock()
		<-callbackDone
		t.Fatal("Explorer callback blocked waiting for ShowWindow on the main UI thread")
	}
	unblock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if visible, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd); visible != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("queued restoration did not show the window")
}

func TestNativeTrayHidesRestoresAndPreservesSSH(t *testing.T) {
	hwnd := nativeTestWindow(t)
	ico, err := os.ReadFile(filepath.Join("..", "..", "build", "windows", "icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	exit := make(chan struct{})
	var exitOnce sync.Once
	identity := testIdentity(t)
	icon, err := newTray(ico, func() { exitOnce.Do(func() { close(exit) }) }, identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(icon.Close)
	if registered, _, _ := notifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&icon.data))); registered == 0 {
		t.Fatal("Windows shell did not retain the notification icon")
	}
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(icon.data) != 976 {
		t.Fatal("NOTIFYICONDATAW layout is incorrect")
	}
	for id, expected := range map[uintptr]string{menuOpen: "Открыть", menuExit: "Выход"} {
		var text [64]uint16
		user32.NewProc("GetMenuStringW").Call(icon.menu, id, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)), 0)
		if got := windows.UTF16ToString(text[:]); got != expected {
			t.Fatalf("menu label %q, want %q", got, expected)
		}
	}

	server := testssh.Start(t)
	base := t.TempDir()
	cfg := &config.Config{BaseDir: base, EnvPath: filepath.Join(base, ".env"), User: "tester", SSHPort: server.Port, KeyPath: server.KeyPath, KnownHosts: filepath.Join(base, "known_hosts"), IPs: []string{"127.0.0.1"}, ProxyHost: "127.0.0.1", ProxyPort: 3128, ProxyAuth: "none", TestURL: "http://example.com", ConnectTimeout: time.Second, CommandTimeout: time.Second, SSHInterval: time.Hour, InternetInterval: time.Hour, Concurrency: 1}
	engine, err := scanner.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Close)
	engine.SetAuto(true)
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		for {
			connection, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer connection.Close(); _, _ = io.Copy(connection, connection) }()
		}
	}()
	free, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	forward := &tunnel.Manager{}
	t.Cleanup(forward.Close)
	if err := forward.Start(context.Background(), engine.SSH, "127.0.0.1", port, echo.Addr().String()); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", forward.State().LocalAddress, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	assertForward := func(text string) {
		t.Helper()
		_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := connection.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, len(text))
		if _, err := io.ReadFull(connection, reply); err != nil || string(reply) != text {
			t.Fatalf("SSH forwarding failed: %q %v", reply, err)
		}
		if !engine.Snapshot().Auto || !forward.State().Running {
			t.Fatal("background services stopped while window was hidden")
		}
	}
	visible := func() bool { result, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd); return result != 0 }
	waitVisible := func(want bool, action string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for visible() != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if visible() != want {
			t.Fatalf("%s: visible=%v, want %v", action, visible(), want)
		}
	}
	send := func(target, msg, wp, lp uintptr) { user32.NewProc("SendMessageW").Call(target, msg, wp, lp) }
	assertForward("before-hide")
	icon.Show()
	waitVisible(true, "show from notification thread")
	if err := icon.Hide(); err != nil {
		t.Fatal(err)
	}
	waitVisible(false, "hide to tray")
	assertForward("hidden-connection-still-works")
	send(icon.window, wmTray, 0, ninKeySelect|(1<<16))
	waitVisible(true, "version-4 keyboard selection")
	send(hwnd, wmSysCommand, scMinimise, 0)
	waitVisible(false, "system-menu minimise")
	assertForward("system-menu-minimise")
	send(icon.window, wmCommand, menuOpen, 0)
	waitVisible(true, "Open menu command")
	showWindow.Call(hwnd, 6) // Native ShowWindow(SW_MINIMIZE), like Wails' runtime.
	waitVisible(false, "native minimisation")
	assertForward("native-minimise")
	send(icon.window, wmTray, 0, ninSelect|(1<<16))
	waitVisible(true, "version-4 icon activation")
	for i := 0; i < 20; i++ {
		_ = icon.Hide()
		icon.Show()
	}
	waitVisible(true, "last request wins after rapid hide/show")
	if icon.data.GUID != identity || icon.data.Flags&0x20 == 0 {
		t.Fatal("icon must retain a stable GUID")
	}
	notifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&icon.data)))
	send(icon.window, uintptr(icon.taskbarCreated), 0, 0)
	if !icon.ready.Load() {
		t.Fatal("icon was not restored after TaskbarCreated")
	}
	if registered, _, _ := notifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&icon.data))); registered == 0 {
		t.Fatal("Explorer restart marked the tray ready without registering its icon")
	}
	assertForward("explorer-restart")
	send(icon.window, wmCommand, menuExit, 0)
	select {
	case <-exit:
	case <-time.After(time.Second):
		t.Fatal("Exit menu command did not reach lifecycle callback")
	}
	icon.Close()
	if err := icon.Hide(); err == nil {
		t.Fatal("closed tray allowed hiding the window")
	}
	current, _, _ := getWindowProc.Call(hwnd, gwlpWndProc)
	if current != icon.originalProcedure.Load() {
		t.Fatal("original native window procedure was not restored")
	}
	forward.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := connection.Write([]byte("after-shutdown")); err == nil {
		buffer := make([]byte, 1)
		if _, err := connection.Read(buffer); err == nil {
			t.Fatal("SSH connection remains active after shutdown")
		} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("SSH shutdown check timed out instead of observing a closed connection")
		}
	}
}
