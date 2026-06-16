package create

type Options struct {
	Name      string
	Image     string
	Snapshot  string
	CPUCount  int
	CPUSet    bool
	Memory    string
	MemorySet bool
}
