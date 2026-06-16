//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "spind-guest-agent is supported on Linux guests only")
	os.Exit(1)
}
