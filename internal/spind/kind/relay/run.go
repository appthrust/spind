package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/suin/spind/internal/shared/streamrelay"
	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/sshrelay"
)

func Run(ctx context.Context, options Options, stderr io.Writer) int {
	if err := RunRelay(ctx, options.ListenPort, options.GuestIP, options.TCPForward, options.Vsock, options.GuestPort, options.TargetPort, options.SSHSocket, options.SSHKey, options.SSHUser); err != nil {
		fmt.Fprintf(stderr, "spind kubernetes-relay: %v\n", err)
		return 1
	}
	return 0
}

func RunRelay(ctx context.Context, listenPort int, guestIP string, tcpForwardPath string, vsockPath string, guestPort uint32, targetPort int, sshSocketPath string, sshKeyPath string, sshUser string) error {
	if listenPort <= 0 || listenPort > 65535 {
		return errors.New("Kubernetes relay listen port is invalid")
	}
	if targetPort <= 0 || targetPort > 65535 {
		return errors.New("Kubernetes relay target port is invalid")
	}
	if guestIP == "" && tcpForwardPath == "" && vsockPath == "" && sshSocketPath == "" {
		return errors.New("Kubernetes relay endpoint is missing")
	}
	if guestIP == "" && tcpForwardPath == "" && vsockPath != "" && guestPort == 0 {
		return errors.New("Kubernetes relay guest port is missing")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort)))
	if err != nil {
		return fmt.Errorf("listen Kubernetes API relay: %w", err)
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		localConn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept Kubernetes API relay connection: %w", err)
		}
		go handleRelayConnection(ctx, localConn, guestIP, tcpForwardPath, vsockPath, guestPort, targetPort, sshSocketPath, sshKeyPath, sshUser)
	}
}

func handleRelayConnection(ctx context.Context, localConn net.Conn, guestIP string, tcpForwardPath string, vsockPath string, guestPort uint32, targetPort int, sshSocketPath string, sshKeyPath string, sshUser string) {
	var guestConn net.Conn
	var err error
	if guestIP != "" {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		guestConn, err = dialer.DialContext(ctx, "tcp", net.JoinHostPort(guestIP, strconv.Itoa(targetPort)))
	} else {
		if tcpForwardPath != "" {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			guestConn, err = dialer.DialContext(ctx, "unix", tcpForwardPath)
		} else if sshSocketPath != "" {
			guestConn, err = sshrelay.Dial(ctx, sshSocketPath, sshKeyPath, sshUser, "127.0.0.1", targetPort)
		} else {
			guestConn, err = cloudhypervisor.DialVsock(ctx, vsockPath, guestPort)
		}
		if err == nil && sshSocketPath == "" {
			if _, err = fmt.Fprintf(guestConn, "CONNECT 127.0.0.1 %d\n", targetPort); err == nil {
				buffer := make([]byte, 3)
				if _, err = io.ReadFull(guestConn, buffer); err == nil && string(buffer) != "OK\n" {
					err = fmt.Errorf("guest TCP forward rejected port %d", targetPort)
				}
			}
		}
	}
	if err != nil {
		_ = localConn.Close()
		if guestConn != nil {
			_ = guestConn.Close()
		}
		return
	}
	streamrelay.RelayRaw(localConn, guestConn)
}
