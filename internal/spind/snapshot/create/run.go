package create

import (
	"context"
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
	"github.com/suin/spind/internal/spind/vm/status"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	manager := vmstart.NewManagerFromConfig(cfg)
	createOptions := spindsnapshot.CreateOptions{
		K8s:            options.K8s,
		KubeconfigPath: options.Kubeconfig,
		Context:        options.Context,
	}
	if err := manager.SnapshotCreateWithOptions(ctx, options.Name, options.VM, createOptions); err != nil {
		code := cliruntime.ExitForError(stderr, err)
		if info, infoErr := status.Info(cfg, options.VM); infoErr == nil {
			output.PrintVMLogHint(stderr, info)
		}
		return code
	}
	fmt.Fprintf(stdout, "created snapshot %q from VM %q\n", options.Name, options.VM)
	return 0
}
