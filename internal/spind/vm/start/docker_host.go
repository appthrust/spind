package start

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	spinddocker "github.com/suin/spind/internal/spind/docker"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func (m *Manager) configureDockerEndpoint(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State) spindvm.State {
	state.DockerSocketPath = filepath.Join(vmDir, "docker.sock")
	state.DockerEndpointURI = "unix://" + state.DockerSocketPath
	state.DockerGuestPort = defaultDockerPort
	state.DockerLogPath = filepath.Join(vmDir, "docker-relay.log")
	state.DockerTCPForwardGuestPort = defaultDockerTCPForwardPort
	state.DockerPortRelayLogPath = filepath.Join(vmDir, "docker-port-relay.log")
	if metadata.Backend == BackendVirtualizationFramework {
		state.DockerTCPForwardSocketPath = filepath.Join(vmDir, "tcp-forward.sock")
	}
	state.DockerAvailable = false
	state.DockerAPIReady = false
	state.DockerLastError = ""
	state.DockerNetworkReady = false
	state.DockerNetworkLastError = ""
	state.DockerGuestIPAddress = ""
	state.DockerDefaultRouteReady = false
	state.DockerDNSReady = false
	state.DockerBridgeReady = false
	state.DockerPullReady = false
	state.DockerPortRelayReady = false
	state.DockerPublishedPorts = nil

	var relayPID int
	var err error
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		relayPID = state.PID
	case BackendCloudHypervisor:
		relayPID, err = startCloudHypervisorDockerRelay(ctx, name, state)
	default:
		err = fmt.Errorf("unsupported backend %q", metadata.Backend)
	}
	if err != nil {
		state.DockerLastError = err.Error()
		_ = os.Remove(state.DockerSocketPath)
		return state
	}
	state.DockerRelayPID = relayPID

	timeout := dockerReadyTimeout(metadata)
	if err := spinddocker.WaitAPI(ctx, state.DockerSocketPath, timeout); err != nil {
		state.DockerLastError = err.Error()
		if metadata.Backend == BackendCloudHypervisor {
			_ = signalProcess(relayPID, syscall.SIGTERM)
			_ = waitForExit(relayPID, 2*time.Second)
			_ = os.Remove(state.DockerSocketPath)
			state.DockerRelayPID = 0
			state.DockerEndpointURI = ""
			state.DockerSocketPath = ""
		}
		return state
	}
	state.DockerAvailable = true
	state.DockerAPIReady = true
	state = m.inspectDockerNetwork(ctx, name, metadata, state)
	state.DockerPublishedPorts = spinddocker.PublishedPorts(ctx, state.DockerSocketPath)
	state = startDockerPortRelay(ctx, name, vmDir, metadata, state)
	return state
}

func dockerReadyTimeout(metadata spindvm.Metadata) time.Duration {
	if isDockerHostImage(metadata) {
		if metadata.Backend == BackendVirtualizationFramework {
			return 120 * time.Second
		}
		return 45 * time.Second
	}
	return 3 * time.Second
}

func isDockerHostImage(metadata spindvm.Metadata) bool {
	return strings.Contains(metadata.Image, "docker")
}

func startCloudHypervisorDockerRelay(ctx context.Context, name string, state spindvm.State) (int, error) {
	if state.CloudHypervisorVsockSocketPath == "" {
		return 0, errors.New("Cloud Hypervisor vsock socket path is missing")
	}
	if state.DockerSocketPath == "" {
		return 0, errors.New("Docker socket path is missing")
	}
	_ = os.Remove(state.DockerSocketPath)
	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve spind executable: %w", err)
	}
	logFile, err := os.OpenFile(state.DockerLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open Docker relay log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, executable,
		"docker-relay",
		name,
		"--endpoint", state.DockerSocketPath,
		"--vsock", state.CloudHypervisorVsockSocketPath,
		"--port", fmt.Sprintf("%d", state.DockerGuestPort),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start Docker relay: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return 0, fmt.Errorf("release Docker relay process: %w", err)
	}
	if err := cloudhypervisor.WaitForSocketFile(state.DockerSocketPath, pid, 5*time.Second, processAlive); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		_ = waitForExit(pid, 2*time.Second)
		return 0, fmt.Errorf("Docker relay socket was not ready: %w", err)
	}
	return pid, nil
}

func cleanupDockerEndpointForVM(vmDir string, state spindvm.State) {
	cleanupDockerEndpoint(state)
	if state.DockerSocketPath == "" {
		_ = os.Remove(filepath.Join(vmDir, "docker.sock"))
	}
	if state.DockerTCPForwardSocketPath == "" {
		_ = os.Remove(filepath.Join(vmDir, "tcp-forward.sock"))
	}
}

func cleanupDockerEndpoint(state spindvm.State) {
	if state.DockerPortRelayPID != 0 && processAlive(state.DockerPortRelayPID) {
		_ = signalProcess(state.DockerPortRelayPID, syscall.SIGTERM)
		_ = waitForExit(state.DockerPortRelayPID, 2*time.Second)
	}
	if state.DockerRelayPID != 0 && state.DockerRelayPID != state.PID && processAlive(state.DockerRelayPID) {
		_ = signalProcess(state.DockerRelayPID, syscall.SIGTERM)
		_ = waitForExit(state.DockerRelayPID, 2*time.Second)
	}
	if state.DockerSocketPath != "" {
		_ = os.Remove(state.DockerSocketPath)
	}
	if state.DockerTCPForwardSocketPath != "" {
		_ = os.Remove(state.DockerTCPForwardSocketPath)
	}
}

func (m *Manager) inspectDockerNetwork(ctx context.Context, name string, metadata spindvm.Metadata, state spindvm.State) spindvm.State {
	if !isDockerHostImage(metadata) {
		return state
	}
	state.DockerNetworkReady = false
	state.DockerNetworkLastError = ""
	state.DockerGuestIPAddress = ""
	state.DockerDefaultRouteReady = false
	state.DockerDNSReady = false
	state.DockerBridgeReady = false
	state.DockerPullReady = false
	script := strings.Join([]string{
		"set -eu",
		"PATH=/usr/sbin:/sbin:/usr/bin:/bin:$PATH",
		"default_dev=$(ip route show default 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i == \"dev\") { print $(i+1); exit }}')",
		"ipaddr=$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i == \"src\") { print $(i+1); exit }}')",
		"if [ -z \"$ipaddr\" ] && [ -n \"$default_dev\" ]; then ipaddr=$(ip -4 -o addr show dev \"$default_dev\" scope global 2>/dev/null | awk '{split($4,a,\"/\"); print a[1]; exit}'); fi",
		"default_route=0",
		"[ -n \"$default_dev\" ] && default_route=1",
		"dns=0",
		"awk '/^nameserver[[:space:]]+/ { found=1 } END { exit found ? 0 : 1 }' /etc/resolv.conf >/dev/null 2>&1 && dns=1",
		"bridge=0",
		"ip link show docker0 >/dev/null 2>&1 && bridge=1",
		"printf 'ip=%s\\ndefault_route=%s\\ndns=%s\\nbridge=%s\\n' \"$ipaddr\" \"$default_route\" \"$dns\" \"$bridge\"",
		"[ -n \"$ipaddr\" ] && [ \"$default_route\" = 1 ]",
	}, "\n")
	stdout, stderr, exitCode, err := m.execCaptureRetry(ctx, name, 10*time.Second, "sh", "-lc", script)
	if err != nil {
		state.DockerNetworkLastError = err.Error()
		return state
	}
	if exitCode != 0 {
		state.DockerNetworkLastError = strings.TrimSpace(stderr)
		if state.DockerNetworkLastError == "" {
			state.DockerNetworkLastError = fmt.Sprintf("network inspection exited with status %d", exitCode)
		}
		return state
	}
	values := parseKeyValueOutput(stdout)
	state.DockerGuestIPAddress = values["ip"]
	state.DockerDefaultRouteReady = values["default_route"] == "1"
	state.DockerDNSReady = values["dns"] == "1"
	state.DockerBridgeReady = values["bridge"] == "1"
	state.DockerNetworkReady = state.DockerGuestIPAddress != "" && state.DockerDefaultRouteReady && state.DockerDNSReady
	state.DockerPullReady = state.DockerAPIReady && state.DockerNetworkReady && state.DockerBridgeReady
	if !state.DockerNetworkReady {
		state.DockerNetworkLastError = "guest network is missing IP, default route, or DNS"
	}
	return state
}

func (m *Manager) execCaptureRetry(ctx context.Context, name string, timeout time.Duration, args ...string) (string, string, int, error) {
	deadline := time.Now().Add(timeout)
	var stdout string
	var stderr string
	var exitCode int
	var err error
	for {
		stdout, stderr, exitCode, err = m.execCapture(ctx, name, args...)
		if err == nil && exitCode == 0 {
			return stdout, stderr, exitCode, nil
		}
		if time.Now().After(deadline) {
			if err == nil {
				return stdout, stderr, exitCode, nil
			}
			return stdout, stderr, exitCode, err
		}
		select {
		case <-ctx.Done():
			return stdout, stderr, exitCode, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (m *Manager) execCapture(ctx context.Context, name string, args ...string) (string, string, int, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode, err := m.Exec(ctx, name, args, strings.NewReader(""), &stdout, &stderr)
	return stdout.String(), stderr.String(), exitCode, err
}

func parseKeyValueOutput(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}
