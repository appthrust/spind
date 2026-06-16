package list

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/vm/status"
	"github.com/suin/spind/internal/spind/vmstore"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	if options.Name != "" {
		info, err := status.Info(cfg, options.Name)
		if err != nil {
			return cliruntime.ExitForError(stderr, err)
		}
		if options.JSON {
			return output.WriteJSON(stdout, output.NewVMInfo(info), stderr)
		}
		output.PrintVMInfo(stdout, info)
		return 0
	}
	vms, err := listVMs(cfg)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if options.JSON {
		result := make([]output.VMInfo, 0, len(vms))
		for _, info := range vms {
			result = append(result, output.NewVMInfo(info))
		}
		return output.WriteJSON(stdout, result, stderr)
	}
	if len(vms) == 0 {
		fmt.Fprintln(stdout, "no VMs")
		return 0
	}
	output.PrintVMList(stdout, vms)
	return 0
}

func listVMs(cfg config.Config) ([]vmstore.Info, error) {
	entries, err := os.ReadDir(cfg.VMStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read VM store: %w", err)
	}
	vms := make([]vmstore.Info, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := status.Info(cfg, entry.Name())
		if err != nil {
			return nil, err
		}
		vms = append(vms, info)
	}
	sort.Slice(vms, func(i int, j int) bool {
		return vms[i].Name < vms[j].Name
	})
	return vms, nil
}
