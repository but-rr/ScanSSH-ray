//go:build windows

package terminal

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func Open(o Options) error {
	args, err := o.arguments()
	if err != nil {
		return err
	}
	sshPath, err := exec.LookPath("ssh.exe")
	if err != nil {
		return fmt.Errorf("не найден ssh.exe: установите компонент Windows «Клиент OpenSSH»")
	}
	process, err := createConsole(sshPath, args, "ScanSSH-ray · CMD · "+o.IP, false)
	if err != nil {
		return fmt.Errorf("не удалось открыть консоль SSH")
	}
	// The console is independent of the GUI and closes when its SSH session ends.
	_ = windows.CloseHandle(process.Thread)
	_ = windows.CloseHandle(process.Process)
	return nil
}

func createConsole(application string, args []string, title string, hidden bool) (*windows.ProcessInformation, error) {
	app, err := windows.UTF16PtrFromString(application)
	if err != nil {
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{application}, args...)))
	if err != nil {
		return nil, err
	}
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return nil, err
	}
	startup := windows.StartupInfo{Title: caption}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	if hidden {
		startup.Flags = windows.STARTF_USESHOWWINDOW
		startup.ShowWindow = windows.SW_HIDE
	}
	// Do not set STARTF_USESTDHANDLES: Windows must create real console handles,
	// rather than inheriting the GUI's null/redirected standard input and output.
	var process windows.ProcessInformation
	if err := windows.CreateProcess(app, commandLine, nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, nil, &startup, &process); err != nil {
		return nil, err
	}
	return &process, nil
}
