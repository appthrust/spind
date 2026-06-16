package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/suin/spind/internal/shared/streamrelay"
	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/sshrelay"
)

func RunRelay(ctx context.Context, endpointPath string, vsockPath string, guestPort uint32) error {
	if endpointPath == "" {
		return errors.New("Docker relay endpoint path is missing")
	}
	if vsockPath == "" {
		return errors.New("Docker relay vsock path is missing")
	}
	if guestPort == 0 {
		return errors.New("Docker relay guest port is missing")
	}
	_ = os.Remove(endpointPath)
	listener, err := net.Listen("unix", endpointPath)
	if err != nil {
		return fmt.Errorf("listen Docker endpoint: %w", err)
	}
	defer listener.Close()
	defer os.Remove(endpointPath)

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
			return fmt.Errorf("accept Docker endpoint connection: %w", err)
		}
		go handleRelayConnection(ctx, localConn, vsockPath, guestPort)
	}
}

func handleRelayConnection(ctx context.Context, localConn net.Conn, vsockPath string, guestPort uint32) {
	guestConn, err := cloudhypervisor.DialVsock(ctx, vsockPath, guestPort)
	if err != nil {
		_ = localConn.Close()
		return
	}
	streamrelay.RelayFramed(localConn, guestConn)
}

func RunPortRelay(ctx context.Context, dockerEndpointPath string, guestIP string, tcpForwardPath string, vsockPath string, guestPort uint32, sshSocketPath string, sshKeyPath string, sshUser string) error {
	if dockerEndpointPath == "" {
		return errors.New("Docker endpoint path is missing")
	}
	if guestIP == "" && tcpForwardPath == "" && vsockPath == "" && sshSocketPath == "" {
		return errors.New("Docker TCP forward endpoint is missing")
	}
	if guestPort == 0 {
		return errors.New("Docker TCP forward guest port is missing")
	}
	relay := &portRelay{
		dockerEndpointPath: dockerEndpointPath,
		guestIP:            guestIP,
		tcpForwardPath:     tcpForwardPath,
		vsockPath:          vsockPath,
		guestPort:          guestPort,
		sshSocketPath:      sshSocketPath,
		sshKeyPath:         sshKeyPath,
		sshUser:            sshUser,
		listeners:          map[uint16]net.Listener{},
	}
	return relay.run(ctx)
}

type portRelay struct {
	dockerEndpointPath string
	guestIP            string
	tcpForwardPath     string
	vsockPath          string
	guestPort          uint32
	sshSocketPath      string
	sshKeyPath         string
	sshUser            string
	listeners          map[uint16]net.Listener
}

func (r *portRelay) run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer r.closeAll()

	for {
		ports := PublishedPortsRaw(ctx, r.dockerEndpointPath)
		r.reconcile(ctx, ports)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *portRelay) reconcile(ctx context.Context, ports []uint16) {
	wanted := map[uint16]struct{}{}
	for _, port := range ports {
		wanted[port] = struct{}{}
		if _, ok := r.listeners[port]; ok {
			continue
		}
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(int(port)))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: listen published Docker port %d: %v\n", port, err)
			continue
		}
		r.listeners[port] = listener
		go r.accept(ctx, port, listener)
	}
	for port, listener := range r.listeners {
		if _, ok := wanted[port]; ok {
			continue
		}
		_ = listener.Close()
		delete(r.listeners, port)
	}
}

func (r *portRelay) accept(ctx context.Context, port uint16, listener net.Listener) {
	for {
		localConn, err := listener.Accept()
		if err != nil {
			return
		}
		go r.handle(ctx, port, localConn)
	}
}

func (r *portRelay) handle(ctx context.Context, port uint16, localConn net.Conn) {
	guestConn, err := r.dialGuest(ctx, port)
	if err != nil {
		_ = localConn.Close()
		return
	}
	streamrelay.RelayRaw(localConn, guestConn)
}

func (r *portRelay) dialGuest(ctx context.Context, port uint16) (net.Conn, error) {
	var conn net.Conn
	var err error
	if r.guestIP != "" {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		return dialer.DialContext(ctx, "tcp", net.JoinHostPort(r.guestIP, strconv.Itoa(int(port))))
	}
	if r.tcpForwardPath != "" {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err = dialer.DialContext(ctx, "unix", r.tcpForwardPath)
	} else if r.sshSocketPath != "" {
		return sshrelay.Dial(ctx, r.sshSocketPath, r.sshKeyPath, r.sshUser, "127.0.0.1", int(port))
	} else {
		conn, err = cloudhypervisor.DialVsock(ctx, r.vsockPath, r.guestPort)
	}
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "CONNECT 127.0.0.1 %d\n", port); err != nil {
		_ = conn.Close()
		return nil, err
	}
	buffer := make([]byte, 3)
	if _, err := io.ReadFull(conn, buffer); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if string(buffer) != "OK\n" {
		_ = conn.Close()
		return nil, fmt.Errorf("guest TCP forward rejected port %d", port)
	}
	return conn, nil
}

func (r *portRelay) closeAll() {
	for port, listener := range r.listeners {
		_ = listener.Close()
		delete(r.listeners, port)
	}
}
