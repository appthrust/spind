package output

import spindvm "github.com/suin/spind/internal/spind/vmstore"

type VMInfo struct {
	Name                          string           `json:"name"`
	Status                        string           `json:"status"`
	Backend                       string           `json:"backend"`
	PID                           int              `json:"pid,omitempty"`
	ExecReady                     bool             `json:"execReady"`
	Image                         string           `json:"image,omitempty"`
	FromSnapshot                  bool             `json:"fromSnapshot"`
	SourceSnapshot                string           `json:"sourceSnapshot,omitempty"`
	RestoreMode                   string           `json:"restoreMode"`
	VMDir                         string           `json:"vmDir"`
	StatePath                     string           `json:"statePath"`
	ExecSocketPath                string           `json:"execSocketPath,omitempty"`
	CloudHypervisorAPISocketPath  string           `json:"cloudHypervisorAPISocketPath,omitempty"`
	CloudHypervisorVsockPath      string           `json:"cloudHypervisorVsockPath,omitempty"`
	Docker                        DockerStatus     `json:"docker"`
	Kubernetes                    KubernetesStatus `json:"kubernetes"`
	DockerSocketPath              string           `json:"dockerSocketPath,omitempty"`
	DockerEndpointURI             string           `json:"dockerEndpointUri,omitempty"`
	DockerRelayPID                int              `json:"dockerRelayPid,omitempty"`
	DockerGuestPort               uint32           `json:"dockerGuestPort,omitempty"`
	DockerAPISupport              string           `json:"dockerApiSupport,omitempty"`
	DockerAPIStatus               string           `json:"dockerApiStatus,omitempty"`
	DockerAPIReady                bool             `json:"dockerApiReady"`
	DockerAvailable               bool             `json:"dockerAvailable"`
	DockerLastError               string           `json:"dockerLastError,omitempty"`
	DockerLogPath                 string           `json:"dockerLogPath,omitempty"`
	DockerNetworkSupport          string           `json:"dockerNetworkSupport,omitempty"`
	DockerNetworkStatus           string           `json:"dockerNetworkStatus,omitempty"`
	DockerNetworkReady            bool             `json:"dockerNetworkReady"`
	DockerNetworkLastError        string           `json:"dockerNetworkLastError,omitempty"`
	DockerGuestIPAddress          string           `json:"dockerGuestIpAddress,omitempty"`
	DockerDefaultRouteReady       bool             `json:"dockerDefaultRouteReady"`
	DockerDNSReady                bool             `json:"dockerDnsReady"`
	DockerBridgeReady             bool             `json:"dockerBridgeReady"`
	DockerPullReady               bool             `json:"dockerPullReady"`
	DockerTCPForwardSocketPath    string           `json:"dockerTcpForwardSocketPath,omitempty"`
	DockerTCPForwardGuestPort     uint32           `json:"dockerTcpForwardGuestPort,omitempty"`
	DockerPortRelayPID            int              `json:"dockerPortRelayPid,omitempty"`
	DockerPortRelaySupport        string           `json:"dockerPortRelaySupport,omitempty"`
	DockerPortRelayStatus         string           `json:"dockerPortRelayStatus,omitempty"`
	DockerPortRelayReady          bool             `json:"dockerPortRelayReady"`
	DockerPortRelayLogPath        string           `json:"dockerPortRelayLogPath,omitempty"`
	DockerPublishedPorts          []string         `json:"dockerPublishedPorts,omitempty"`
	DockerNetworkBackend          string           `json:"dockerNetworkBackend,omitempty"`
	DockerNetworkBackendPID       int              `json:"dockerNetworkBackendPid,omitempty"`
	DockerNetworkSocketPath       string           `json:"dockerNetworkSocketPath,omitempty"`
	DockerNetworkBackendLogPath   string           `json:"dockerNetworkBackendLogPath,omitempty"`
	DockerTapName                 string           `json:"dockerTapName,omitempty"`
	DockerGuestMAC                string           `json:"dockerGuestMac,omitempty"`
	HostShareSupport              string           `json:"hostShareSupport,omitempty"`
	HostShareStatus               string           `json:"hostShareStatus,omitempty"`
	HostShareStatusReason         string           `json:"hostShareStatusReason,omitempty"`
	HostSharePath                 string           `json:"hostSharePath,omitempty"`
	HostShareReady                bool             `json:"hostShareReady"`
	HostShareLastError            string           `json:"hostShareLastError,omitempty"`
	HostShareMountSocketPath      string           `json:"hostShareMountSocketPath,omitempty"`
	HostShareGuestPort            uint32           `json:"hostShareGuestPort,omitempty"`
	HostShareVirtioFSPID          int              `json:"hostShareVirtiofsPid,omitempty"`
	HostShareVirtioFSSocketPath   string           `json:"hostShareVirtiofsSocketPath,omitempty"`
	HostShareVirtioFSLogPath      string           `json:"hostShareVirtiofsLogPath,omitempty"`
	KubernetesSupport             string           `json:"kubernetesSupport,omitempty"`
	KubernetesStatus              string           `json:"kubernetesStatus,omitempty"`
	KubernetesReady               bool             `json:"kubernetesReady"`
	KubernetesLastError           string           `json:"kubernetesLastError,omitempty"`
	KubernetesKubeconfigPath      string           `json:"kubernetesKubeconfigPath,omitempty"`
	KubernetesContext             string           `json:"kubernetesContext,omitempty"`
	KubernetesAPIServerURL        string           `json:"kubernetesApiServerUrl,omitempty"`
	KubernetesAPIServerPort       int              `json:"kubernetesApiServerPort,omitempty"`
	KubernetesAPIServerTargetPort int              `json:"kubernetesApiServerTargetPort,omitempty"`
	KubernetesRelayPID            int              `json:"kubernetesRelayPid,omitempty"`
	KubernetesRelayLogPath        string           `json:"kubernetesRelayLogPath,omitempty"`
	SerialLogPath                 string           `json:"serialLogPath,omitempty"`
	BackendLogPath                string           `json:"backendLogPath,omitempty"`
	EventLogPath                  string           `json:"eventLogPath,omitempty"`
	StartedAt                     string           `json:"startedAt,omitempty"`
	UpdatedAt                     string           `json:"updatedAt,omitempty"`
	LastStartDuration             string           `json:"lastStartDuration,omitempty"`
	LogPaths                      []string         `json:"logPaths,omitempty"`
}

type DockerStatus struct {
	API         DockerCapability `json:"api"`
	Network     DockerCapability `json:"network"`
	PortPublish DockerCapability `json:"portPublish"`
	SharedPath  DockerCapability `json:"sharedPath"`
}

type DockerCapability struct {
	Support  string `json:"support"`
	Ready    string `json:"ready"`
	Reason   string `json:"reason,omitempty"`
	Log      string `json:"log,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Path     string `json:"path,omitempty"`
}

type KubernetesStatus struct {
	Support    string `json:"support"`
	Ready      string `json:"ready"`
	Reason     string `json:"reason,omitempty"`
	Log        string `json:"log,omitempty"`
	Kubeconfig string `json:"kubeconfig,omitempty"`
	Context    string `json:"context,omitempty"`
	APIServer  string `json:"apiServer,omitempty"`
	RelayPID   int    `json:"relayPid,omitempty"`
}

func NewVMInfo(info spindvm.Info) VMInfo {
	return VMInfo{
		Name:                          info.Name,
		Status:                        info.Status,
		Backend:                       info.Backend,
		PID:                           info.PID,
		ExecReady:                     info.ExecReady,
		Image:                         info.Image,
		FromSnapshot:                  info.FromSnapshot,
		SourceSnapshot:                info.SourceSnapshot,
		RestoreMode:                   info.RestoreMode,
		VMDir:                         info.VMDir,
		StatePath:                     info.StatePath,
		ExecSocketPath:                info.ExecSocketPath,
		CloudHypervisorAPISocketPath:  info.CloudHypervisorAPISocketPath,
		CloudHypervisorVsockPath:      info.CloudHypervisorVsockPath,
		Docker:                        newDockerStatus(info),
		Kubernetes:                    newKubernetesStatus(info),
		DockerSocketPath:              info.DockerSocketPath,
		DockerEndpointURI:             info.DockerEndpointURI,
		DockerRelayPID:                info.DockerRelayPID,
		DockerGuestPort:               info.DockerGuestPort,
		DockerAPISupport:              info.DockerAPISupport,
		DockerAPIStatus:               info.DockerAPIStatus,
		DockerAPIReady:                info.DockerAPIReady,
		DockerAvailable:               info.DockerAvailable,
		DockerLastError:               info.DockerLastError,
		DockerLogPath:                 info.DockerLogPath,
		DockerNetworkSupport:          info.DockerNetworkSupport,
		DockerNetworkStatus:           info.DockerNetworkStatus,
		DockerNetworkReady:            info.DockerNetworkReady,
		DockerNetworkLastError:        info.DockerNetworkLastError,
		DockerGuestIPAddress:          info.DockerGuestIPAddress,
		DockerDefaultRouteReady:       info.DockerDefaultRouteReady,
		DockerDNSReady:                info.DockerDNSReady,
		DockerBridgeReady:             info.DockerBridgeReady,
		DockerPullReady:               info.DockerPullReady,
		DockerTCPForwardSocketPath:    info.DockerTCPForwardSocketPath,
		DockerTCPForwardGuestPort:     info.DockerTCPForwardGuestPort,
		DockerPortRelayPID:            info.DockerPortRelayPID,
		DockerPortRelaySupport:        info.DockerPortRelaySupport,
		DockerPortRelayStatus:         info.DockerPortRelayStatus,
		DockerPortRelayReady:          info.DockerPortRelayReady,
		DockerPortRelayLogPath:        info.DockerPortRelayLogPath,
		DockerPublishedPorts:          info.DockerPublishedPorts,
		DockerNetworkBackend:          info.DockerNetworkBackend,
		DockerNetworkBackendPID:       info.DockerNetworkBackendPID,
		DockerNetworkSocketPath:       info.DockerNetworkSocketPath,
		DockerNetworkBackendLogPath:   info.DockerNetworkBackendLogPath,
		DockerTapName:                 info.DockerTapName,
		DockerGuestMAC:                info.DockerGuestMAC,
		HostShareSupport:              info.HostShareSupport,
		HostShareStatus:               info.HostShareStatus,
		HostShareStatusReason:         info.HostShareStatusReason,
		HostSharePath:                 info.HostSharePath,
		HostShareReady:                info.HostShareReady,
		HostShareLastError:            info.HostShareLastError,
		HostShareMountSocketPath:      info.HostShareMountSocketPath,
		HostShareGuestPort:            info.HostShareGuestPort,
		HostShareVirtioFSPID:          info.HostShareVirtioFSPID,
		HostShareVirtioFSSocketPath:   info.HostShareVirtioFSSocketPath,
		HostShareVirtioFSLogPath:      info.HostShareVirtioFSLogPath,
		KubernetesSupport:             info.KubernetesSupport,
		KubernetesStatus:              info.KubernetesStatus,
		KubernetesReady:               info.KubernetesReady,
		KubernetesLastError:           info.KubernetesLastError,
		KubernetesKubeconfigPath:      info.KubernetesKubeconfigPath,
		KubernetesContext:             info.KubernetesContext,
		KubernetesAPIServerURL:        info.KubernetesAPIServerURL,
		KubernetesAPIServerPort:       info.KubernetesAPIServerPort,
		KubernetesAPIServerTargetPort: info.KubernetesAPIServerTargetPort,
		KubernetesRelayPID:            info.KubernetesRelayPID,
		KubernetesRelayLogPath:        info.KubernetesRelayLogPath,
		SerialLogPath:                 info.SerialLogPath,
		BackendLogPath:                info.BackendLogPath,
		EventLogPath:                  info.EventLogPath,
		StartedAt:                     FormatTime(info.StartedAt),
		UpdatedAt:                     FormatTime(info.UpdatedAt),
		LastStartDuration:             FormatOptionalDuration(info.LastStartDuration),
		LogPaths:                      info.LogPaths,
	}
}

func newKubernetesStatus(info spindvm.Info) KubernetesStatus {
	return KubernetesStatus{
		Support:    info.KubernetesSupport,
		Ready:      info.KubernetesStatus,
		Reason:     capabilityReason(info.KubernetesStatus, info.KubernetesLastError),
		Log:        unavailableLog(info.KubernetesStatus, info.KubernetesRelayLogPath),
		Kubeconfig: info.KubernetesKubeconfigPath,
		Context:    info.KubernetesContext,
		APIServer:  info.KubernetesAPIServerURL,
		RelayPID:   info.KubernetesRelayPID,
	}
}

func newDockerStatus(info spindvm.Info) DockerStatus {
	return DockerStatus{
		API: DockerCapability{
			Support:  info.DockerAPISupport,
			Ready:    info.DockerAPIStatus,
			Reason:   unavailableReason(info.DockerAPIStatus, info.DockerLastError),
			Log:      unavailableLog(info.DockerAPIStatus, info.DockerLogPath),
			Endpoint: info.DockerEndpointURI,
		},
		Network: DockerCapability{
			Support: info.DockerNetworkSupport,
			Ready:   info.DockerNetworkStatus,
			Reason:  unavailableReason(info.DockerNetworkStatus, info.DockerNetworkLastError),
			Log:     unavailableLog(info.DockerNetworkStatus, info.BackendLogPath),
		},
		PortPublish: DockerCapability{
			Support: info.DockerPortRelaySupport,
			Ready:   info.DockerPortRelayStatus,
			Reason:  unavailableReason(info.DockerPortRelayStatus, info.DockerLastError),
			Log:     unavailableLog(info.DockerPortRelayStatus, info.DockerPortRelayLogPath),
		},
		SharedPath: DockerCapability{
			Support: info.HostShareSupport,
			Ready:   info.HostShareStatus,
			Reason:  capabilityReason(info.HostShareStatus, info.HostShareStatusReason),
			Log:     unavailableLog(info.HostShareStatus, info.HostShareVirtioFSLogPath),
			Path:    info.HostSharePath,
		},
	}
}

func unavailableReason(status string, reason string) string {
	if status != "unavailable" {
		return ""
	}
	return reason
}

func unavailableLog(status string, logPath string) string {
	if status != "unavailable" {
		return ""
	}
	return logPath
}

func capabilityReason(status string, reason string) string {
	switch status {
	case "unavailable", "not-applicable":
		return reason
	default:
		return ""
	}
}
