package vz

type RunnerConfig struct {
	Name                string `json:"name"`
	KernelPath          string `json:"kernelPath"`
	InitramfsPath       string `json:"initramfsPath"`
	DiskPath            string `json:"diskPath,omitempty"`
	Disks               []Disk `json:"disks,omitempty"`
	LogPath             string `json:"logPath"`
	ExecSocketPath      string `json:"execSocketPath"`
	ControlSocketPath   string `json:"controlSocketPath"`
	RestoreStatePath    string `json:"restoreStatePath,omitempty"`
	MachineIdentifier   string `json:"machineIdentifier,omitempty"`
	ExecPort            uint32 `json:"execPort"`
	DockerSocketPath    string `json:"dockerSocketPath,omitempty"`
	DockerPort          uint32 `json:"dockerPort,omitempty"`
	TCPForwardPath      string `json:"tcpForwardPath,omitempty"`
	TCPForwardPort      uint32 `json:"tcpForwardPort,omitempty"`
	NetworkMAC          string `json:"networkMac,omitempty"`
	HostSharePath       string `json:"hostSharePath,omitempty"`
	HostShareTag        string `json:"hostShareTag,omitempty"`
	HostShareSocketPath string `json:"hostShareSocketPath,omitempty"`
	HostSharePort       uint32 `json:"hostSharePort,omitempty"`
	CPUCount            int    `json:"cpuCount"`
	MemoryMiB           int    `json:"memoryMiB"`
	KernelCommandLine   string `json:"kernelCommandLine"`
}

type Disk struct {
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}
