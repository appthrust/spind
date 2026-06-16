package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/store"
	"github.com/suin/spind/internal/spind/vmstore"
)

type Runtime struct {
	Ctx             context.Context
	Config          config.Config
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	Execute         bool
	ExitCode        int
	SelectedCommand *cobra.Command
}

func New(ctx context.Context, cfg config.Config, stdin io.Reader, stdout io.Writer, stderr io.Writer, execute bool) *Runtime {
	return &Runtime{
		Ctx:     ctx,
		Config:  cfg,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Execute: execute,
	}
}

type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

func (r *Runtime) Run(command *cobra.Command, action func() int) error {
	r.SelectedCommand = command
	if !r.Execute {
		return nil
	}
	code := action()
	r.ExitCode = code
	if code != 0 {
		return ExitError{Code: code}
	}
	return nil
}

func ExitForError(stderr io.Writer, err error) int {
	switch {
	case errors.Is(err, store.ErrInvalidName):
		fmt.Fprintf(stderr, "spind: invalid name: %v\n", err)
		return 2
	case errors.Is(err, vmstore.ErrNotRunning):
		fmt.Fprintf(stderr, "spind: not running: %v\n", err)
		return 1
	case errors.Is(err, store.ErrNotFound):
		fmt.Fprintf(stderr, "spind: not found: %v\n", err)
		return 1
	case errors.Is(err, store.ErrAlreadyExists):
		fmt.Fprintf(stderr, "spind: already exists: %v\n", err)
		return 1
	default:
		fmt.Fprintf(stderr, "spind: %v\n", err)
		return 1
	}
}
