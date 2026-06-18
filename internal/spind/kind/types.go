package kind

import "time"

const (
	DistributionKind       = "kind"
	DistributionK3d        = "k3d"
	DirName                = "kind"
	MetadataName           = "metadata.json"
	KubeconfigTemplateName = "kubeconfig.template"
)

type Metadata struct {
	KindReady               bool          `json:"kindReady"`
	Distribution            string        `json:"distribution,omitempty"`
	SourceKubeconfigPath    string        `json:"sourceKubeconfigPath,omitempty"`
	SourceContext           string        `json:"sourceContext,omitempty"`
	SourceCluster           string        `json:"sourceCluster,omitempty"`
	SourceUser              string        `json:"sourceUser,omitempty"`
	SourceServer            string        `json:"sourceServer,omitempty"`
	APIServerTargetPort     int           `json:"apiServerTargetPort,omitempty"`
	RegistryTargetPort      int           `json:"registryTargetPort,omitempty"`
	RegistryHost            string        `json:"registryHost,omitempty"`
	RegistryHostFromCluster string        `json:"registryHostFromCluster,omitempty"`
	Nodes                   []NodeSummary `json:"nodes,omitempty"`
	ReadyCheck              string        `json:"readyCheck,omitempty"`
	ReadyCheckCompletedAt   time.Time     `json:"readyCheckCompletedAt,omitempty"`
}

type NodeSummary struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type SnapshotOptions struct {
	Distribution   string
	KubeconfigPath string
	Context        string
}
