package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
)

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	return RunWithIO(args, strings.NewReader(""), stdout, stderr)
}

func RunWithIO(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	cfg, err := config.NewFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "spind: %v\n", err)
		return 1
	}
	options := Options{}
	runtime := cliruntime.New(context.Background(), cfg, stdin, stdout, stderr, true)
	root := NewRoot(&options, runtime)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var exit cliruntime.ExitError
		if errors.As(err, &exit) {
			return exit.Code
		}
		fmt.Fprintf(stderr, "spind: %v\n", err)
		return 2
	}
	return runtime.ExitCode
}
