package prune

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	if err := ValidateSelection(options); err != nil {
		fmt.Fprintf(stderr, "spind snapshot prune: %v\n", err)
		return 2
	}
	var olderThan time.Duration
	if options.OlderThan != "" {
		var err error
		olderThan, err = time.ParseDuration(options.OlderThan)
		if err != nil {
			fmt.Fprintf(stderr, "spind snapshot prune: invalid --older-than: %v\n", err)
			return 2
		}
	}
	targets, total, err := snapshotStore(cfg).Prune(olderThan, options.Backend, options.DryRun)
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if len(targets) == 0 {
		if options.DryRun {
			fmt.Fprintln(stdout, "no snapshots would be deleted")
		} else {
			fmt.Fprintln(stdout, "no snapshots deleted")
		}
		return 0
	}
	if options.DryRun {
		fmt.Fprintln(stdout, "snapshots that would be deleted:")
	} else {
		fmt.Fprintln(stdout, "deleted snapshots:")
	}
	output.PrintSnapshotPruneList(stdout, targets)
	if options.DryRun {
		fmt.Fprintf(stdout, "total planned size: %s\n", output.FormatSize(total))
	} else {
		fmt.Fprintf(stdout, "total deleted size: %s\n", output.FormatSize(total))
	}
	return 0
}

func ValidateSelection(options Options) error {
	hasFilter := options.OlderThan != "" || options.Backend != ""
	if options.All && hasFilter {
		return errors.New("--all cannot be combined with --older-than or --backend")
	}
	if !options.All && !hasFilter {
		return errors.New("refusing to prune snapshots without --all or a filter")
	}
	return nil
}

func snapshotStore(cfg config.Config) spindsnapshot.Store {
	return spindsnapshot.Store{SnapshotStore: cfg.SnapshotStore}
}
