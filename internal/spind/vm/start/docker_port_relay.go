package start

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func startDockerPortRelay(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State) spindvm.State {
	if !state.DockerAvailable || state.DockerSocketPath == "" {
		return state
	}
	args := []string{
		"docker-port-relay",
		name,
		"--docker-endpoint", state.DockerSocketPath,
		"--guest-port", fmt.Sprintf("%d", state.DockerTCPForwardGuestPort),
	}
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if state.ExecSocketPath == "" {
			state.DockerLastError = appendError(state.DockerLastError, "exec socket path is missing")
			return state
		}
		args = append(args,
			"--ssh-socket", state.ExecSocketPath,
			"--ssh-key", filepath.Join(vmDir, vmSSHPrivateKeyName),
			"--ssh-user", metadata.ExecUser,
		)
	case BackendCloudHypervisor:
		if state.CloudHypervisorVsockSocketPath == "" {
			state.DockerLastError = appendError(state.DockerLastError, "Cloud Hypervisor vsock socket path is missing")
			return state
		}
		args = append(args, "--vsock", state.CloudHypervisorVsockSocketPath)
	default:
		state.DockerLastError = appendError(state.DockerLastError, fmt.Sprintf("unsupported backend %q", metadata.Backend))
		return state
	}

	executable, err := os.Executable()
	if err != nil {
		state.DockerLastError = appendError(state.DockerLastError, fmt.Sprintf("resolve spind executable: %v", err))
		return state
	}
	logFile, err := os.OpenFile(state.DockerPortRelayLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		state.DockerLastError = appendError(state.DockerLastError, fmt.Sprintf("open Docker port relay log: %v", err))
		return state
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		state.DockerLastError = appendError(state.DockerLastError, fmt.Sprintf("start Docker port relay: %v", err))
		return state
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		state.DockerLastError = appendError(state.DockerLastError, fmt.Sprintf("release Docker port relay process: %v", err))
		return state
	}
	state.DockerPortRelayPID = pid
	state.DockerPortRelayReady = true
	return state
}

func appendError(existing string, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
