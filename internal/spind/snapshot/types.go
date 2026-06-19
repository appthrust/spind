package snapshot

import (
	"time"

	spindimage "github.com/suin/spind/internal/spind/image"
	spindkind "github.com/suin/spind/internal/spind/kind"
)

type Metadata struct {
	Name              string                    `json:"name"`
	SourceVM          string                    `json:"sourceVM"`
	Image             string                    `json:"image,omitempty"`
	ImageType         string                    `json:"imageType,omitempty"`
	CreatedAt         time.Time                 `json:"createdAt"`
	ProjectName       string                    `json:"projectName,omitempty"`
	ProjectRoot       string                    `json:"projectRoot,omitempty"`
	ProjectRole       string                    `json:"projectRole,omitempty"`
	Backend           string                    `json:"backend"`
	Architecture      string                    `json:"architecture,omitempty"`
	ExecUser          string                    `json:"execUser,omitempty"`
	CPUCount          int                       `json:"cpuCount"`
	MemoryMiB         int                       `json:"memoryMiB"`
	ExecPort          uint32                    `json:"execPort"`
	KernelCommandLine string                    `json:"kernelCommandLine,omitempty"`
	MachineIdentifier string                    `json:"machineIdentifier,omitempty"`
	NetworkMAC        string                    `json:"networkMac,omitempty"`
	KindReady         bool                      `json:"kindReady,omitempty"`
	K8sDistribution   string                    `json:"k8sDistribution,omitempty"`
	Disks             []spindimage.DiskMetadata `json:"disks,omitempty"`
}

type Info struct {
	Name            string
	SourceVM        string
	Backend         string
	CreatedAt       time.Time
	CPUCount        int
	MemoryMiB       int
	ExecPort        uint32
	KernelCommand   string
	DiskSizeBytes   int64
	StateSizeBytes  int64
	TotalSizeBytes  int64
	Health          string
	HealthMessage   string
	SnapshotDir     string
	KindReady       bool
	K8sDistribution string
	KindMetadata    spindkind.Metadata
	KindTemplate    bool
	Artifacts       []ArtifactInfo
}

type ArtifactInfo struct {
	Name  string
	Path  string
	Size  int64
	Found bool
}

type CreateOptions struct {
	K8s            string
	KubeconfigPath string
	Context        string
}
