package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spinddocker "github.com/suin/spind/internal/spind/docker"
	"github.com/suin/spind/internal/spind/doctor"
	imagebuild "github.com/suin/spind/internal/spind/image/build"
	imagedelete "github.com/suin/spind/internal/spind/image/delete"
	imagelist "github.com/suin/spind/internal/spind/image/list"
	kindrelay "github.com/suin/spind/internal/spind/kind/relay"
	kubeconfigmerge "github.com/suin/spind/internal/spind/kubeconfig/merge"
	kubeconfigunmerge "github.com/suin/spind/internal/spind/kubeconfig/unmerge"
	snapshotcreate "github.com/suin/spind/internal/spind/snapshot/create"
	snapshotdelete "github.com/suin/spind/internal/spind/snapshot/delete"
	snapshotinspect "github.com/suin/spind/internal/spind/snapshot/inspect"
	snapshotlist "github.com/suin/spind/internal/spind/snapshot/list"
	snapshotprune "github.com/suin/spind/internal/spind/snapshot/prune"
	"github.com/suin/spind/internal/spind/up"
	"github.com/suin/spind/internal/spind/vm"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
)

type Options struct {
	Doctor     doctor.Options
	VM         vm.Options
	Up         up.Options
	Image      ImageOptions
	Snapshot   SnapshotOptions
	Kubeconfig KubeconfigOptions
	Relay      RelayOptions
}

type ImageOptions struct {
	Build  imagebuild.Options
	List   imagelist.Options
	Delete imagedelete.Options
}

type SnapshotOptions struct {
	Create  snapshotcreate.Options
	List    snapshotlist.Options
	Inspect snapshotinspect.Options
	Delete  snapshotdelete.Options
	Prune   snapshotprune.Options
}

type KubeconfigOptions struct {
	Merge   kubeconfigmerge.Options
	Unmerge kubeconfigunmerge.Options
}

type RelayOptions struct {
	Docker     DockerRelayOptions
	DockerPort DockerPortRelayOptions
	Kubernetes kindrelay.Options
}

type DockerRelayOptions struct {
	VMName   string
	Endpoint string
	Vsock    string
	Port     uint32
}

type DockerPortRelayOptions struct {
	VMName         string
	DockerEndpoint string
	GuestIP        string
	TCPForward     string
	Vsock          string
	GuestPort      uint32
	SSHSocket      string
	SSHKey         string
	SSHUser        string
}

type Parsed struct {
	command *cobra.Command
}

func (c *Parsed) Command() string {
	if c == nil || c.command == nil {
		return ""
	}
	path := strings.TrimPrefix(c.command.CommandPath(), c.command.Root().Name()+" ")
	useFields := strings.Fields(c.command.Use)
	if len(useFields) <= 1 {
		return path
	}
	return path + " " + strings.Join(useFields[1:], " ")
}

func Parse(args []string, stdout io.Writer, stderr io.Writer) (options Options, ctx *Parsed, exitCode int, handled bool) {
	runtime := cliruntime.New(context.Background(), config.Config{}, strings.NewReader(""), stdout, stderr, false)
	root := NewRoot(&options, runtime)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var exit cliruntime.ExitError
		if errors.As(err, &exit) {
			return options, nil, exit.Code, true
		}
		fmt.Fprintf(stderr, "spind: %v\n", err)
		return options, nil, 2, true
	}
	if runtime.SelectedCommand == nil {
		return options, nil, 0, true
	}
	return options, &Parsed{command: runtime.SelectedCommand}, 0, false
}

func NewRoot(options *Options, runtime *cliruntime.Runtime) *cobra.Command {
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:           "spind",
		Short:         "Manage local spind VMs.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(runtime.Stdout)
	root.SetErr(runtime.Stderr)
	root.AddCommand(
		doctor.New(&options.Doctor, runtime),
		up.New(&options.Up, runtime),
		vm.New(&options.VM, runtime),
		newImageCommand(&options.Image, runtime),
		newSnapshotCommand(&options.Snapshot, runtime),
		newKubeconfigCommand(&options.Kubeconfig, runtime),
		newDockerRelayCommand(&options.Relay.Docker, runtime),
		newDockerPortRelayCommand(&options.Relay.DockerPort, runtime),
		kindrelay.New(&options.Relay.Kubernetes, runtime),
		newPrepareVZRunnerCommand(runtime),
	)
	return root
}

func newPrepareVZRunnerCommand(runtime *cliruntime.Runtime) *cobra.Command {
	return &cobra.Command{
		Use:    "prepare-vz-runner",
		Short:  "Prepare the managed Virtualization.framework runner.",
		Hidden: true,
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				manager := vmstart.NewManagerFromConfig(runtime.Config)
				path, err := manager.PrepareManagedVirtualizationRunner(runtime.Ctx)
				if err != nil {
					fmt.Fprintf(runtime.Stderr, "spind prepare-vz-runner: %v\n", err)
					return 1
				}
				fmt.Fprintf(runtime.Stdout, "%s\n", path)
				return 0
			})
		},
	}
}

func newImageCommand(options *ImageOptions, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "image",
		Short: "Manage base images.",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(imagebuild.New(&options.Build, runtime), imagelist.New(&options.List, runtime), imagedelete.New(&options.Delete, runtime))
	return command
}

func newSnapshotCommand(options *SnapshotOptions, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage saved state snapshots.",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(
		snapshotcreate.New(&options.Create, runtime),
		snapshotlist.New(&options.List, runtime),
		snapshotinspect.New(&options.Inspect, runtime),
		snapshotdelete.New(&options.Delete, runtime),
		snapshotprune.New(&options.Prune, runtime),
	)
	return command
}

func newKubeconfigCommand(options *KubeconfigOptions, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Manage explicit kubeconfig merge state.",
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	command.AddCommand(kubeconfigmerge.New(&options.Merge, runtime), kubeconfigunmerge.New(&options.Unmerge, runtime))
	return command
}

func newDockerRelayCommand(options *DockerRelayOptions, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:    "docker-relay <vm-name>",
		Short:  "Run an internal Docker host relay.",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			if options.Endpoint == "" {
				return errors.New("--endpoint is required")
			}
			if options.Vsock == "" {
				return errors.New("--vsock is required")
			}
			if options.Port == 0 {
				return errors.New("--port is required")
			}
			options.VMName = args[0]
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				if err := spinddocker.RunRelay(runtime.Ctx, options.Endpoint, options.Vsock, options.Port); err != nil {
					fmt.Fprintf(runtime.Stderr, "spind docker-relay: %v\n", err)
					return 1
				}
				return 0
			})
		},
	}
	command.Flags().StringVar(&options.Endpoint, "endpoint", "", "Host Docker endpoint socket path.")
	command.Flags().StringVar(&options.Vsock, "vsock", "", "Cloud Hypervisor vsock socket path.")
	command.Flags().Uint32Var(&options.Port, "port", 0, "Guest Docker vsock port.")
	return command
}

func newDockerPortRelayCommand(options *DockerPortRelayOptions, runtime *cliruntime.Runtime) *cobra.Command {
	command := &cobra.Command{
		Use:    "docker-port-relay <vm-name>",
		Short:  "Run an internal Docker published port relay.",
		Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(1)(nil, args); err != nil {
				return err
			}
			if options.DockerEndpoint == "" {
				return errors.New("--docker-endpoint is required")
			}
			if options.GuestPort == 0 {
				return errors.New("--guest-port is required")
			}
			options.VMName = args[0]
			return nil
		},
		RunE: func(command *cobra.Command, _ []string) error {
			return runtime.Run(command, func() int {
				if err := spinddocker.RunPortRelay(runtime.Ctx, options.DockerEndpoint, options.GuestIP, options.TCPForward, options.Vsock, options.GuestPort, options.SSHSocket, options.SSHKey, options.SSHUser); err != nil {
					fmt.Fprintf(runtime.Stderr, "spind docker-port-relay: %v\n", err)
					return 1
				}
				return 0
			})
		},
	}
	command.Flags().StringVar(&options.DockerEndpoint, "docker-endpoint", "", "Host Docker endpoint socket path.")
	command.Flags().StringVar(&options.GuestIP, "guest-ip", "", "Guest IP address for direct TCP forwarding.")
	command.Flags().StringVar(&options.TCPForward, "tcp-forward", "", "Host TCP forward socket path.")
	command.Flags().StringVar(&options.Vsock, "vsock", "", "Cloud Hypervisor vsock socket path.")
	command.Flags().Uint32Var(&options.GuestPort, "guest-port", 0, "Guest TCP forward vsock port.")
	command.Flags().StringVar(&options.SSHSocket, "ssh-socket", "", "Host SSH relay socket path.")
	command.Flags().StringVar(&options.SSHKey, "ssh-key", "", "Host SSH relay private key path.")
	command.Flags().StringVar(&options.SSHUser, "ssh-user", "", "Host SSH relay user.")
	return command
}
