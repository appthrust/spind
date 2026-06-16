package start

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	spindkind "github.com/suin/spind/internal/spind/kind"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func kubernetesSupport(metadata spindvm.Metadata) string {
	if metadata.KindReady {
		return capabilitySupported
	}
	return capabilityUnsupported
}

func kubernetesStatus(metadata spindvm.Metadata, state spindvm.State) string {
	if kubernetesSupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(state.KubernetesReady)
}

func prepareKindSnapshot(ctx context.Context, tmpDir string, vmDir string, state spindvm.State, options spindsnapshot.CreateOptions) (*spindkind.Metadata, error) {
	if !options.Kind {
		return nil, nil
	}
	return spindkind.PrepareSnapshot(ctx, tmpDir, state.DockerSocketPath, state.DockerAvailable && state.DockerAPIReady, spindkind.SnapshotOptions{
		KubeconfigPath: options.KubeconfigPath,
		Context:        options.Context,
	})
}

func copyKindSnapshotArtifacts(snapshotDir string, vmDir string) error {
	return spindkind.CopyArtifacts(snapshotDir, vmDir)
}

func (m *Manager) configureKubernetesEndpoint(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State) spindvm.State {
	if !metadata.KindReady {
		return state
	}
	state.KubernetesReady = false
	state.KubernetesLastError = ""
	state.KubernetesKubeconfigPath = filepath.Join(vmDir, "kubeconfig")
	state.KubernetesContext = "spind-" + name
	state.KubernetesRelayLogPath = filepath.Join(vmDir, "kubernetes-relay.log")

	kindMetadata, err := readKindMetadata(vmDir)
	if err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	state.KubernetesAPIServerTargetPort = kindMetadata.APIServerTargetPort
	if state.KubernetesAPIServerTargetPort == 0 {
		state.KubernetesLastError = "kind API server target port is missing"
		return state
	}
	port, err := allocateKubernetesPort(state.KubernetesAPIServerPort)
	if err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	state.KubernetesAPIServerPort = port
	state.KubernetesAPIServerURL = fmt.Sprintf("https://127.0.0.1:%d", port)
	if err := spindkind.GenerateKubeconfig(vmDir, name, state.KubernetesAPIServerURL, state.KubernetesKubeconfigPath); err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	pid, err := startKubernetesRelay(ctx, name, vmDir, metadata, state)
	if err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	state.KubernetesRelayPID = pid
	if err := waitForTCPPort(ctx, "127.0.0.1", port, 5*time.Second); err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	if err := kubectlCheckGeneratedKubeconfig(ctx, state.KubernetesKubeconfigPath); err != nil {
		state.KubernetesLastError = err.Error()
		return state
	}
	state.KubernetesReady = true
	return state
}

func readKindMetadata(vmDir string) (spindkind.Metadata, error) {
	return spindkind.ReadMetadata(vmDir)
}

func allocateKubernetesPort(preferred int) (int, error) {
	if preferred > 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(preferred))
		if err == nil {
			_ = listener.Close()
			return preferred, nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate Kubernetes API port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func startKubernetesRelay(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State) (int, error) {
	args := []string{
		"kubernetes-relay",
		name,
		"--listen-port", strconv.Itoa(state.KubernetesAPIServerPort),
		"--target-port", strconv.Itoa(state.KubernetesAPIServerTargetPort),
		"--guest-port", fmt.Sprintf("%d", state.DockerTCPForwardGuestPort),
	}
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if state.ExecSocketPath == "" {
			return 0, errors.New("exec socket path is missing")
		}
		args = append(args,
			"--ssh-socket", state.ExecSocketPath,
			"--ssh-key", filepath.Join(vmDir, vmSSHPrivateKeyName),
			"--ssh-user", metadata.ExecUser,
		)
	case BackendCloudHypervisor:
		if state.CloudHypervisorVsockSocketPath == "" {
			return 0, errors.New("Cloud Hypervisor vsock socket path is missing")
		}
		args = append(args, "--vsock", state.CloudHypervisorVsockSocketPath)
	default:
		return 0, fmt.Errorf("unsupported backend %q", metadata.Backend)
	}
	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve spind executable: %w", err)
	}
	logFile, err := os.OpenFile(state.KubernetesRelayLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open Kubernetes relay log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start Kubernetes relay: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return 0, fmt.Errorf("release Kubernetes relay process: %w", err)
	}
	return pid, nil
}

func kubectlCheckGeneratedKubeconfig(ctx context.Context, kubeconfigPath string) error {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "get", "nodes")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("check generated kubeconfig: %w: %s", err, string(output))
	}
	return nil
}

func cleanupKubernetesEndpoint(state spindvm.State) {
	if state.KubernetesRelayPID != 0 && processAlive(state.KubernetesRelayPID) {
		_ = signalProcess(state.KubernetesRelayPID, syscall.SIGTERM)
		_ = waitForExit(state.KubernetesRelayPID, 2*time.Second)
	}
}

func waitForTCPPort(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("TCP port %s was not ready within %s: %w", net.JoinHostPort(host, strconv.Itoa(port)), timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
