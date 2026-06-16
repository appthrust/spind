package exec

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:                "exec <vm-name> -- <command> [args...]",
		Short:              "Execute a command in a running VM.",
		DisableFlagParsing: true,
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(nil, args); err != nil {
				return err
			}
			options.VMName = args[0]
			options.Command = args[1:]
			return nil
		},
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
				return command.Help()
			}
			return runtime.Run(command, func() int {
				return Run(runtime.Ctx, runtime.Config, *options, runtime.Stdin, runtime.Stdout, runtime.Stderr)
			})
		},
	}
}
