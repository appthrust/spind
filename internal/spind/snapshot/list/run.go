package list

import (
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	snapshots, err := snapshotStore(cfg).List()
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	output.PrintSnapshotWarnings(stderr, snapshots)
	if options.JSON {
		result := make([]output.SnapshotInfo, 0, len(snapshots))
		for _, info := range snapshots {
			result = append(result, output.NewSnapshotInfo(info))
		}
		if code := output.WriteJSON(stdout, result, stderr); code != 0 {
			return code
		}
		if options.Strict && output.SnapshotsHaveProblems(snapshots) {
			return 1
		}
		return 0
	}
	if len(snapshots) == 0 {
		fmt.Fprintln(stdout, "no snapshots")
		return 0
	}
	output.PrintSnapshotList(stdout, snapshots)
	if options.Strict && output.SnapshotsHaveProblems(snapshots) {
		return 1
	}
	return 0
}

func snapshotStore(cfg config.Config) spindsnapshot.Store {
	return spindsnapshot.Store{SnapshotStore: cfg.SnapshotStore}
}
