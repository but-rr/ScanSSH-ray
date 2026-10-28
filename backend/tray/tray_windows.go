//go:build windows

// Package tray owns the Windows notification icon and its message loop.
// It never owns network connections or terminates the process itself.
package tray

import (
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmClose       = 0x0010
	wmSize        = 0x0005
	wmCommand     = 0x0111
	wmSysCommand  = 0x0112
	wmTray        = 0x8001
	wmLeftUp      = 0x0202
	wmLeftDouble  = 0x0203
	wmRightUp     = 0x0205
	wmContextMenu = 0x007B
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401
	scMinimise    = 0xF020
	sizeMinimised = 1
	menuOpen      = 1
	menuExit      = 2
	nimAdd        = 0
	nimDelete     = 2
	nimVersion    = 4
	gwlpWndProc   = ^uintptr(3) // -4, without a signed-to-unsigned conversion.
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	unregisterClass  = user32.NewProc("UnregisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	callWindowProc   = user32.NewProc("CallWindowProcW")
	getWindowProc    = user32.NewProc("GetWindowLongPtrW")
	setWindowProc    = user32.NewProc("SetWindowLongPtrW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	registerMessage  = user32.NewProc("RegisterWindowMessageW")
	enumWindows      = user32.NewProc("EnumWindows")
	getWindowProcess = user32.NewProc("GetWindowThreadProcessId")
	getClassName     = user32.NewProc("GetClassNameW")
	showWindow       = user32.NewProc("ShowWindow")
	isIconic         = user32.NewProc("IsIconic")
	isWindow         = user32.NewProc("IsWindow")
	setForeground    = user32.NewProc("SetForegroundWindow")
	createPopup      = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	destroyMenu      = user32.NewProc("DestroyMenu")
	getCursor        = user32.NewProc("GetCursorPos")
	trackPopup       = user32.NewProc("TrackPopupMenu")
	createIcon       = user32.NewProc("CreateIconFromResourceEx")
	destroyIcon      = user32.NewProc("DestroyIcon")
	notifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	getModule        = kernel32.NewProc("GetModuleHandleW")
)

type point struct{ X, Y int32 }
type message struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type windowClass struct {
	Size, Style                        uint32
	Procedure                          uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type iconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

type Tray struct {
	mainWindow, window, icon, menu, instance uintptr
	callback, mainCallback                   uintptr
	originalProcedure                        atomic.Uintptr
	taskbarCreated                           uint32
	className                                *uint16
	data                                     iconData
	ready                                    atomic.Bool
	closing                                  atomic.Bool
	stop                                     sync.Once
	done                                     chan struct{}
	onExit                                   func()
}

// New returns only once an actual notification icon and menu are available.
func New(ico []byte, onExit func()) (*Tray, error) {
	image, err := iconImage(ico)
	if err != nil {
		return nil, err
	}
	main := findMainWindow()
	if main == 0 {
		return nil, fmt.Errorf("не найдено окно ScanSSH-ray")
	}
	t := &Tray{mainWindow: main, done: make(chan struct{}), onExit: onExit}
	started := make(chan error, 1)
	go t.run(image, started)
	if err = <-started; err != nil {
		<-t.done
		return nil, err
	}
	return t, nil
}

func (t *Tray) run(image []byte, started chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)
	defer t.cleanup()
	if err := t.init(image); err != nil {
		started <- err
		return
	}
	started <- nil
	var msg message
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (t *Tray) init(image []byte) error {
	// The application is built for windows/amd64; choose the matching Win32
	// pointer API explicitly if a 32-bit developer build is ever used.
	if unsafe.Sizeof(uintptr(0)) == 4 {
		getWindowProc = user32.NewProc("GetWindowLongW")
		setWindowProc = user32.NewProc("SetWindowLongW")
	}
	t.instance, _, _ = getModule.Call(0)
	t.className = windows.StringToUTF16Ptr(fmt.Sprintf("ScanSSH-ray.Tray.%d", t.mainWindow))
	t.callback = windows.NewCallback(t.windowProcedure)
	wc := windowClass{Procedure: t.callback, Instance: t.instance, ClassName: t.className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if result, _, err := registerClass.Call(uintptr(unsafe.Pointer(&wc))); result == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}
	t.window, _, _ = createWindow.Call(0, uintptr(unsafe.Pointer(t.className)), 0, 0, 0, 0, 0, 0, 0, 0, t.instance, 0)
	if t.window == 0 {
		return fmt.Errorf("не удалось создать окно трея")
	}
	t.icon, _, _ = createIcon.Call(uintptr(unsafe.Pointer(&image[0])), uintptr(len(image)), 1, 0x00030000, 32, 32, 0)
	runtime.KeepAlive(image)
	if t.icon == 0 {
		return fmt.Errorf("не удалось загрузить иконку трея")
	}
	t.menu, _, _ = createPopup.Call()
	if t.menu == 0 {
		return fmt.Errorf("не удалось создать меню трея")
	}
	for _, entry := range []struct {
		flags, id uintptr
		text      string
	}{{0, menuOpen, "Открыть"}, {0x0800, 0, ""}, {0, menuExit, "Выход"}} {
		label := windows.StringToUTF16Ptr(entry.text)
		if result, _, err := appendMenu.Call(t.menu, entry.flags, entry.id, uintptr(unsafe.Pointer(label))); result == 0 {
			return fmt.Errorf("AppendMenuW: %w", err)
		}
	}
	created := windows.StringToUTF16Ptr("TaskbarCreated")
	msg, _, _ := registerMessage.Call(uintptr(unsafe.Pointer(created)))
	t.taskbarCreated = uint32(msg)
	t.data = iconData{Window: t.window, ID: 1, Flags: 1 | 2 | 4 | 0x80, Callback: wmTray, Icon: t.icon, Version: 4}
	t.data.Size = uint32(unsafe.Sizeof(t.data))
	copy(t.data.Tip[:], windows.StringToUTF16("ScanSSH-ray"))
	if err := t.addIcon(); err != nil {
		return err
	}
	// Intercept native minimisation too (taskbar/system menu/WindowMinimise).
	// Forward all other messages to Wails' original procedure unchanged.
	original, _, _ := getWindowProc.Call(t.mainWindow, gwlpWndProc)
	if original == 0 {
		return fmt.Errorf("не удалось прочитать обработчик окна")
	}
	t.originalProcedure.Store(original)
	t.mainCallback = windows.NewCallback(t.mainProcedure)
	if result, _, err := setWindowProc.Call(t.mainWindow, gwlpWndProc, t.mainCallback); result == 0 {
		return fmt.Errorf("SetWindowLongPtrW: %w", err)
	}
	t.ready.Store(true)
	return nil
}

func (t *Tray) addIcon() error {
	if result, _, err := notifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&t.data))); result == 0 {
		return fmt.Errorf("Shell_NotifyIconW: %w", err)
	}
	notifyIcon.Call(nimVersion, uintptr(unsafe.Pointer(&t.data)))
	return nil
}

func (t *Tray) windowProcedure(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case wmTray:
		switch uint32(lParam & 0xFFFF) {
		case wmLeftUp, wmLeftDouble, ninSelect, ninKeySelect:
			t.Show()
		case wmRightUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case wmCommand:
		t.command(wParam & 0xFFFF)
		return 0
	case wmClose:
		postQuitMessage.Call(0)
		return 0
	}
	if t.taskbarCreated != 0 && uint32(msg) == t.taskbarCreated && !t.closing.Load() {
		t.ready.Store(false)
		if err := t.addIcon(); err != nil {
			log.Printf("Восстановление трея: %v", err)
			t.Show() // A visible window remains recoverable if Explorer rejects the icon.
		} else {
			t.ready.Store(true)
		}
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func (t *Tray) mainProcedure(hwnd, msg, wParam, lParam uintptr) uintptr {
	if !t.closing.Load() && t.ready.Load() {
		if uint32(msg) == wmSysCommand && wParam&0xFFF0 == scMinimise {
			_ = t.Hide()
			return 0
		}
		if uint32(msg) == wmSize && wParam == sizeMinimised {
			_ = t.Hide()
		}
	}
	result, _, _ := callWindowProc.Call(t.originalProcedure.Load(), hwnd, msg, wParam, lParam)
	return result
}

func (t *Tray) showMenu() {
	var cursor point
	getCursor.Call(uintptr(unsafe.Pointer(&cursor)))
	setForeground.Call(t.window)
	id, _, _ := trackPopup.Call(t.menu, 0x0100|0x0002, uintptr(cursor.X), uintptr(cursor.Y), 0, t.window, 0)
	postMessage.Call(t.window, 0, 0, 0) // Allow the menu to dismiss on outside clicks.
	t.command(id)
}

func (t *Tray) command(id uintptr) {
	if t.closing.Load() {
		return
	}
	switch id {
	case menuOpen:
		t.Show()
	case menuExit:
		if t.onExit != nil {
			go t.onExit()
		}
	}
}

func (t *Tray) Hide() error {
	if t == nil || t.closing.Load() || !t.ready.Load() {
		return fmt.Errorf("трей пока недоступен — окно остаётся открытым")
	}
	showWindow.Call(t.mainWindow, 0) // SW_HIDE removes the taskbar button too.
	return nil
}

func (t *Tray) Show() {
	if t == nil || t.closing.Load() {
		return
	}
	if iconic, _, _ := isIconic.Call(t.mainWindow); iconic != 0 {
		showWindow.Call(t.mainWindow, 9) // SW_RESTORE restores the pre-minimise state.
	} else {
		showWindow.Call(t.mainWindow, 5) // SW_SHOW keeps a maximised window maximised.
	}
	setForeground.Call(t.mainWindow)
}

func (t *Tray) Close() {
	if t == nil {
		return
	}
	t.stop.Do(func() { t.closing.Store(true); postMessage.Call(t.window, wmClose, 0, 0) })
	<-t.done
}

func (t *Tray) cleanup() {
	t.closing.Store(true)
	t.ready.Store(false)
	if t.mainCallback != 0 {
		if valid, _, _ := isWindow.Call(t.mainWindow); valid != 0 {
			if current, _, _ := getWindowProc.Call(t.mainWindow, gwlpWndProc); current == t.mainCallback {
				setWindowProc.Call(t.mainWindow, gwlpWndProc, t.originalProcedure.Load())
			}
		}
	}
	if t.window != 0 {
		notifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&t.data)))
		destroyWindow.Call(t.window)
	}
	if t.menu != 0 {
		destroyMenu.Call(t.menu)
	}
	if t.icon != 0 {
		destroyIcon.Call(t.icon)
	}
	if t.className != nil {
		unregisterClass.Call(uintptr(unsafe.Pointer(t.className)), t.instance)
	}
}

func findMainWindow() uintptr {
	var found uintptr
	callback := windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		getWindowProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid != windows.GetCurrentProcessId() {
			return 1
		}
		var class [128]uint16
		getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if windows.UTF16ToString(class[:]) != "wailsWindow" {
			return 1
		}
		found = hwnd
		return 0
	})
	enumWindows.Call(callback, 0)
	return found
}
