package merge

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/kubeconfig"
	"github.com/suin/spind/internal/spind/store"
	"github.com/suin/spind/internal/spind/vm/status"
	"github.com/suin/spind/internal/spind/vmstore"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	path, err := Merge(cfg, options)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	fmt.Fprintf(stdout, "merged kubeconfig for VM %q into %s\n", options.Name, path)
	return 0
}

func Merge(cfg config.Config, options Options) (string, error) {
	info, err := status.Info(cfg, options.Name)
	if err != nil {
		return "", err
	}
	if info.Status != "running" {
		return "", fmt.Errorf("VM %q: %w", options.Name, vmstore.ErrNotRunning)
	}
	if info.KubernetesSupport != "supported" {
		return "", fmt.Errorf("VM %q is not a kind-ready VM", options.Name)
	}
	sourcePath := info.KubernetesKubeconfigPath
	if sourcePath == "" {
		sourcePath = filepath.Join(info.VMDir, "kubeconfig")
	}
	if stat, err := os.Stat(sourcePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("VM kubeconfig %q: %w", sourcePath, store.ErrNotFound)
		}
		return "", fmt.Errorf("stat VM kubeconfig: %w", err)
	} else if stat.IsDir() {
		return "", fmt.Errorf("VM kubeconfig %q is a directory", sourcePath)
	}

	paths, err := kubeconfig.CandidatePaths(options.Kubeconfig)
	if err != nil {
		return "", err
	}
	targetPath, err := kubeconfig.PathForMerge(paths)
	if err != nil {
		return "", err
	}
	err = kubeconfig.MergeFile(targetPath, sourcePath, "spind-"+options.Name, kubeconfig.MergeOptions{
		Replace:           options.Replace,
		SetCurrentContext: options.SetCurrentContext,
	})
	if err != nil {
		return "", err
	}
	return targetPath, nil
}
