package testssh

import (
	"io"
	"net"
	"sync"
	"testing"
)

// Stall accepts TCP but never completes the SSH handshake. It lets tests wait
// for a real in-progress connection without sleeps or external hosts.
func Stall(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	var mu sync.Mutex
	var connection net.Conn
	closed := false
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		closed = true
		if connection != nil {
			_ = connection.Close()
		}
		mu.Unlock()
	})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		mu.Lock()
		if closed {
			mu.Unlock()
			return
		}
		connection = conn
		close(accepted)
		mu.Unlock()
		_, _ = io.Copy(io.Discard, conn)
	}()
	return listener.Addr().(*net.TCPAddr).Port, accepted
}
