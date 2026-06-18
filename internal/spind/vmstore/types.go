package vmstore

import (
	"errors"
	"time"
)

const (
	BackendVirtualizationFramework = "virtualization-framework"
	BackendCloudHypervisor         = "cloud-hypervisor"
)

var ErrNotRunning = errors.New("not running")

type Metadata struct {
	Name             string    `json:"name"`
	Image            string    `json:"image,omitempty"`
	ImageType        string    `json:"imageType,omitempty"`
	Backend          string    `json:"backend,omitempty"`
	Architecture     string    `json:"architecture"`
	ExecUser         string    `json:"execUser,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	ProjectName      string    `json:"projectName,omitempty"`
	ProjectRoot      string    `json:"projectRoot,omitempty"`
	ProjectRole      string    `json:"projectRole,omitempty"`
	FromSnapshot     bool      `json:"fromSnapshot,omitempty"`
	SourceSnapshot   string    `json:"sourceSnapshot,omitempty"`
	RestoreStatePath string    `json:"restoreStatePath,omitempty"`
	CPUCount         int       `json:"cpuCount,omitempty"`
	MemoryMiB        int       `json:"memoryMiB,omitempty"`
	ExecPort         uint32    `json:"execPort,omitempty"`
	KindReady        bool      `json:"kindReady,omitempty"`
	K8sDistribution  string    `json:"k8sDistribution,omitempty"`
	KubeconfigPath   string    `json:"kubeconfigPath,omitempty"`
}

type State struct {
	Status                         string    `json:"status"`
	Backend                        string    `json:"backend,omitempty"`
	PID                            int       `json:"pid,omitempty"`
	ExecSocketPath                 string    `json:"execSocketPath,omitempty"`
	ControlSocketPath              string    `json:"controlSocketPath,omitempty"`
	ExecPort                       uint32    `json:"execPort,omitempty"`
	CloudHypervisorAPISocketPath   string    `json:"cloudHypervisorAPISocketPath,omitempty"`
	CloudHypervisorVsockSocketPath string    `json:"cloudHypervisorVsockSocketPath,omitempty"`
	GuestCID                       uint32    `json:"guestCID,omitempty"`
	SerialLogPath                  string    `json:"serialLogPath,omitempty"`
	VMMLogPath                     string    `json:"vmmLogPath,omitempty"`
	EventLogPath                   string    `json:"eventLogPath,omitempty"`
	ExecReady                      bool      `json:"execReady,omitempty"`
	DockerSocketPath               string    `json:"dockerSocketPath,omitempty"`
	DockerEndpointURI              string    `json:"dockerEndpointUri,omitempty"`
	DockerRelayPID                 int       `json:"dockerRelayPid,omitempty"`
	DockerGuestPort                uint32    `json:"dockerGuestPort,omitempty"`
	DockerAPIReady                 bool      `json:"dockerApiReady,omitempty"`
	DockerAvailable                bool      `json:"dockerAvailable,omitempty"`
	DockerLastError                string    `json:"dockerLastError,omitempty"`
	DockerLogPath                  string    `json:"dockerLogPath,omitempty"`
	DockerNetworkReady             bool      `json:"dockerNetworkReady,omitempty"`
	DockerNetworkLastError         string    `json:"dockerNetworkLastError,omitempty"`
	DockerGuestIPAddress           string    `json:"dockerGuestIpAddress,omitempty"`
	DockerDefaultRouteReady        bool      `json:"dockerDefaultRouteReady,omitempty"`
	DockerDNSReady                 bool      `json:"dockerDnsReady,omitempty"`
	DockerBridgeReady              bool      `json:"dockerBridgeReady,omitempty"`
	DockerPullReady                bool      `json:"dockerPullReady,omitempty"`
	DockerTCPForwardSocketPath     string    `json:"dockerTcpForwardSocketPath,omitempty"`
	DockerTCPForwardGuestPort      uint32    `json:"dockerTcpForwardGuestPort,omitempty"`
	DockerPortRelayPID             int       `json:"dockerPortRelayPid,omitempty"`
	DockerPortRelayReady           bool      `json:"dockerPortRelayReady,omitempty"`
	DockerPortRelayLogPath         string    `json:"dockerPortRelayLogPath,omitempty"`
	DockerPublishedPorts           []string  `json:"dockerPublishedPorts,omitempty"`
	DockerNetworkBackend           string    `json:"dockerNetworkBackend,omitempty"`
	DockerNetworkBackendPID        int       `json:"dockerNetworkBackendPid,omitempty"`
	DockerNetworkSocketPath        string    `json:"dockerNetworkSocketPath,omitempty"`
	DockerNetworkBackendLogPath    string    `json:"dockerNetworkBackendLogPath,omitempty"`
	DockerTapName                  string    `json:"dockerTapName,omitempty"`
	DockerGuestMAC                 string    `json:"dockerGuestMac,omitempty"`
	HostSharePath                  string    `json:"hostSharePath,omitempty"`
	HostShareReady                 bool      `json:"hostShareReady,omitempty"`
	HostShareLastError             string    `json:"hostShareLastError,omitempty"`
	HostShareMountSocketPath       string    `json:"hostShareMountSocketPath,omitempty"`
	HostShareGuestPort             uint32    `json:"hostShareGuestPort,omitempty"`
	HostShareVirtioFSPID           int       `json:"hostShareVirtiofsPid,omitempty"`
	HostShareVirtioFSSocketPath    string    `json:"hostShareVirtiofsSocketPath,omitempty"`
	HostShareVirtioFSLogPath       string    `json:"hostShareVirtiofsLogPath,omitempty"`
	KubernetesReady                bool      `json:"kubernetesReady,omitempty"`
	KubernetesLastError            string    `json:"kubernetesLastError,omitempty"`
	KubernetesKubeconfigPath       string    `json:"kubernetesKubeconfigPath,omitempty"`
	KubernetesContext              string    `json:"kubernetesContext,omitempty"`
	KubernetesAPIServerURL         string    `json:"kubernetesApiServerUrl,omitempty"`
	KubernetesAPIServerPort        int       `json:"kubernetesApiServerPort,omitempty"`
	KubernetesAPIServerTargetPort  int       `json:"kubernetesApiServerTargetPort,omitempty"`
	KubernetesRelayPID             int       `json:"kubernetesRelayPid,omitempty"`
	KubernetesRelayLogPath         string    `json:"kubernetesRelayLogPath,omitempty"`
	RegistryReady                  bool      `json:"registryReady,omitempty"`
	RegistryLastError              string    `json:"registryLastError,omitempty"`
	RegistryURL                    string    `json:"registryUrl,omitempty"`
	RegistryPort                   int       `json:"registryPort,omitempty"`
	RegistryTargetPort             int       `json:"registryTargetPort,omitempty"`
	RegistryRelayPID               int       `json:"registryRelayPid,omitempty"`
	RegistryRelayLogPath           string    `json:"registryRelayLogPath,omitempty"`
	RegistryHostFromCluster        string    `json:"registryHostFromCluster,omitempty"`
	RegistryLocalHostingUpdated    bool      `json:"registryLocalHostingUpdated,omitempty"`
	StartedAt                      time.Time `json:"startedAt,omitempty"`
	LastStartDurationMS            int64     `json:"lastStartDurationMs,omitempty"`
	UpdatedAt                      time.Time `json:"updatedAt"`
}

type Info struct {
	Name                          string
	Status                        string
	Backend                       string
	PID                           int
	ExecReady                     bool
	Image                         string
	FromSnapshot                  bool
	SourceSnapshot                string
	RestoreMode                   string
	VMDir                         string
	StatePath                     string
	ExecSocketPath                string
	CloudHypervisorAPISocketPath  string
	CloudHypervisorVsockPath      string
	DockerSocketPath              string
	DockerEndpointURI             string
	DockerRelayPID                int
	DockerGuestPort               uint32
	DockerAPISupport              string
	DockerAPIStatus               string
	DockerAPIReady                bool
	DockerAvailable               bool
	DockerLastError               string
	DockerLogPath                 string
	DockerNetworkSupport          string
	DockerNetworkStatus           string
	DockerNetworkReady            bool
	DockerNetworkLastError        string
	DockerGuestIPAddress          string
	DockerDefaultRouteReady       bool
	DockerDNSReady                bool
	DockerBridgeReady             bool
	DockerPullReady               bool
	DockerTCPForwardSocketPath    string
	DockerTCPForwardGuestPort     uint32
	DockerPortRelayPID            int
	DockerPortRelaySupport        string
	DockerPortRelayStatus         string
	DockerPortRelayReady          bool
	DockerPortRelayLogPath        string
	DockerPublishedPorts          []string
	DockerNetworkBackend          string
	DockerNetworkBackendPID       int
	DockerNetworkSocketPath       string
	DockerNetworkBackendLogPath   string
	DockerTapName                 string
	DockerGuestMAC                string
	HostShareSupport              string
	HostShareStatus               string
	HostShareStatusReason         string
	HostSharePath                 string
	HostShareReady                bool
	HostShareLastError            string
	HostShareMountSocketPath      string
	HostShareGuestPort            uint32
	HostShareVirtioFSPID          int
	HostShareVirtioFSSocketPath   string
	HostShareVirtioFSLogPath      string
	KubernetesSupport             string
	KubernetesStatus              string
	KubernetesReady               bool
	KubernetesLastError           string
	KubernetesKubeconfigPath      string
	KubernetesContext             string
	KubernetesAPIServerURL        string
	KubernetesAPIServerPort       int
	KubernetesAPIServerTargetPort int
	KubernetesRelayPID            int
	KubernetesRelayLogPath        string
	RegistryReady                 bool
	RegistryLastError             string
	RegistryURL                   string
	RegistryPort                  int
	RegistryTargetPort            int
	RegistryRelayPID              int
	RegistryRelayLogPath          string
	RegistryHostFromCluster       string
	RegistryLocalHostingUpdated   bool
	SerialLogPath                 string
	BackendLogPath                string
	EventLogPath                  string
	StartedAt                     time.Time
	UpdatedAt                     time.Time
	LastStartDuration             time.Duration
	LogPaths                      []string
}

type DeleteOptions struct {
	Force             bool
	UnmergeKubeconfig bool
}

type DeleteResult struct {
	SizeBytes         int64
	KubeconfigRemoved []string
}
