package config

import (
	"fmt"
	"os"
	"path/filepath"

	spindimage "github.com/suin/spind/internal/spind/image"
)

func NewFromEnv() (Config, error) {
	home, err := DefaultHome()
	if err != nil {
		return Config{}, err
	}
	if value := FirstEnv("SPIND_HOME", "KIDO_HOME"); value != "" {
		home = value
	}

	imageStore := filepath.Join(home, "images")
	if value := FirstEnv("SPIND_IMAGE_STORE", "KIDO_IMAGE_STORE"); value != "" {
		imageStore = value
	}
	templateStore := filepath.Join(home, "templates")
	if value := FirstEnv("SPIND_TEMPLATE_STORE", "KIDO_TEMPLATE_STORE"); value != "" {
		templateStore = value
	}
	builtinTemplateSource := FirstEnv("SPIND_BUILTIN_TEMPLATE_SOURCE", "KIDO_BUILTIN_TEMPLATE_SOURCE")
	if builtinTemplateSource == "" {
		builtinTemplateSource = spindimage.FindDefaultTemplateSource()
	}

	vmStore := filepath.Join(home, "vms")
	if value := FirstEnv("SPIND_VM_STORE", "KIDO_VM_STORE"); value != "" {
		vmStore = value
	}
	snapshotStore := filepath.Join(home, "snapshots")
	if value := FirstEnv("SPIND_SNAPSHOT_STORE", "KIDO_SNAPSHOT_STORE"); value != "" {
		snapshotStore = value
	}

	runnerPath := FirstEnv("SPIND_VZ_RUNNER", "KIDO_VZ_RUNNER")
	cloudHypervisorPath := FirstEnv("SPIND_CLOUD_HYPERVISOR", "KIDO_CLOUD_HYPERVISOR")
	if cloudHypervisorPath == "" {
		cloudHypervisorPath = FindExecutable("cloud-hypervisor")
	}

	return Config{
		Home:                  home,
		ImageStore:            imageStore,
		TemplateStore:         templateStore,
		BuiltinTemplateSource: builtinTemplateSource,
		VMStore:               vmStore,
		SnapshotStore:         snapshotStore,
		RunnerPath:            runnerPath,
		CloudHypervisorPath:   cloudHypervisorPath,
	}, nil
}

func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".spind"), nil
}
