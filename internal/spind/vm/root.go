package vm

import (
	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/vm/create"
	vmdelete "github.com/suin/spind/internal/spind/vm/delete"
	vmexec "github.com/suin/spind/internal/spind/vm/exec"
	vmlist "github.com/suin/spind/internal/spind/vm/list"
	"github.com/suin/spind/internal/spind/vm/start"
	"github.com/suin/spind/internal/spind/vm/stop"
)

type Options struct {
	Create create.Options
	Start  start.Options
	Stop   stop.Options
	Delete vmdelete.Options
	Exec   vmexec.Options
	List   vmlist.Options
}

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "vm",
		Short: "Manage VMs.",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		create.New(&options.Create, runtime),
		start.New(&options.Start, runtime),
		stop.New(&options.Stop, runtime),
		vmdelete.New(&options.Delete, runtime),
		vmexec.New(&options.Exec, runtime),
		vmlist.New(&options.List, runtime),
	)
	return command
}
