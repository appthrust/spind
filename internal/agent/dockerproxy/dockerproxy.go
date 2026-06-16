package dockerproxy

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/suin/spind/internal/agent/vsock"
	"github.com/suin/spind/internal/shared/streamrelay"
)

const (
	vsockPort         = 10240
	dockerUnixPath    = "/var/run/docker.sock"
	connectionWorkers = 16
)

func Run(ctx context.Context) error {
	go func() {
		if err := vsock.Listen(ctx, vsockPort, handleConnection); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "spind-guest-agent docker-proxy: host-initiated vsock listener disabled: %v\n", err)
		}
	}()
	for range connectionWorkers {
		go maintainHostConnection(ctx)
	}
	<-ctx.Done()
	return nil
}

func maintainHostConnection(ctx context.Context) {
	for {
		dockerConn, err := net.DialTimeout("unix", dockerUnixPath, 5*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}
		vsockConn, err := vsock.ConnectHost(ctx, vsockPort)
		if err != nil {
			_ = dockerConn.Close()
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}
		streamrelay.RelayFramed(dockerConn, vsockConn)
	}
}

func handleConnection(vsockConn net.Conn) {
	dockerConn, err := net.DialTimeout("unix", dockerUnixPath, 5*time.Second)
	if err != nil {
		_ = vsockConn.Close()
		return
	}
	streamrelay.RelayFramed(dockerConn, vsockConn)
}
