package docker

type ContainerSummary struct {
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
	Ports  []PortMapping     `json:"Ports"`
}

type PortMapping struct {
	IP          string `json:"IP"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort"`
	Type        string `json:"Type"`
}
