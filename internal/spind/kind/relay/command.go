package relay

import (
	"errors"

	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
)

func New(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:    "kubernetes-relay <vm-name>",
		Short:  "Run an internal Kubernetes API relay.",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			if options.ListenPort == 0 {
				return errors.New("--listen-port is required")
			}
			if options.TargetPort == 0 {
				return errors.New("--target-port is required")
			}
			if options.GuestPort == 0 {
				return errors.New("--guest-port is required")
			}
			options.VMName = args[0]
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				return Run(runtime.Ctx, *options, runtime.Stderr)
			})
		},
	}
	command.Flags().IntVar(&options.ListenPort, "listen-port", 0, "Host loopback listen port.")
	command.Flags().IntVar(&options.TargetPort, "target-port", 0, "Guest Kubernetes API target port.")
	command.Flags().StringVar(&options.GuestIP, "guest-ip", "", "Guest IP address for direct TCP forwarding.")
	command.Flags().StringVar(&options.TCPForward, "tcp-forward", "", "Host TCP forward socket path.")
	command.Flags().StringVar(&options.Vsock, "vsock", "", "Cloud Hypervisor vsock socket path.")
	command.Flags().Uint32Var(&options.GuestPort, "guest-port", 0, "Guest TCP forward vsock port.")
	command.Flags().StringVar(&options.SSHSocket, "ssh-socket", "", "Host SSH relay socket path.")
	command.Flags().StringVar(&options.SSHKey, "ssh-key", "", "Host SSH relay private key path.")
	command.Flags().StringVar(&options.SSHUser, "ssh-user", "", "Host SSH relay user.")
	return command
}
