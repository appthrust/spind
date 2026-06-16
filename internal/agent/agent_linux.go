//go:build linux

package agent

import (
	"context"
	"fmt"

	"github.com/suin/spind/internal/agent/dockerproxy"
	"github.com/suin/spind/internal/agent/mounthelper"
	"github.com/suin/spind/internal/agent/sshproxy"
	"github.com/suin/spind/internal/agent/tcpforward"
)

const (
	CommandSSHProxy        = "ssh-proxy"
	CommandDockerProxy     = "docker-proxy"
	CommandTCPForwardProxy = "tcp-forward-proxy"
	CommandMountHelper     = "mount-helper"

	Usage = "usage: spind-guest-agent <ssh-proxy|docker-proxy|tcp-forward-proxy|mount-helper>"
)

func Run(ctx context.Context, command string) error {
	switch command {
	case CommandSSHProxy:
		return sshproxy.Run(ctx)
	case CommandDockerProxy:
		return dockerproxy.Run(ctx)
	case CommandTCPForwardProxy:
		return tcpforward.Run(ctx)
	case CommandMountHelper:
		return mounthelper.Run(ctx)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}
