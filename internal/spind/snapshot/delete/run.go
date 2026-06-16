package delete

import (
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	size, err := snapshotStore(cfg).DeleteWithSize(options.Name)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	fmt.Fprintf(stdout, "deleted snapshot %q (%s)\n", options.Name, output.FormatSize(size))
	return 0
}

func snapshotStore(cfg config.Config) spindsnapshot.Store {
	return spindsnapshot.Store{SnapshotStore: cfg.SnapshotStore}
}
