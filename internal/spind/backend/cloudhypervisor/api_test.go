package cloudhypervisor

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDialVsockAcceptsConnectOK(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response string
	}{
		{name: "plain OK", response: "OK\n"},
		{name: "OK with local port", response: "OK 1073741882\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			socketPath := testUnixSocketPath(t)
			listener, err := net.Listen("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()

			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					done <- err
					return
				}
				if line != "CONNECT 10222\n" {
					t.Errorf("CONNECT request = %q", line)
				}
				_, err = conn.Write([]byte(tt.response))
				done <- err
			}()

			conn, err := DialVsock(context.Background(), socketPath, 10222)
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDialVsockRejectsConnectError(t *testing.T) {
	socketPath := testUnixSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_, err = bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		_, err = conn.Write([]byte("ERR refused\n"))
		done <- err
	}()

	conn, err := DialVsock(context.Background(), socketPath, 10222)
	if err == nil {
		conn.Close()
		t.Fatal("expected error")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testUnixSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "spind-ch-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	})
	return filepath.Join(dir, "sock")
}
