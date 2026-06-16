package create

import (
	"errors"

	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a VM from an image or snapshot.",
		Args: func(command *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(command, args); err != nil {
				return err
			}
			options.Name = args[0]
			options.CPUSet = command.Flags().Changed("cpu")
			options.MemorySet = command.Flags().Changed("memory")
			return validateSource(*options)
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Ctx, runtime.Config, *options, runtime.Stdout, runtime.Stderr)
			})
		},
	}
	command.Flags().StringVar(&options.Image, "image", "", "Image name.")
	command.Flags().StringVar(&options.Snapshot, "snapshot", "", "Snapshot name.")
	command.Flags().IntVar(&options.CPUCount, "cpu", 0, "CPU count.")
	command.Flags().StringVar(&options.Memory, "memory", "", "Memory size.")
	return command
}

func validateSource(options Options) error {
	if options.CPUSet && options.CPUCount <= 0 {
		return errors.New("--cpu must be positive")
	}
	if options.MemorySet {
		if _, err := parseMemoryMiB(options.Memory); err != nil {
			return err
		}
	}
	switch {
	case options.Image != "" && options.Snapshot != "":
		return errors.New("--image and --snapshot cannot be combined")
	case options.Image == "" && options.Snapshot == "":
		return errors.New("one of --image or --snapshot is required")
	case options.Snapshot != "" && options.CPUSet:
		return errors.New("--cpu cannot be combined with --snapshot")
	case options.Snapshot != "" && options.MemorySet:
		return errors.New("--memory cannot be combined with --snapshot")
	default:
		return nil
	}
}
