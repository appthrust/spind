package relay

import (
	"bufio"
	"io"
	"net"
)

func Plain(left net.Conn, right net.Conn) {
	done := make(chan struct{}, 2)
	go copyAndClose(left, right, done)
	go copyAndClose(right, left, done)
	<-done
	_ = left.Close()
	_ = right.Close()
	<-done
}

func Buffered(buffered *bufio.Reader, vsockConn net.Conn, target net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(target, buffered)
		closeWrite(target)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(vsockConn, target)
		closeWrite(vsockConn)
		done <- struct{}{}
	}()
	<-done
	<-done
	_ = target.Close()
}

func copyAndClose(dst net.Conn, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	closeWrite(dst)
	done <- struct{}{}
}

func closeWrite(conn net.Conn) {
	if closer, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
		return
	}
	_ = conn.Close()
}
