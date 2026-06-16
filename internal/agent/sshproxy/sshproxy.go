package sshproxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/suin/spind/internal/agent/relay"
	"github.com/suin/spind/internal/agent/vsock"
)

const (
	vsockPort = 10222
	localAddr = "127.0.0.1:22"
)

func Run(ctx context.Context) error {
	go func() {
		if err := vsock.Listen(ctx, vsockPort, handleConnection); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "spind-guest-agent ssh-proxy: host-initiated vsock listener disabled: %v\n", err)
		}
	}()

	for {
		sshConn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		vsockConn, err := vsock.ConnectHost(ctx, vsockPort)
		if err != nil {
			_ = sshConn.Close()
			if ctx.Err() != nil {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}
		relay.Plain(vsockConn, sshConn)
	}
}

func handleConnection(vsockConn net.Conn) {
	sshConn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		_ = vsockConn.Close()
		return
	}
	relay.Plain(vsockConn, sshConn)
}
