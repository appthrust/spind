package start

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/suin/spind/internal/spind/kubeconfig"
)

type KubeconfigMergeOptions struct {
	KubeconfigPath    string
	Replace           bool
	SetCurrentContext bool
}

type KubeconfigUnmergeOptions struct {
	KubeconfigPath string
}

func (m *Manager) KubeconfigMerge(name string, options KubeconfigMergeOptions) (string, error) {
	info, err := m.VMStatus(name)
	if err != nil {
		return "", err
	}
	if info.Status != "running" {
		return "", fmt.Errorf("VM %q: %w", name, ErrNotRunning)
	}
	if info.KubernetesSupport != capabilitySupported {
		return "", fmt.Errorf("VM %q is not a kind-ready VM", name)
	}
	sourcePath := info.KubernetesKubeconfigPath
	if sourcePath == "" {
		sourcePath = filepath.Join(info.VMDir, "kubeconfig")
	}
	if stat, err := os.Stat(sourcePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("VM kubeconfig %q: %w", sourcePath, ErrNotFound)
		}
		return "", fmt.Errorf("stat VM kubeconfig: %w", err)
	} else if stat.IsDir() {
		return "", fmt.Errorf("VM kubeconfig %q is a directory", sourcePath)
	}

	paths, err := kubeconfig.CandidatePaths(options.KubeconfigPath)
	if err != nil {
		return "", err
	}
	targetPath, err := kubeconfig.PathForMerge(paths)
	if err != nil {
		return "", err
	}
	err = kubeconfig.MergeFile(targetPath, sourcePath, "spind-"+name, kubeconfig.MergeOptions{
		Replace:           options.Replace,
		SetCurrentContext: options.SetCurrentContext,
	})
	if err != nil {
		return "", err
	}
	return targetPath, nil
}

func (m *Manager) KubeconfigUnmerge(name string, options KubeconfigUnmergeOptions) ([]string, error) {
	if _, err := m.VMStatus(name); err != nil {
		return nil, err
	}
	paths, err := kubeconfig.CandidatePaths(options.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(paths))
	for _, path := range paths {
		ok, err := kubeconfig.UnmergeFile(path, "spind-"+name)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, path)
		}
	}
	return changed, nil
}
