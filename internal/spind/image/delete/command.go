package delete

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete base images.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.MinimumNArgs(1)(nil, args); err != nil {
				return err
			}
			options.Names = args
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().BoolVar(&options.Force, "force", false, "Delete images even if VM metadata references them.")
	return command
}
