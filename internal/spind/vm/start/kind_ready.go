package start

import (
	"bytes"
	"context"
	"encoding/json"
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
	"gopkg.in/yaml.v3"
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
	if options.K8s == "" {
		return nil, nil
	}
	return spindkind.PrepareSnapshot(ctx, tmpDir, state.DockerSocketPath, state.DockerAvailable && state.DockerAPIReady, spindkind.SnapshotOptions{
		Distribution:   options.K8s,
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
	pid, err := startKubernetesRelay(ctx, name, vmDir, metadata, state, kindMetadata)
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
	state = m.configureRegistryEndpoint(ctx, name, vmDir, metadata, state, kindMetadata)
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

func startKubernetesRelay(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State, kindMetadata spindkind.Metadata) (int, error) {
	return startTCPRelay(ctx, name, vmDir, metadata, state, state.KubernetesRelayLogPath, state.KubernetesAPIServerPort, state.KubernetesAPIServerTargetPort, "Kubernetes", kindMetadata.Distribution == spindkind.DistributionK3d)
}

func startRegistryRelay(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State) (int, error) {
	return startTCPRelay(ctx, name, vmDir, metadata, state, state.RegistryRelayLogPath, state.RegistryPort, state.RegistryTargetPort, "registry", true)
}

func startTCPRelay(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State, logPath string, listenPort int, targetPort int, service string, preferGuestIP bool) (int, error) {
	args, err := tcpRelayArgs(name, vmDir, metadata, state, listenPort, targetPort, preferGuestIP)
	if err != nil {
		return 0, err
	}
	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve spind executable: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open %s relay log: %w", service, err)
	}
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s relay: %w", service, err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return 0, fmt.Errorf("release %s relay process: %w", service, err)
	}
	return pid, nil
}

func tcpRelayArgs(name string, vmDir string, metadata spindvm.Metadata, state spindvm.State, listenPort int, targetPort int, preferGuestIP bool) ([]string, error) {
	args := []string{
		"kubernetes-relay",
		name,
		"--listen-port", strconv.Itoa(listenPort),
		"--target-port", strconv.Itoa(targetPort),
		"--guest-port", fmt.Sprintf("%d", state.DockerTCPForwardGuestPort),
	}
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if preferGuestIP && state.DockerGuestIPAddress != "" {
			return append(args, "--guest-ip", state.DockerGuestIPAddress), nil
		}
		if state.ExecSocketPath == "" {
			return nil, errors.New("exec socket path is missing")
		}
		args = append(args,
			"--ssh-socket", state.ExecSocketPath,
			"--ssh-key", filepath.Join(vmDir, vmSSHPrivateKeyName),
			"--ssh-user", metadata.ExecUser,
		)
	case BackendCloudHypervisor:
		if state.CloudHypervisorVsockSocketPath == "" {
			return nil, errors.New("Cloud Hypervisor vsock socket path is missing")
		}
		args = append(args, "--vsock", state.CloudHypervisorVsockSocketPath)
	default:
		return nil, fmt.Errorf("unsupported backend %q", metadata.Backend)
	}
	return args, nil
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
	if state.RegistryRelayPID != 0 && processAlive(state.RegistryRelayPID) {
		_ = signalProcess(state.RegistryRelayPID, syscall.SIGTERM)
		_ = waitForExit(state.RegistryRelayPID, 2*time.Second)
	}
}

func (m *Manager) configureRegistryEndpoint(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata, state spindvm.State, kindMetadata spindkind.Metadata) spindvm.State {
	if kindMetadata.Distribution != spindkind.DistributionK3d || kindMetadata.RegistryTargetPort == 0 {
		return state
	}
	state.RegistryReady = false
	state.RegistryLastError = ""
	state.RegistryRelayLogPath = filepath.Join(vmDir, "registry-relay.log")
	state.RegistryTargetPort = kindMetadata.RegistryTargetPort
	state.RegistryHostFromCluster = kindMetadata.RegistryHostFromCluster
	state.RegistryURL = fmt.Sprintf("localhost:%d", state.RegistryTargetPort)
	port, err := allocateKubernetesPort(firstPositive(state.RegistryPort, state.RegistryTargetPort))
	if err != nil {
		state.RegistryLastError = err.Error()
		return state
	}
	state.RegistryPort = port
	pid, err := startRegistryRelay(ctx, name, vmDir, metadata, state)
	if err != nil {
		state.RegistryLastError = err.Error()
		return state
	}
	state.RegistryRelayPID = pid
	if err := waitForTCPPort(ctx, "127.0.0.1", port, 5*time.Second); err != nil {
		state.RegistryLastError = err.Error()
		return state
	}
	if err := updateLocalRegistryHosting(ctx, state.KubernetesKubeconfigPath, state.RegistryURL, kindMetadata.RegistryHostFromCluster); err != nil {
		state.RegistryLastError = err.Error()
		return state
	}
	state.RegistryLocalHostingUpdated = true
	state.RegistryReady = true
	return state
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type configMapDocument struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   map[string]string `json:"metadata" yaml:"metadata"`
	Data       map[string]string `json:"data" yaml:"data"`
}

type localRegistryHostingV1 struct {
	Host                     string `yaml:"host"`
	HostFromClusterNetwork   string `yaml:"hostFromClusterNetwork,omitempty"`
	HostFromContainerRuntime string `yaml:"hostFromContainerRuntime,omitempty"`
	Help                     string `yaml:"help,omitempty"`
}

func updateLocalRegistryHosting(ctx context.Context, kubeconfigPath string, registryURL string, hostFromCluster string) error {
	hosting, err := readLocalRegistryHosting(ctx, kubeconfigPath)
	if err != nil {
		hosting = localRegistryHostingV1{
			HostFromClusterNetwork:   hostFromCluster,
			HostFromContainerRuntime: hostFromCluster,
			Help:                     "https://k3d.io/stable/usage/registries/#using-a-local-registry",
		}
	}
	hosting.Host = registryURL
	if hosting.HostFromClusterNetwork == "" {
		hosting.HostFromClusterNetwork = hostFromCluster
	}
	if hosting.HostFromContainerRuntime == "" {
		hosting.HostFromContainerRuntime = hostFromCluster
	}
	data, err := yaml.Marshal(hosting)
	if err != nil {
		return fmt.Errorf("marshal local registry hosting: %w", err)
	}
	cm := configMapDocument{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: map[string]string{
			"name":      "local-registry-hosting",
			"namespace": "kube-public",
		},
		Data: map[string]string{"localRegistryHosting.v1": string(data)},
	}
	manifest, err := yaml.Marshal(cm)
	if err != nil {
		return fmt.Errorf("marshal local registry hosting ConfigMap: %w", err)
	}
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(manifest)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply local registry hosting ConfigMap: %w: %s", err, string(output))
	}
	return nil
}

func readLocalRegistryHosting(ctx context.Context, kubeconfigPath string) (localRegistryHostingV1, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "--namespace", "kube-public", "get", "configmap", "local-registry-hosting", "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return localRegistryHostingV1{}, fmt.Errorf("read local registry hosting ConfigMap: %w: %s", err, string(output))
	}
	var cm configMapDocument
	if err := json.Unmarshal(output, &cm); err != nil {
		return localRegistryHostingV1{}, fmt.Errorf("parse local registry hosting ConfigMap: %w", err)
	}
	raw := cm.Data["localRegistryHosting.v1"]
	if raw == "" {
		return localRegistryHostingV1{}, errors.New("local registry hosting ConfigMap has no localRegistryHosting.v1 data")
	}
	var hosting localRegistryHostingV1
	if err := yaml.Unmarshal([]byte(raw), &hosting); err != nil {
		return localRegistryHostingV1{}, fmt.Errorf("parse localRegistryHosting.v1: %w", err)
	}
	return hosting, nil
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
