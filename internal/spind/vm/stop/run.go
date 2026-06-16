package stop

import (
	"context"
	"fmt"
	"io"

	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	manager := vmstart.NewManagerFromConfig(cfg)
	stopped, err := manager.Stop(ctx, options.Name)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if stopped {
		fmt.Fprintf(stdout, "stopped VM %q\n", options.Name)
	} else {
		fmt.Fprintf(stdout, "VM %q is already stopped\n", options.Name)
	}
	return 0
}
