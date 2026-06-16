package exec

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
	"github.com/suin/spind/internal/spind/vm/status"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	manager := vmstart.NewManagerFromConfig(cfg)
	vmName, guestCommand, err := ParseCommand(options)
	if err != nil {
		fmt.Fprintf(stderr, "spind vm exec: %v\n", err)
		return 2
	}
	exitCode, err := manager.Exec(ctx, vmName, guestCommand, stdin, stdout, stderr)
	if err != nil {
		code := cliruntime.ExitForError(stderr, err)
		if info, infoErr := status.Info(cfg, vmName); infoErr == nil {
			output.PrintVMLogHint(stderr, info)
		}
		return code
	}
	return exitCode
}

func ParseCommand(options Options) (string, []string, error) {
	if options.VMName == "" {
		return "", nil, errors.New("requires VM name")
	}
	if len(options.Command) < 2 {
		return "", nil, errors.New("requires <vm-name> -- <command> [args...]")
	}
	if options.Command[0] != "--" {
		return "", nil, errors.New("requires -- before command")
	}
	guestCommand := options.Command[1:]
	if len(guestCommand) == 0 {
		return "", nil, errors.New("requires command")
	}
	return options.VMName, guestCommand, nil
}
