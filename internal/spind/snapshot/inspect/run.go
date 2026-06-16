package inspect

import (
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	info, err := snapshotStore(cfg).Info(options.Name)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if info.Health != "ok" {
		fmt.Fprintf(stderr, "warning: snapshot %q health is %s: %s\n", info.Name, info.Health, output.DisplayValue(info.HealthMessage))
	}
	if options.JSON {
		return output.WriteJSON(stdout, output.NewSnapshotInfo(info), stderr)
	}
	output.PrintSnapshotInfo(stdout, info)
	return 0
}

func snapshotStore(cfg config.Config) spindsnapshot.Store {
	return spindsnapshot.Store{SnapshotStore: cfg.SnapshotStore}
}
