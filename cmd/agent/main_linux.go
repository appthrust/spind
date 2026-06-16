//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/suin/spind/internal/agent"
)

func main() {
	command, ok := guestAgentCommand()
	if !ok {
		fmt.Fprintln(os.Stderr, agent.Usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, command); err != nil {
		fmt.Fprintf(os.Stderr, "spind-guest-agent %s: %v\n", command, err)
		os.Exit(1)
	}
}

func guestAgentCommand() (string, bool) {
	if len(os.Args) >= 2 {
		return os.Args[1], true
	}
	switch filepath.Base(os.Args[0]) {
	case "spind-vsock-ssh-proxy":
		return agent.CommandSSHProxy, true
	case "spind-vsock-docker-proxy":
		return agent.CommandDockerProxy, true
	case "spind-vsock-tcp-forward-proxy":
		return agent.CommandTCPForwardProxy, true
	case "spind-vsock-mount-helper":
		return agent.CommandMountHelper, true
	default:
		return "", false
	}
}
