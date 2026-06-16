package create

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
	if options.Snapshot != "" {
		if options.CPUSet || options.MemorySet {
			return cliruntime.ExitForError(stderr, validateSource(options))
		}
		if err := manager.CreateFromSnapshot(ctx, options.Name, options.Snapshot, ""); err != nil {
			return cliruntime.ExitForError(stderr, err)
		}
		fmt.Fprintf(stdout, "created VM %q from snapshot %q\n", options.Name, options.Snapshot)
		return 0
	}
	createOptions, err := managerCreateOptions(options)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if err := manager.CreateFromImageWithOptions(ctx, options.Name, options.Image, "", createOptions); err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	fmt.Fprintf(stdout, "created VM %q from image %q\n", options.Name, options.Image)
	return 0
}

func managerCreateOptions(options Options) (vmstart.CreateOptions, error) {
	createOptions := vmstart.CreateOptions{}
	if options.CPUSet {
		if options.CPUCount <= 0 {
			return createOptions, fmt.Errorf("--cpu must be positive")
		}
		createOptions.CPUCount = options.CPUCount
	}
	if options.MemorySet {
		memoryMiB, err := parseMemoryMiB(options.Memory)
		if err != nil {
			return createOptions, err
		}
		createOptions.MemoryMiB = memoryMiB
	}
	return createOptions, nil
}
