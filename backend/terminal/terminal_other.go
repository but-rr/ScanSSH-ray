//go:build !windows

package terminal

import "fmt"

func Open(o Options) error {
	return fmt.Errorf("консоль CMD доступна только в Windows")
}
