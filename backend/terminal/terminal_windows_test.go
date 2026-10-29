//go:build windows

package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/windows"
)

type consoleProbe struct {
	Interactive bool     `json:"interactive"`
	Args        []string `json:"args"`
}

func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == "--console-probe" {
		input, inputErr := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
		output, outputErr := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
		var mode uint32
		interactive := inputErr == nil && outputErr == nil && windows.GetConsoleMode(input, &mode) == nil && windows.GetConsoleMode(output, &mode) == nil
		data, _ := json.Marshal(consoleProbe{interactive, os.Args[3:]})
		if err := os.WriteFile(os.Args[2], data, 0600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNewConsoleHasInteractiveHandlesAndPreservesArguments(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result := filepath.Join(t.TempDir(), "console probe.json")
	args := []string{`C:\keys with spaces\a&b`, `domain\operator`, `a"b`, `%PATH%`, "пароль не передаётся"}
	process, err := createConsole(executable, append([]string{"--console-probe", result}, args...), "ScanSSH-ray console test", true)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	status, err := windows.WaitForSingleObject(process.Process, 60000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		_ = windows.TerminateProcess(process.Process, 1)
		t.Fatalf("console probe did not exit: status=%d err=%v", status, err)
	}
	data, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var probe consoleProbe
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatal(err)
	}
	if !probe.Interactive || !reflect.DeepEqual(probe.Args, args) {
		t.Fatalf("new console lost standard handles or arguments: %#v", probe)
	}
}
