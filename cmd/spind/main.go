package main

import (
	"os"

	"github.com/suin/spind/internal/spind/cli"
)

func main() {
	os.Exit(cli.RunWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
