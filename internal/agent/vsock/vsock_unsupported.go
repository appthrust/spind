//go:build !linux

package vsock

import (
	"context"
	"errors"
	"net"
)

func Listen(context.Context, uint32, func(net.Conn)) error {
	return errors.New("vsock is supported on Linux guests only")
}

func ConnectHost(context.Context, uint32) (net.Conn, error) {
	return nil, errors.New("vsock is supported on Linux guests only")
}
