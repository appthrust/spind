package doctor

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check host requirements.",
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Ctx, runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().BoolVar(&options.JSON, "json", false, "Print JSON output.")
	return command
}
