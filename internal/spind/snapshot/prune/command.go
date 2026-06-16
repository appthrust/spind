package prune

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "prune",
		Short: "Prune saved state snapshots.",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().BoolVar(&options.All, "all", false, "Prune all snapshots.")
	command.Flags().StringVar(&options.OlderThan, "older-than", "", "Only prune snapshots older than this duration.")
	command.Flags().StringVar(&options.Backend, "backend", "", "Only prune snapshots for this backend.")
	command.Flags().BoolVar(&options.DryRun, "dry-run", false, "List snapshots that would be deleted without deleting them.")
	return command
}
