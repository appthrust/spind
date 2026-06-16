//go:build !linux

package agent

import (
	"context"
	"errors"
)

const (
	CommandSSHProxy        = "ssh-proxy"
	CommandDockerProxy     = "docker-proxy"
	CommandTCPForwardProxy = "tcp-forward-proxy"
	CommandMountHelper     = "mount-helper"

	Usage = "usage: spind-guest-agent <ssh-proxy|docker-proxy|tcp-forward-proxy|mount-helper>"
)

func Run(context.Context, string) error {
	return errors.New("spind-guest-agent is supported on Linux guests only")
}
