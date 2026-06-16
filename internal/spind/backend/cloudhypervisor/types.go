package cloudhypervisor

type Config struct {
	Name                string `json:"name"`
	KernelPath          string `json:"kernelPath"`
	InitramfsPath       string `json:"initramfsPath"`
	DiskPath            string `json:"diskPath"`
	Disks               []Disk `json:"disks,omitempty"`
	APISocketPath       string `json:"apiSocketPath"`
	VsockSocketPath     string `json:"vsockSocketPath"`
	SerialLogPath       string `json:"serialLogPath"`
	VMMLogPath          string `json:"vmmLogPath"`
	EventLogPath        string `json:"eventLogPath"`
	GuestCID            uint32 `json:"guestCID"`
	NetBackend          string `json:"netBackend,omitempty"`
	NetSocketPath       string `json:"netSocketPath,omitempty"`
	NetLogPath          string `json:"netLogPath,omitempty"`
	NetTapName          string `json:"netTapName,omitempty"`
	NetMAC              string `json:"netMac,omitempty"`
	HostSharePath       string `json:"hostSharePath,omitempty"`
	HostShareTag        string `json:"hostShareTag,omitempty"`
	HostShareSocketPath string `json:"hostShareSocketPath,omitempty"`
	HostShareLogPath    string `json:"hostShareLogPath,omitempty"`
	HostSharePort       uint32 `json:"hostSharePort,omitempty"`
	ExecPort            uint32 `json:"execPort"`
	CPUCount            int    `json:"cpuCount"`
	MemoryMiB           int    `json:"memoryMiB"`
	KernelCommandLine   string `json:"kernelCommandLine"`
}

type Disk struct {
	Path      string `json:"path"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
	ImageType string `json:"imageType,omitempty"`
}
