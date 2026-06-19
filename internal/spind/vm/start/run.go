package start

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/vm/status"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	manager := NewManagerFromConfig(cfg)
	startedAt := time.Now()
	started, err := manager.Start(ctx, options.Name)
	if err != nil {
		code := cliruntime.ExitForError(stderr, err)
		if info, infoErr := status.Info(cfg, options.Name); infoErr == nil {
			output.PrintVMLogHint(stderr, info)
		}
		return code
	}
	info, infoErr := manager.VMStatus(options.Name)
	if started {
		if infoErr == nil && info.FromSnapshot {
			duration := info.LastStartDuration
			if duration == 0 {
				duration = time.Since(startedAt)
			}
			fmt.Fprintf(stdout, "restored VM %q in %s\n", options.Name, output.FormatDuration(duration))
		} else {
			fmt.Fprintf(stdout, "started VM %q\n", options.Name)
		}
	} else {
		fmt.Fprintf(stdout, "VM %q is already running\n", options.Name)
	}
	if infoErr == nil {
		output.PrintHostShareStartLine(stdout, stderr, info)
		output.PrintDockerStartLine(stdout, stderr, info)
		output.PrintKubernetesStartLine(stdout, stderr, info)
		output.PrintRegistryStartLine(stdout, stderr, info)
	}
	return 0
}
