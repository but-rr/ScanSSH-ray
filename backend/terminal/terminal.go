// Package terminal opens a separate interactive console for a selected SSH host.
package terminal

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Options struct {
	IP, User, KeyPath, KnownHosts string
	Port                          int
}

func (o Options) arguments() ([]string, error) {
	if net.ParseIP(o.IP) == nil || o.Port < 1 || o.Port > 65535 || strings.TrimSpace(o.User) == "" || o.KnownHosts == "" {
		return nil, fmt.Errorf("проверьте адрес ПК и профиль SSH")
	}
	for _, value := range []string{o.User, o.KeyPath, o.KnownHosts} {
		if strings.ContainsAny(value, "\x00\r\n") {
			return nil, fmt.Errorf("недопустимые символы в профиле SSH")
		}
	}
	if strings.ContainsAny(o.KnownHosts, "\"") {
		return nil, fmt.Errorf("недопустимый путь к known_hosts")
	}
	// Arguments go directly to ssh.exe, never through a local command shell.
	knownHosts := strings.ReplaceAll(o.KnownHosts, `\`, "/")
	args := []string{"-F", "NUL", "-t", "-o", "StrictHostKeyChecking=accept-new", "-o", `UserKnownHostsFile="` + knownHosts + `"`, "-o", "IdentitiesOnly=yes", "-p", strconv.Itoa(o.Port), "-l", o.User}
	if o.KeyPath != "" {
		args = append(args, "-i", o.KeyPath)
	}
	return append(args, o.IP, "cmd.exe /d"), nil
}