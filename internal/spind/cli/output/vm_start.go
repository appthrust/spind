package output

import (
	"fmt"
	"io"

	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func PrintHostShareStartLine(stdout io.Writer, stderr io.Writer, info spindvm.Info) {
	if info.HostShareSupport == "unsupported" {
		return
	}
	if info.HostSharePath == "" {
		return
	}
	if info.HostShareReady {
		fmt.Fprintf(stdout, "mount: %s\n", info.HostSharePath)
		return
	}
	fmt.Fprintf(stdout, "mount: unavailable\n")
	if info.HostShareLastError != "" {
		fmt.Fprintf(stderr, "warning: host share unavailable: %s\n", info.HostShareLastError)
	}
	if info.HostShareVirtioFSLogPath != "" {
		fmt.Fprintf(stderr, "hostShareLogPath: %s\n", info.HostShareVirtioFSLogPath)
	}
}

func PrintDockerStartLine(stdout io.Writer, stderr io.Writer, info spindvm.Info) {
	if info.DockerAvailable {
		fmt.Fprintf(stdout, "docker: %s\n", info.DockerEndpointURI)
		if info.DockerNetworkSupport == "supported" && info.DockerNetworkStatus == "unavailable" && info.DockerNetworkLastError != "" {
			fmt.Fprintf(stderr, "warning: Docker network unavailable: %s\n", info.DockerNetworkLastError)
			if info.BackendLogPath != "" {
				fmt.Fprintf(stderr, "backendLogPath: %s\n", info.BackendLogPath)
			}
		}
		if info.DockerPortRelaySupport == "supported" && info.DockerPortRelayStatus == "unavailable" {
			if info.DockerLastError != "" {
				fmt.Fprintf(stderr, "warning: Docker port publish relay unavailable: %s\n", info.DockerLastError)
			} else {
				fmt.Fprintln(stderr, "warning: Docker port publish relay unavailable")
			}
			if info.DockerPortRelayLogPath != "" {
				fmt.Fprintf(stderr, "dockerPortRelayLogPath: %s\n", info.DockerPortRelayLogPath)
			}
		}
		return
	}
	fmt.Fprintln(stdout, "docker: unavailable")
	if info.DockerLastError != "" {
		fmt.Fprintf(stderr, "warning: Docker endpoint unavailable: %s\n", info.DockerLastError)
	}
	if info.DockerLogPath != "" {
		fmt.Fprintf(stderr, "dockerLogPath: %s\n", info.DockerLogPath)
	}
}

func PrintKubernetesStartLine(stdout io.Writer, stderr io.Writer, info spindvm.Info) {
	if info.KubernetesSupport != "supported" {
		return
	}
	if info.KubernetesReady {
		fmt.Fprintln(stdout, "kubernetes: ready")
		fmt.Fprintf(stdout, "kubeconfig: %s\n", info.KubernetesKubeconfigPath)
		fmt.Fprintf(stdout, "context: %s\n", info.KubernetesContext)
		fmt.Fprintf(stdout, "api server: %s\n", info.KubernetesAPIServerURL)
		return
	}
	fmt.Fprintln(stdout, "kubernetes: unavailable")
	if info.KubernetesLastError != "" {
		fmt.Fprintf(stderr, "warning: Kubernetes API unavailable: %s\n", info.KubernetesLastError)
	}
	if info.KubernetesRelayLogPath != "" {
		fmt.Fprintf(stderr, "kubernetesLogPath: %s\n", info.KubernetesRelayLogPath)
	}
}
