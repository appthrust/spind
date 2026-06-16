package create

import (
	"errors"

	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "create <snapshot-name>",
		Short: "Create a saved state snapshot.",
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			if options.VM == "" {
				return errors.New("--vm is required")
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
	command.Flags().StringVar(&options.VM, "vm", "", "Source VM name.")
	command.Flags().BoolVar(&options.Kind, "kind", false, "Create a kind-ready snapshot.")
	command.Flags().StringVar(&options.Kubeconfig, "kubeconfig", "", "Host kubeconfig path for kind-ready snapshot. Defaults to KUBECONFIG or ~/.kube/config.")
	command.Flags().StringVar(&options.Context, "context", "", "Kubeconfig context for kind-ready snapshot. Defaults to current-context.")
	return command
}
