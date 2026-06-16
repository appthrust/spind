package kubeconfig

type MergeCommandOptions struct {
	KubeconfigPath    string
	Replace           bool
	SetCurrentContext bool
}

type UnmergeCommandOptions struct {
	KubeconfigPath string
}
