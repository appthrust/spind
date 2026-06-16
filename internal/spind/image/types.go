package image

import "time"

type Metadata struct {
	Name                             string         `json:"name"`
	ImageType                        string         `json:"imageType,omitempty"`
	Architecture                     string         `json:"architecture"`
	CreatedAt                        time.Time      `json:"createdAt"`
	KernelCommandLine                string         `json:"kernelCommandLine"`
	ExecUser                         string         `json:"execUser,omitempty"`
	CPUCount                         int            `json:"cpuCount,omitempty"`
	MemoryMiB                        int            `json:"memoryMiB,omitempty"`
	SSHAuthorizedKeyCommandLineParam string         `json:"sshAuthorizedKeyCommandLineParam,omitempty"`
	Disks                            []DiskMetadata `json:"disks,omitempty"`
}

type DiskMetadata struct {
	Name      string              `json:"name"`
	ReadOnly  bool                `json:"readOnly,omitempty"`
	ImageType string              `json:"imageType,omitempty"`
	Create    *DiskCreateMetadata `json:"create,omitempty"`
}

type DiskCreateMetadata struct {
	Size   string `json:"size,omitempty"`
	FSType string `json:"fsType,omitempty"`
	Label  string `json:"label,omitempty"`
}

type Info struct {
	Name              string
	Architecture      string
	CreatedAt         time.Time
	KernelCommandLine string
	ExecUser          string
	SizeBytes         int64
	ImageDir          string
	Health            string
	HealthMessage     string
}

type BuildOptions struct {
	Config   string
	Force    bool
	DataSize string
}

type BuildResult struct {
	Name         string
	ImageDir     string
	TemplatePath string
	SizeBytes    int64
	CreatedAt    time.Time
}

type DeleteOptions struct {
	Force bool
}

type DeleteResult struct {
	SizeBytes     int64
	ReferencedVMs []string
}
