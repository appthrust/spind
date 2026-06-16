package unmerge

import (
	"fmt"
	"io"

	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/kubeconfig"
	"github.com/suin/spind/internal/spind/vm/status"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	paths, err := Unmerge(cfg, options)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if len(paths) == 0 {
		fmt.Fprintf(stdout, "no kubeconfig entries for VM %q were removed\n", options.Name)
		return 0
	}
	for _, path := range paths {
		fmt.Fprintf(stdout, "removed kubeconfig entries for VM %q from %s\n", options.Name, path)
	}
	return 0
}

func Unmerge(cfg config.Config, options Options) ([]string, error) {
	if _, err := status.Info(cfg, options.Name); err != nil {
		return nil, err
	}
	paths, err := kubeconfig.CandidatePaths(options.Kubeconfig)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(paths))
	for _, path := range paths {
		ok, err := kubeconfig.UnmergeFile(path, "spind-"+options.Name)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, path)
		}
	}
	return changed, nil
}
