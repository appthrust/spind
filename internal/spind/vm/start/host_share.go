package start

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
	"syscall"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/backend/vz"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func currentHostSharePath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

func prepareHostShareConfig(config *vz.RunnerConfig) {
	path, err := currentHostSharePath()
	if err != nil {
		config.HostSharePath = ""
		return
	}
	config.HostSharePath = path
}

func prepareCloudHypervisorHostShareConfig(config *cloudhypervisor.Config) error {
	path, err := currentHostSharePath()
	if err != nil {
		config.HostSharePath = ""
		return err
	}
	config.HostSharePath = path
	return nil
}

func (m *Manager) configureHostShare(ctx context.Context, metadata spindvm.Metadata, state spindvm.State) spindvm.State {
	state.HostShareReady = false
	state.HostShareLastError = ""
	if state.HostSharePath == "" {
		return state
	}
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if state.HostShareMountSocketPath == "" {
			state.HostShareLastError = "host share mount socket path is missing"
			return state
		}
		if err := sendHostShareRequest(ctx, state.HostShareMountSocketPath, "MOUNT", state.HostSharePath); err != nil {
			state.HostShareLastError = err.Error()
			return state
		}
	case BackendCloudHypervisor:
		if state.CloudHypervisorVsockSocketPath == "" {
			state.HostShareLastError = "Cloud Hypervisor vsock socket path is missing"
			return state
		}
		if state.HostShareLastError != "" && (state.HostShareVirtioFSPID == 0 || !processAlive(state.HostShareVirtioFSPID)) {
			return state
		}
		if state.HostShareVirtioFSPID == 0 || !processAlive(state.HostShareVirtioFSPID) {
			state.HostShareLastError = "virtiofsd is not running"
			return state
		}
		if err := sendHostShareVsockRequest(ctx, state.CloudHypervisorVsockSocketPath, state.HostShareGuestPort, "MOUNT", state.HostSharePath); err != nil {
			state.HostShareLastError = err.Error()
			return state
		}
	default:
		state.HostShareLastError = "host path sharing is unavailable for this backend"
		return state
	}
	state.HostShareReady = true
	return state
}

func unmountHostShare(ctx context.Context, state spindvm.State) {
	if state.HostSharePath == "" {
		return
	}
	switch state.Backend {
	case BackendCloudHypervisor:
		if state.CloudHypervisorVsockSocketPath != "" && state.HostShareGuestPort != 0 {
			_ = sendHostShareVsockRequest(ctx, state.CloudHypervisorVsockSocketPath, state.HostShareGuestPort, "UNMOUNT", state.HostSharePath)
		}
	default:
		if state.HostShareMountSocketPath != "" {
			_ = sendHostShareRequest(ctx, state.HostShareMountSocketPath, "UNMOUNT", state.HostSharePath)
		}
	}
}

func cleanupHostShare(ctx context.Context, state spindvm.State) {
	unmountHostShare(ctx, state)
	cleanupHostShareArtifacts(ctx, state)
}

func cleanupHostShareArtifacts(ctx context.Context, state spindvm.State) {
	if state.HostShareMountSocketPath != "" {
		_ = os.Remove(state.HostShareMountSocketPath)
	}
	if state.HostShareVirtioFSPID != 0 {
		_ = signalProcess(state.HostShareVirtioFSPID, syscall.SIGTERM)
		if err := waitForExit(state.HostShareVirtioFSPID, 3*time.Second); err != nil {
			_ = signalProcess(state.HostShareVirtioFSPID, syscall.SIGKILL)
			_ = waitForExit(state.HostShareVirtioFSPID, 3*time.Second)
		}
	}
	if state.HostShareVirtioFSSocketPath != "" {
		_ = os.Remove(state.HostShareVirtioFSSocketPath)
	}
}

func startCloudHypervisorVirtioFS(ctx context.Context, config cloudhypervisor.Config) (int, error) {
	if config.HostSharePath == "" {
		return 0, errors.New("host share path is missing")
	}
	if config.HostShareSocketPath == "" {
		return 0, errors.New("virtiofsd socket path is missing")
	}
	if err := os.Remove(config.HostShareSocketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("remove stale virtiofsd socket: %w", err)
	}
	virtiofsdPath, err := exec.LookPath("virtiofsd")
	if err != nil {
		return 0, errors.New("virtiofsd binary not found")
	}
	logFile, err := os.OpenFile(config.HostShareLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open virtiofsd log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, virtiofsdPath,
		"--socket-path="+config.HostShareSocketPath,
		"--shared-dir="+config.HostSharePath,
		"--cache=never",
		"--sandbox=namespace",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start virtiofsd: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return 0, fmt.Errorf("release virtiofsd process: %w", err)
	}
	if err := cloudhypervisor.WaitForSocketFile(config.HostShareSocketPath, pid, 10*time.Second, processAlive); err != nil {
		cleanupHostShareArtifacts(ctx, spindvm.State{
			HostShareVirtioFSPID:        pid,
			HostShareVirtioFSSocketPath: config.HostShareSocketPath,
		})
		return 0, err
	}
	return pid, nil
}

func sendHostShareRequest(ctx context.Context, socketPath string, op string, path string) error {
	if socketPath == "" {
		return errors.New("host share mount socket path is missing")
	}
	if path == "" {
		return errors.New("host share path is missing")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect host share mount helper: %w", err)
	}
	defer conn.Close()
	return sendHostShareConnRequest(ctx, conn, op, path)
}

func sendHostShareVsockRequest(ctx context.Context, socketPath string, port uint32, op string, path string) error {
	if port == 0 {
		return errors.New("host share guest port is missing")
	}
	conn, err := cloudhypervisor.DialVsock(ctx, socketPath, port)
	if err != nil {
		return fmt.Errorf("connect host share mount helper: %w", err)
	}
	defer conn.Close()
	return sendHostShareConnRequest(ctx, conn, op, path)
}

func sendHostShareConnRequest(ctx context.Context, conn net.Conn, op string, path string) error {
	if path == "" {
		return errors.New("host share path is missing")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}
	if _, err := fmt.Fprintf(conn, "%s %s\n", op, path); err != nil {
		return fmt.Errorf("send host share mount request: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read host share mount response: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "OK" {
		return nil
	}
	if strings.HasPrefix(line, "ERR ") {
		return errors.New(strings.TrimPrefix(line, "ERR "))
	}
	return fmt.Errorf("unexpected host share mount response %q", line)
}
