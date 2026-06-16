//go:build linux

package mounthelper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/suin/spind/internal/agent/vsock"
	"golang.org/x/sys/unix"
)

const (
	vsockPort         = 10242
	connectionWorkers = 4
	virtioFSTag       = "spind-cwd"
)

func Run(ctx context.Context) error {
	go func() {
		if err := vsock.Listen(ctx, vsockPort, handleConn); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "spind-guest-agent mount-helper: host-initiated vsock listener disabled: %v\n", err)
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
		conn, err := vsock.ConnectHost(ctx, vsockPort)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}
		handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	if err := handleRequest(strings.TrimSpace(line)); err != nil {
		fmt.Fprintf(conn, "ERR %s\n", err)
		return
	}
	fmt.Fprintln(conn, "OK")
}

func handleRequest(line string) error {
	op, target, ok := strings.Cut(line, " ")
	if !ok {
		return errors.New("invalid request")
	}
	target, err := cleanMountPath(target)
	if err != nil {
		return err
	}
	switch op {
	case "MOUNT":
		return mountShare(target)
	case "UNMOUNT":
		return unmountShare(target)
	case "STATUS":
		if !isMountPoint(target) {
			return errors.New("not mounted")
		}
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", op)
	}
}

func cleanMountPath(value string) (string, error) {
	if value == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("path must be absolute")
	}
	clean := filepath.Clean(value)
	if clean == "/" {
		return "", errors.New("refusing to mount /")
	}
	for _, part := range strings.Split(clean, string(os.PathSeparator)) {
		if part == ".." {
			return "", errors.New("path must not contain ..")
		}
	}
	return clean, nil
}

func mountShare(target string) error {
	if isMountPoint(target) {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("create mount point: %w", err)
	}
	if output, err := exec.Command("mount", "-t", "virtiofs", virtioFSTag, target).CombinedOutput(); err != nil {
		return fmt.Errorf("mount virtiofs: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func unmountShare(target string) error {
	if !isMountPoint(target) {
		return nil
	}
	if err := unix.Unmount(target, 0); err != nil {
		return fmt.Errorf("unmount: %w", err)
	}
	return nil
}

func isMountPoint(target string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[4] == target {
			return true
		}
	}
	return false
}
