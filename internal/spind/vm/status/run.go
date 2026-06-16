package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spinddocker "github.com/suin/spind/internal/spind/docker"
	"github.com/suin/spind/internal/spind/process"
	"github.com/suin/spind/internal/spind/store"
	"github.com/suin/spind/internal/spind/vmstore"
)

const (
	capabilitySupported     = "supported"
	capabilityUnsupported   = "unsupported"
	capabilityReady         = "ready"
	capabilityUnavailable   = "unavailable"
	capabilityNotApplicable = "not-applicable"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	info, err := Info(cfg, options.Name)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if options.JSON {
		return output.WriteJSON(stdout, output.NewVMInfo(info), stderr)
	}
	output.PrintVMInfo(stdout, info)
	return 0
}

func Info(cfg config.Config, name string) (vmstore.Info, error) {
	if err := vmstore.ValidateName(name); err != nil {
		return vmstore.Info{}, err
	}
	vmDir := filepath.Join(cfg.VMStore, name)
	metadata, err := vmstore.ReadMetadata(vmDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return vmstore.Info{}, fmt.Errorf("VM %q: %w", name, store.ErrNotFound)
		}
		return vmstore.Info{}, fmt.Errorf("read VM metadata: %w", err)
	}
	hydrateMetadata(&metadata)
	state, err := vmstore.ReadState(vmDir)
	if err != nil {
		return vmstore.Info{}, err
	}
	status := state.Status
	execReady := state.ExecReady
	if status == "running" && (state.PID == 0 || !process.Alive(state.PID)) {
		status = "stopped"
		execReady = false
	}
	restoreMode := "boot"
	if metadata.FromSnapshot {
		restoreMode = "saved-state"
	}
	serialLogPath, backendLogPath, eventLogPath := namedLogPaths(vmDir, metadata.Backend, state)
	dockerPublishedPorts := state.DockerPublishedPorts
	if state.DockerAvailable && state.DockerSocketPath != "" {
		dockerPublishedPorts = spinddocker.PublishedPorts(context.Background(), state.DockerSocketPath)
	}
	return vmstore.Info{
		Name:                          name,
		Status:                        status,
		Backend:                       metadata.Backend,
		PID:                           state.PID,
		ExecReady:                     execReady,
		Image:                         metadata.Image,
		FromSnapshot:                  metadata.FromSnapshot,
		SourceSnapshot:                metadata.SourceSnapshot,
		RestoreMode:                   restoreMode,
		VMDir:                         vmDir,
		StatePath:                     filepath.Join(vmDir, vmstore.StateName),
		ExecSocketPath:                state.ExecSocketPath,
		CloudHypervisorAPISocketPath:  state.CloudHypervisorAPISocketPath,
		CloudHypervisorVsockPath:      state.CloudHypervisorVsockSocketPath,
		DockerSocketPath:              state.DockerSocketPath,
		DockerEndpointURI:             state.DockerEndpointURI,
		DockerRelayPID:                state.DockerRelayPID,
		DockerGuestPort:               state.DockerGuestPort,
		DockerAPISupport:              dockerCapabilitySupport(metadata),
		DockerAPIStatus:               dockerCapabilityStatus(metadata, state.DockerAvailable && state.DockerAPIReady && state.DockerSocketPath != ""),
		DockerAPIReady:                state.DockerAPIReady,
		DockerAvailable:               state.DockerAvailable && state.DockerSocketPath != "",
		DockerLastError:               state.DockerLastError,
		DockerLogPath:                 state.DockerLogPath,
		DockerNetworkSupport:          dockerCapabilitySupport(metadata),
		DockerNetworkStatus:           dockerCapabilityStatus(metadata, state.DockerNetworkReady),
		DockerNetworkReady:            state.DockerNetworkReady,
		DockerNetworkLastError:        state.DockerNetworkLastError,
		DockerGuestIPAddress:          state.DockerGuestIPAddress,
		DockerDefaultRouteReady:       state.DockerDefaultRouteReady,
		DockerDNSReady:                state.DockerDNSReady,
		DockerBridgeReady:             state.DockerBridgeReady,
		DockerPullReady:               state.DockerPullReady,
		DockerTCPForwardSocketPath:    state.DockerTCPForwardSocketPath,
		DockerTCPForwardGuestPort:     state.DockerTCPForwardGuestPort,
		DockerPortRelayPID:            state.DockerPortRelayPID,
		DockerPortRelaySupport:        dockerCapabilitySupport(metadata),
		DockerPortRelayStatus:         dockerCapabilityStatus(metadata, state.DockerPortRelayReady && state.DockerPortRelayPID != 0 && process.Alive(state.DockerPortRelayPID)),
		DockerPortRelayReady:          state.DockerPortRelayReady && state.DockerPortRelayPID != 0 && process.Alive(state.DockerPortRelayPID),
		DockerPortRelayLogPath:        state.DockerPortRelayLogPath,
		DockerPublishedPorts:          dockerPublishedPorts,
		DockerNetworkBackend:          state.DockerNetworkBackend,
		DockerNetworkBackendPID:       state.DockerNetworkBackendPID,
		DockerNetworkSocketPath:       state.DockerNetworkSocketPath,
		DockerNetworkBackendLogPath:   state.DockerNetworkBackendLogPath,
		DockerTapName:                 state.DockerTapName,
		DockerGuestMAC:                state.DockerGuestMAC,
		HostShareSupport:              hostShareSupport(metadata),
		HostShareStatus:               hostShareStatus(metadata, state),
		HostShareStatusReason:         hostShareStatusReason(metadata, state),
		HostSharePath:                 state.HostSharePath,
		HostShareReady:                state.HostShareReady,
		HostShareLastError:            state.HostShareLastError,
		HostShareMountSocketPath:      state.HostShareMountSocketPath,
		HostShareGuestPort:            state.HostShareGuestPort,
		HostShareVirtioFSPID:          state.HostShareVirtioFSPID,
		HostShareVirtioFSSocketPath:   state.HostShareVirtioFSSocketPath,
		HostShareVirtioFSLogPath:      state.HostShareVirtioFSLogPath,
		KubernetesSupport:             kubernetesSupport(metadata),
		KubernetesStatus:              kubernetesStatus(metadata, state),
		KubernetesReady:               state.KubernetesReady,
		KubernetesLastError:           state.KubernetesLastError,
		KubernetesKubeconfigPath:      firstNonEmpty(state.KubernetesKubeconfigPath, metadata.KubeconfigPath),
		KubernetesContext:             state.KubernetesContext,
		KubernetesAPIServerURL:        state.KubernetesAPIServerURL,
		KubernetesAPIServerPort:       state.KubernetesAPIServerPort,
		KubernetesAPIServerTargetPort: state.KubernetesAPIServerTargetPort,
		KubernetesRelayPID:            state.KubernetesRelayPID,
		KubernetesRelayLogPath:        state.KubernetesRelayLogPath,
		SerialLogPath:                 serialLogPath,
		BackendLogPath:                backendLogPath,
		EventLogPath:                  eventLogPath,
		StartedAt:                     state.StartedAt,
		UpdatedAt:                     state.UpdatedAt,
		LastStartDuration:             time.Duration(state.LastStartDurationMS) * time.Millisecond,
		LogPaths:                      logPaths(vmDir, metadata.Backend, state),
	}, nil
}

func hydrateMetadata(metadata *vmstore.Metadata) {
	if metadata.ExecUser == "" {
		metadata.ExecUser = "spind"
	}
	if metadata.Backend == "" {
		metadata.Backend = vmstore.BackendVirtualizationFramework
	}
}

func dockerCapabilitySupport(metadata vmstore.Metadata) string {
	if !isDockerHostImage(metadata) {
		return capabilityUnsupported
	}
	return capabilitySupported
}

func dockerCapabilityStatus(metadata vmstore.Metadata, ready bool) string {
	if dockerCapabilitySupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(ready)
}

func hostShareSupport(metadata vmstore.Metadata) string {
	if !isDockerHostImage(metadata) {
		return capabilityUnsupported
	}
	if metadata.Backend == vmstore.BackendCloudHypervisor && metadata.FromSnapshot {
		return capabilityUnsupported
	}
	return capabilitySupported
}

func hostShareStatus(metadata vmstore.Metadata, state vmstore.State) string {
	if hostShareSupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(state.HostShareReady)
}

func hostShareStatusReason(metadata vmstore.Metadata, state vmstore.State) string {
	if metadata.Backend == vmstore.BackendCloudHypervisor && metadata.FromSnapshot && isDockerHostImage(metadata) {
		return "cloud-hypervisor snapshot restore prioritizes saved-state restore speed"
	}
	return state.HostShareLastError
}

func kubernetesSupport(metadata vmstore.Metadata) string {
	if metadata.KindReady {
		return capabilitySupported
	}
	return capabilityUnsupported
}

func kubernetesStatus(metadata vmstore.Metadata, state vmstore.State) string {
	if kubernetesSupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(state.KubernetesReady)
}

func capabilityStatus(ready bool) string {
	if ready {
		return capabilityReady
	}
	return capabilityUnavailable
}

func isDockerHostImage(metadata vmstore.Metadata) bool {
	return strings.Contains(metadata.Image, "docker")
}

func logPaths(vmDir string, backend string, state vmstore.State) []string {
	candidates := []string{}
	switch backend {
	case vmstore.BackendCloudHypervisor:
		candidates = append(candidates,
			state.VMMLogPath,
			state.SerialLogPath,
			state.EventLogPath,
			state.HostShareVirtioFSLogPath,
			state.KubernetesRelayLogPath,
			filepath.Join(vmDir, "cloud-hypervisor.log"),
			filepath.Join(vmDir, "serial.log"),
			filepath.Join(vmDir, "cloud-hypervisor-event.log"),
			filepath.Join(vmDir, "virtiofsd.log"),
		)
	default:
		candidates = append(candidates, state.KubernetesRelayLogPath, filepath.Join(vmDir, vmstore.LogName))
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func namedLogPaths(vmDir string, backend string, state vmstore.State) (serial string, backendLog string, event string) {
	switch backend {
	case vmstore.BackendCloudHypervisor:
		serial = firstNonEmpty(state.SerialLogPath, filepath.Join(vmDir, "serial.log"))
		backendLog = firstNonEmpty(state.VMMLogPath, filepath.Join(vmDir, "cloud-hypervisor.log"))
		event = firstNonEmpty(state.EventLogPath, filepath.Join(vmDir, "cloud-hypervisor-event.log"))
	default:
		backendLog = filepath.Join(vmDir, vmstore.LogName)
	}
	return serial, backendLog, event
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
