//go:build linux

package vsock

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const HostCID = 2

func Listen(ctx context.Context, port uint32, handle func(net.Conn)) error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("create listening vsock socket: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("configure listening vsock socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		return fmt.Errorf("bind listening vsock socket: %w", err)
	}
	if err := unix.Listen(fd, 16); err != nil {
		return fmt.Errorf("listen on vsock: %w", err)
	}
	for {
		acceptedFD, _, err := unix.Accept(fd)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("accept vsock connection: %w", err)
		}
		go handle(&fileConn{File: os.NewFile(uintptr(acceptedFD), fmt.Sprintf("vsock:%d", port)), port: port})
	}
}

func ConnectHost(ctx context.Context, port uint32) (net.Conn, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("create vsock socket: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("configure vsock socket: %w", err)
	}
	if err := unix.Connect(fd, &unix.SockaddrVM{CID: HostCID, Port: port}); err != nil {
		if !errors.Is(err, unix.EINPROGRESS) {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("connect host vsock: %w", err)
		}
		if err := waitForConnect(ctx, fd, time.Second); err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("connect host vsock: %w", err)
		}
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("configure connected vsock socket: %w", err)
	}
	select {
	case <-ctx.Done():
		_ = unix.Close(fd)
		return nil, ctx.Err()
	default:
	}
	return &fileConn{File: os.NewFile(uintptr(fd), fmt.Sprintf("vsock:%d", port)), port: port}, nil
}

func waitForConnect(ctx context.Context, fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return os.ErrDeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pollTimeout := int(remaining / time.Millisecond)
		if pollTimeout < 1 {
			pollTimeout = 1
		}
		events := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
		n, err := unix.Poll(events, pollTimeout)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if n == 0 {
			return os.ErrDeadlineExceeded
		}
		socketErr, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			return err
		}
		if socketErr != 0 {
			return syscall.Errno(socketErr)
		}
		return nil
	}
}

type fileConn struct {
	*os.File
	port uint32
}

func (c *fileConn) LocalAddr() net.Addr {
	return Addr(c.port)
}

func (c *fileConn) RemoteAddr() net.Addr {
	return Addr(c.port)
}

func (c *fileConn) SetDeadline(time.Time) error {
	return nil
}

func (c *fileConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *fileConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *fileConn) CloseWrite() error {
	return unix.Shutdown(int(c.Fd()), unix.SHUT_WR)
}

type Addr uint32

func (a Addr) Network() string {
	return "vsock"
}

func (a Addr) String() string {
	return fmt.Sprintf("vsock:%d", a)
}
