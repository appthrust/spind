package list

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "list [name]",
		Short: "List VMs.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.MaximumNArgs(1)(nil, args); err != nil {
				return err
			}
			if len(args) == 1 {
				options.Name = args[0]
			}
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().BoolVar(&options.JSON, "json", false, "Write JSON output.")
	return command
}
