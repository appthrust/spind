package merge

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "merge <name>",
		Short: "Merge a VM kubeconfig into a global kubeconfig.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			options.Name = args[0]
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().StringVar(&options.Kubeconfig, "kubeconfig", "", "Target kubeconfig path.")
	command.Flags().BoolVar(&options.Replace, "replace", false, "Replace existing spind entries for this VM.")
	command.Flags().BoolVar(&options.SetCurrentContext, "set-current-context", false, "Set current-context to this VM context.")
	return command
}
