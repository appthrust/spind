package streamrelay

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestRelayFramedPreservesHalfClose(t *testing.T) {
	client, leftLocal := tcpPair(t)
	rightLocal, server := tcpPair(t)
	framedLeft, framedRight := net.Pipe()

	deadline := time.Now().Add(5 * time.Second)
	for _, conn := range []*net.TCPConn{client, leftLocal, rightLocal, server} {
		if err := conn.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
	}

	leftDone := make(chan struct{})
	rightDone := make(chan struct{})
	go func() {
		RelayFramed(leftLocal, framedLeft)
		close(leftDone)
	}()
	go func() {
		RelayFramed(rightLocal, framedRight)
		close(rightDone)
	}()

	serverErr := make(chan error, 1)
	serverReceived := make(chan string, 1)
	go func() {
		request, err := io.ReadAll(server)
		if err != nil {
			serverErr <- err
			return
		}
		serverReceived <- string(request)
		time.Sleep(100 * time.Millisecond)
		if _, err := server.Write([]byte("response")); err != nil {
			serverErr <- err
			return
		}
		if err := server.CloseWrite(); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-serverReceived:
		if got != "request" {
			t.Fatalf("server received %q, want request", got)
		}
	case err := <-serverErr:
		t.Fatalf("server failed before receiving request: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive request")
	}

	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "response" {
		t.Fatalf("client received %q, want response", response)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	waitForRelay(t, leftDone)
	waitForRelay(t, rightDone)
}

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan *net.TCPConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptTCP()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case server := <-accepted:
		t.Cleanup(func() { _ = client.Close() })
		t.Cleanup(func() { _ = server.Close() })
		return client, server
	case err := <-acceptErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out accepting TCP connection")
	}
	return nil, nil
}

func waitForRelay(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not finish")
	}
}
