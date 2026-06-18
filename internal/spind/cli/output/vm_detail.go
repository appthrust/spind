package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func PrintVMInfo(stdout io.Writer, info spindvm.Info) {
	fmt.Fprintf(stdout, "name: %s\n", info.Name)
	fmt.Fprintf(stdout, "status: %s\n", info.Status)
	fmt.Fprintf(stdout, "backend: %s\n", info.Backend)
	fmt.Fprintf(stdout, "execReady: %t\n", info.ExecReady)
	fmt.Fprintf(stdout, "restoreMode: %s\n", info.RestoreMode)
	fmt.Fprintf(stdout, "vmDir: %s\n", info.VMDir)
	fmt.Fprintf(stdout, "statePath: %s\n", info.StatePath)
	if info.Image != "" {
		fmt.Fprintf(stdout, "image: %s\n", info.Image)
	}
	if info.PID != 0 {
		fmt.Fprintf(stdout, "pid: %d\n", info.PID)
	}
	if info.FromSnapshot {
		fmt.Fprintf(stdout, "fromSnapshot: %t\n", info.FromSnapshot)
		fmt.Fprintf(stdout, "sourceSnapshot: %s\n", info.SourceSnapshot)
	}
	if info.ExecSocketPath != "" {
		fmt.Fprintf(stdout, "execSocketPath: %s\n", info.ExecSocketPath)
	}
	if info.CloudHypervisorAPISocketPath != "" {
		fmt.Fprintf(stdout, "cloudHypervisorAPISocketPath: %s\n", info.CloudHypervisorAPISocketPath)
	}
	if info.CloudHypervisorVsockPath != "" {
		fmt.Fprintf(stdout, "cloudHypervisorVsockPath: %s\n", info.CloudHypervisorVsockPath)
	}
	PrintDockerInfo(stdout, info)
	PrintHostShareInfo(stdout, info)
	PrintKubernetesInfo(stdout, info)
	PrintRegistryInfo(stdout, info)
	if !info.StartedAt.IsZero() {
		fmt.Fprintf(stdout, "startedAt: %s\n", info.StartedAt.Format(time.RFC3339))
	}
	if !info.UpdatedAt.IsZero() {
		fmt.Fprintf(stdout, "updatedAt: %s\n", info.UpdatedAt.Format(time.RFC3339))
	}
	if info.LastStartDuration > 0 {
		fmt.Fprintf(stdout, "lastStartDuration: %s\n", FormatDuration(info.LastStartDuration))
	}
	if info.SerialLogPath != "" {
		fmt.Fprintf(stdout, "serialLogPath: %s\n", info.SerialLogPath)
	}
	if info.BackendLogPath != "" {
		fmt.Fprintf(stdout, "backendLogPath: %s\n", info.BackendLogPath)
	}
	if info.EventLogPath != "" {
		fmt.Fprintf(stdout, "eventLogPath: %s\n", info.EventLogPath)
	}
	if len(info.LogPaths) > 0 {
		fmt.Fprintln(stdout, "logs:")
		for _, path := range info.LogPaths {
			fmt.Fprintf(stdout, "  %s\n", path)
		}
	}
}
func PrintDockerInfo(stdout io.Writer, info spindvm.Info) {
	if info.DockerAvailable {
		fmt.Fprintf(stdout, "docker: %s\n", info.DockerEndpointURI)
	} else {
		fmt.Fprintln(stdout, "docker: unavailable")
	}
	if info.DockerAPISupport != "" {
		fmt.Fprintf(stdout, "dockerApiSupport: %s\n", info.DockerAPISupport)
	}
	if info.DockerAPIStatus != "" {
		fmt.Fprintf(stdout, "dockerApiStatus: %s\n", info.DockerAPIStatus)
	}
	if info.DockerSocketPath != "" {
		fmt.Fprintf(stdout, "dockerSocketPath: %s\n", info.DockerSocketPath)
	}
	if info.DockerRelayPID != 0 {
		fmt.Fprintf(stdout, "dockerRelayPid: %d\n", info.DockerRelayPID)
	}
	if info.DockerGuestPort != 0 {
		fmt.Fprintf(stdout, "dockerGuestPort: %d\n", info.DockerGuestPort)
	}
	fmt.Fprintf(stdout, "dockerApiReady: %t\n", info.DockerAPIReady)
	if info.DockerLastError != "" {
		fmt.Fprintf(stdout, "dockerLastError: %s\n", info.DockerLastError)
	}
	if info.DockerLogPath != "" {
		fmt.Fprintf(stdout, "dockerLogPath: %s\n", info.DockerLogPath)
	}
	if info.DockerNetworkSupport != "" {
		fmt.Fprintf(stdout, "dockerNetworkSupport: %s\n", info.DockerNetworkSupport)
	}
	if info.DockerNetworkStatus != "" {
		fmt.Fprintf(stdout, "dockerNetworkStatus: %s\n", info.DockerNetworkStatus)
	}
	fmt.Fprintf(stdout, "dockerNetworkReady: %t\n", info.DockerNetworkReady)
	if info.DockerGuestIPAddress != "" {
		fmt.Fprintf(stdout, "dockerGuestIpAddress: %s\n", info.DockerGuestIPAddress)
	}
	fmt.Fprintf(stdout, "dockerDefaultRouteReady: %t\n", info.DockerDefaultRouteReady)
	fmt.Fprintf(stdout, "dockerDnsReady: %t\n", info.DockerDNSReady)
	fmt.Fprintf(stdout, "dockerBridgeReady: %t\n", info.DockerBridgeReady)
	fmt.Fprintf(stdout, "dockerPullReady: %t\n", info.DockerPullReady)
	if info.DockerNetworkLastError != "" {
		fmt.Fprintf(stdout, "dockerNetworkLastError: %s\n", info.DockerNetworkLastError)
	}
	if info.DockerTCPForwardSocketPath != "" {
		fmt.Fprintf(stdout, "dockerTcpForwardSocketPath: %s\n", info.DockerTCPForwardSocketPath)
	}
	if info.DockerTCPForwardGuestPort != 0 {
		fmt.Fprintf(stdout, "dockerTcpForwardGuestPort: %d\n", info.DockerTCPForwardGuestPort)
	}
	if info.DockerPortRelayPID != 0 {
		fmt.Fprintf(stdout, "dockerPortRelayPid: %d\n", info.DockerPortRelayPID)
	}
	if info.DockerPortRelaySupport != "" {
		fmt.Fprintf(stdout, "dockerPortRelaySupport: %s\n", info.DockerPortRelaySupport)
	}
	if info.DockerPortRelayStatus != "" {
		fmt.Fprintf(stdout, "dockerPortRelayStatus: %s\n", info.DockerPortRelayStatus)
	}
	fmt.Fprintf(stdout, "dockerPortRelayReady: %t\n", info.DockerPortRelayReady)
	if info.DockerPortRelayLogPath != "" {
		fmt.Fprintf(stdout, "dockerPortRelayLogPath: %s\n", info.DockerPortRelayLogPath)
	}
	if len(info.DockerPublishedPorts) > 0 {
		fmt.Fprintf(stdout, "dockerPublishedPorts: %s\n", strings.Join(info.DockerPublishedPorts, ","))
	}
	if info.DockerNetworkBackend != "" {
		fmt.Fprintf(stdout, "dockerNetworkBackend: %s\n", info.DockerNetworkBackend)
	}
	if info.DockerNetworkBackendPID != 0 {
		fmt.Fprintf(stdout, "dockerNetworkBackendPid: %d\n", info.DockerNetworkBackendPID)
	}
	if info.DockerNetworkSocketPath != "" {
		fmt.Fprintf(stdout, "dockerNetworkSocketPath: %s\n", info.DockerNetworkSocketPath)
	}
	if info.DockerNetworkBackendLogPath != "" {
		fmt.Fprintf(stdout, "dockerNetworkBackendLogPath: %s\n", info.DockerNetworkBackendLogPath)
	}
	if info.DockerTapName != "" {
		fmt.Fprintf(stdout, "dockerTapName: %s\n", info.DockerTapName)
	}
	if info.DockerGuestMAC != "" {
		fmt.Fprintf(stdout, "dockerGuestMac: %s\n", info.DockerGuestMAC)
	}
}

func PrintHostShareInfo(stdout io.Writer, info spindvm.Info) {
	switch info.HostShareStatus {
	case "ready":
		if info.HostSharePath != "" {
			fmt.Fprintf(stdout, "shared path: %s\n", info.HostSharePath)
		}
	case "unavailable":
		fmt.Fprintln(stdout, "shared path: unavailable")
		if info.HostShareStatusReason != "" {
			fmt.Fprintf(stdout, "shared path reason: %s\n", info.HostShareStatusReason)
		}
		if info.HostShareVirtioFSLogPath != "" {
			fmt.Fprintf(stdout, "log: %s\n", info.HostShareVirtioFSLogPath)
		}
	case "not-applicable":
		fmt.Fprintln(stdout, "shared path: unsupported")
		if info.HostShareStatusReason != "" {
			fmt.Fprintf(stdout, "shared path reason: %s\n", info.HostShareStatusReason)
		}
	}
	if info.HostShareSupport != "" {
		fmt.Fprintf(stdout, "hostShareSupport: %s\n", info.HostShareSupport)
	}
	if info.HostShareStatus != "" {
		fmt.Fprintf(stdout, "hostShareStatus: %s\n", info.HostShareStatus)
	}
	if info.HostSharePath != "" {
		fmt.Fprintf(stdout, "hostSharePath: %s\n", info.HostSharePath)
	}
	fmt.Fprintf(stdout, "hostShareReady: %t\n", info.HostShareReady)
	if info.HostShareStatusReason != "" {
		fmt.Fprintf(stdout, "hostShareStatusReason: %s\n", info.HostShareStatusReason)
	}
	if info.HostShareLastError != "" {
		fmt.Fprintf(stdout, "hostShareLastError: %s\n", info.HostShareLastError)
	}
	if info.HostShareMountSocketPath != "" {
		fmt.Fprintf(stdout, "hostShareMountSocketPath: %s\n", info.HostShareMountSocketPath)
	}
	if info.HostShareGuestPort != 0 {
		fmt.Fprintf(stdout, "hostShareGuestPort: %d\n", info.HostShareGuestPort)
	}
	if info.HostShareVirtioFSPID != 0 {
		fmt.Fprintf(stdout, "hostShareVirtiofsPid: %d\n", info.HostShareVirtioFSPID)
	}
	if info.HostShareVirtioFSSocketPath != "" {
		fmt.Fprintf(stdout, "hostShareVirtiofsSocketPath: %s\n", info.HostShareVirtioFSSocketPath)
	}
	if info.HostShareVirtioFSLogPath != "" {
		fmt.Fprintf(stdout, "hostShareVirtiofsLogPath: %s\n", info.HostShareVirtioFSLogPath)
	}
}

func PrintKubernetesInfo(stdout io.Writer, info spindvm.Info) {
	if info.KubernetesSupport == "unsupported" || info.KubernetesSupport == "" {
		return
	}
	if info.KubernetesReady {
		fmt.Fprintln(stdout, "kubernetes: ready")
	} else {
		fmt.Fprintln(stdout, "kubernetes: unavailable")
	}
	fmt.Fprintf(stdout, "kubernetesSupport: %s\n", info.KubernetesSupport)
	fmt.Fprintf(stdout, "kubernetesStatus: %s\n", info.KubernetesStatus)
	if info.KubernetesKubeconfigPath != "" {
		fmt.Fprintf(stdout, "kubeconfig: %s\n", info.KubernetesKubeconfigPath)
	}
	if info.KubernetesContext != "" {
		fmt.Fprintf(stdout, "context: %s\n", info.KubernetesContext)
	}
	if info.KubernetesAPIServerURL != "" {
		fmt.Fprintf(stdout, "api server: %s\n", info.KubernetesAPIServerURL)
	}
	if info.KubernetesAPIServerPort != 0 {
		fmt.Fprintf(stdout, "kubernetesApiServerPort: %d\n", info.KubernetesAPIServerPort)
	}
	if info.KubernetesAPIServerTargetPort != 0 {
		fmt.Fprintf(stdout, "kubernetesApiServerTargetPort: %d\n", info.KubernetesAPIServerTargetPort)
	}
	if info.KubernetesRelayPID != 0 {
		fmt.Fprintf(stdout, "kubernetesRelayPid: %d\n", info.KubernetesRelayPID)
	}
	if info.KubernetesLastError != "" {
		fmt.Fprintf(stdout, "kubernetesLastError: %s\n", info.KubernetesLastError)
	}
	if info.KubernetesRelayLogPath != "" {
		fmt.Fprintf(stdout, "kubernetesLogPath: %s\n", info.KubernetesRelayLogPath)
	}
}

func PrintRegistryInfo(stdout io.Writer, info spindvm.Info) {
	if info.RegistryURL == "" && info.RegistryLastError == "" && info.RegistryTargetPort == 0 {
		return
	}
	if info.RegistryReady {
		fmt.Fprintln(stdout, "registry: ready")
	} else {
		fmt.Fprintln(stdout, "registry: unavailable")
	}
	if info.RegistryURL != "" {
		fmt.Fprintf(stdout, "registryUrl: %s\n", info.RegistryURL)
	}
	if info.RegistryPort != 0 {
		fmt.Fprintf(stdout, "registryPort: %d\n", info.RegistryPort)
	}
	if info.RegistryTargetPort != 0 {
		fmt.Fprintf(stdout, "registryTargetPort: %d\n", info.RegistryTargetPort)
	}
	if info.RegistryRelayPID != 0 {
		fmt.Fprintf(stdout, "registryRelayPid: %d\n", info.RegistryRelayPID)
	}
	if info.RegistryHostFromCluster != "" {
		fmt.Fprintf(stdout, "registryHostFromCluster: %s\n", info.RegistryHostFromCluster)
	}
	if info.RegistryLastError != "" {
		fmt.Fprintf(stdout, "registryLastError: %s\n", info.RegistryLastError)
	}
	if info.RegistryRelayLogPath != "" {
		fmt.Fprintf(stdout, "registryLogPath: %s\n", info.RegistryRelayLogPath)
	}
}

func PrintVMLogHint(stderr io.Writer, info spindvm.Info) {
	if len(info.LogPaths) == 0 {
		return
	}
	fmt.Fprintln(stderr, "logs:")
	for _, path := range info.LogPaths {
		fmt.Fprintf(stderr, "  %s\n", path)
	}
}
