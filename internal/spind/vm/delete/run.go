package delete

import (
	"context"
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	manager := vmstart.NewManagerFromConfig(cfg)
	for _, name := range options.Names {
		result, err := manager.DeleteVM(ctx, name, spindvm.DeleteOptions{
			Force:             options.Force,
			UnmergeKubeconfig: options.UnmergeKubeconfig,
		})
		if err != nil {
			return cliruntime.ExitForError(stderr, err)
		}
		for _, path := range result.KubeconfigRemoved {
			fmt.Fprintf(stdout, "removed kubeconfig entries for VM %q from %s\n", name, path)
		}
		fmt.Fprintf(stdout, "deleted VM %q (%s)\n", name, output.FormatSize(result.SizeBytes))
	}
	return 0
}
