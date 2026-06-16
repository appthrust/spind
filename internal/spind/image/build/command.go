package build

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "build <name>",
		Short: "Build a base image.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			options.Name = args[0]
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Ctx, runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().StringVar(&options.Config, "config", "", "Image build config source.")
	command.Flags().BoolVar(&options.Force, "force", false, "Replace an existing image.")
	command.Flags().StringVar(&options.DataSize, "data-size", "", "Writable data disk size.")
	return command
}
