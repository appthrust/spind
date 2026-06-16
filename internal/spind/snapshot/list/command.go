package list

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "list",
		Short: "List saved state snapshots.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().BoolVar(&options.Strict, "strict", false, "Fail if any listed snapshot is unhealthy.")
	command.Flags().BoolVar(&options.JSON, "json", false, "Write JSON output.")
	return command
}
