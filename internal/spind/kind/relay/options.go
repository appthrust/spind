package relay

type Options struct {
	VMName     string
	ListenPort int
	TargetPort int
	GuestIP    string
	TCPForward string
	Vsock      string
	GuestPort  uint32
	SSHSocket  string
	SSHKey     string
	SSHUser    string
}
