//go:build !windows

package tray

import "fmt"

type Tray struct{}

func New(_ []byte, _ func()) (*Tray, error) {
	return nil, fmt.Errorf("трей поддерживается только в Windows")
}
func (*Tray) Hide() error { return fmt.Errorf("трей недоступен") }
func (*Tray) Show()       {}
func (*Tray) Close()      {}
