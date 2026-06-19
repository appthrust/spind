package start

import (
	"bytes"
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/backend/vz"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/diskimage"
	spinddocker "github.com/suin/spind/internal/spind/docker"
	"github.com/suin/spind/internal/spind/filecopy"
	spindimage "github.com/suin/spind/internal/spind/image"
	spindkind "github.com/suin/spind/internal/spind/kind"
	"github.com/suin/spind/internal/spind/requirements"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	"github.com/suin/spind/internal/spind/store"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
	"golang.org/x/crypto/ssh"
)

const (
	BackendVirtualizationFramework = spindvm.BackendVirtualizationFramework
	BackendCloudHypervisor         = spindvm.BackendCloudHypervisor

	cloudHypervisorNetworkBackendPasst = "passt"
	cloudHypervisorNetworkBackendTap   = "tap"

	defaultCPUCount             = 2
	defaultMemoryMiB            = 1024
	defaultDockerMemoryMiB      = 2048
	defaultExecPort             = 10222
	defaultDockerPort           = 10240
	defaultDockerTCPForwardPort = 10241
	defaultHostShareMountPort   = 10242
	defaultHostShareTag         = "spind-cwd"
	defaultExecUser             = "spind"
	defaultGuestCID             = 3

	imageKernelName    = "kernel"
	imageInitramfsName = "initramfs"
	imageDiskName      = "disk.img"
	imageMetadataName  = "metadata.json"

	vmMetadataName            = spindvm.MetadataName
	vmStateName               = spindvm.StateName
	vmConfigName              = "runner-config.json"
	vmLogName                 = "runner.log"
	cloudHypervisorConfigName = "cloud-hypervisor-config.json"
	vmSSHPrivateKeyName       = "ssh_key"
	vmSSHPublicKeyName        = "ssh_key.pub"

	snapshotMetadataName                   = "metadata.json"
	snapshotVirtualizationFrameworkDirName = "vf"
	snapshotCloudHypervisorDirName         = "cloud-hypervisor"
	snapshotKindDirName                    = spindkind.DirName
	snapshotKindMetadataName               = spindkind.MetadataName
	snapshotKindKubeconfigTemplateName     = spindkind.KubeconfigTemplateName
	vmCloudHypervisorSnapshotDirName       = "cloud-hypervisor-snapshot"
	cloudHypervisorSnapshotConfigName      = "config.json"
	cloudHypervisorMemoryRangesName        = "memory-ranges"
	cloudHypervisorStateName               = "state.json"
)

const (
	capabilitySupported     = "supported"
	capabilityUnsupported   = "unsupported"
	capabilityReady         = "ready"
	capabilityUnavailable   = "unavailable"
	capabilityNotApplicable = "not-applicable"
)

type Manager struct {
	Home                  string
	ImageStore            string
	TemplateStore         string
	BuiltinTemplateSource string
	VMStore               string
	SnapshotStore         string
	RunnerPath            string
	CloudHypervisorPath   string

	skipVMStartRequirements bool
}

type stopMode int

const (
	stopModeGraceful stopMode = iota
	stopModeDelete
)

type CreateOptions struct {
	CPUCount  int
	MemoryMiB int
}

func NewManagerFromConfig(cfg config.Config) *Manager {
	return &Manager{
		Home:                  cfg.Home,
		ImageStore:            cfg.ImageStore,
		TemplateStore:         cfg.TemplateStore,
		BuiltinTemplateSource: cfg.BuiltinTemplateSource,
		VMStore:               cfg.VMStore,
		SnapshotStore:         cfg.SnapshotStore,
		RunnerPath:            cfg.RunnerPath,
		CloudHypervisorPath:   cfg.CloudHypervisorPath,
	}
}

func NewManagerFromEnv() (*Manager, error) {
	home, err := defaultHome()
	if err != nil {
		return nil, err
	}
	if value := envFirst("SPIND_HOME", "KIDO_HOME"); value != "" {
		home = value
	}

	imageStore := filepath.Join(home, "images")
	if value := envFirst("SPIND_IMAGE_STORE", "KIDO_IMAGE_STORE"); value != "" {
		imageStore = value
	}
	templateStore := filepath.Join(home, "templates")
	if value := envFirst("SPIND_TEMPLATE_STORE", "KIDO_TEMPLATE_STORE"); value != "" {
		templateStore = value
	}
	builtinTemplateSource := envFirst("SPIND_BUILTIN_TEMPLATE_SOURCE", "KIDO_BUILTIN_TEMPLATE_SOURCE")
	if builtinTemplateSource == "" {
		builtinTemplateSource = spindimage.FindDefaultTemplateSource()
	}

	vmStore := filepath.Join(home, "vms")
	if value := envFirst("SPIND_VM_STORE", "KIDO_VM_STORE"); value != "" {
		vmStore = value
	}
	snapshotStore := filepath.Join(home, "snapshots")
	if value := envFirst("SPIND_SNAPSHOT_STORE", "KIDO_SNAPSHOT_STORE"); value != "" {
		snapshotStore = value
	}

	runnerPath := envFirst("SPIND_VZ_RUNNER", "KIDO_VZ_RUNNER")
	cloudHypervisorPath := envFirst("SPIND_CLOUD_HYPERVISOR", "KIDO_CLOUD_HYPERVISOR")
	if cloudHypervisorPath == "" {
		cloudHypervisorPath = findExecutable("cloud-hypervisor")
	}

	return &Manager{
		Home:                  home,
		ImageStore:            imageStore,
		TemplateStore:         templateStore,
		BuiltinTemplateSource: builtinTemplateSource,
		VMStore:               vmStore,
		SnapshotStore:         snapshotStore,
		RunnerPath:            runnerPath,
		CloudHypervisorPath:   cloudHypervisorPath,
	}, nil
}

func (m *Manager) Create(ctx context.Context, name string, imageName string, backend string) error {
	return m.CreateFromImage(ctx, name, imageName, backend)
}

func (m *Manager) CreateFromImage(ctx context.Context, name string, imageName string, backend string) error {
	return m.CreateFromImageWithOptions(ctx, name, imageName, backend, CreateOptions{})
}

func (m *Manager) CreateFromImageWithOptions(ctx context.Context, name string, imageName string, backend string, options CreateOptions) error {
	if err := validateStoreName(name); err != nil {
		return err
	}
	if err := validateCreateOptions(options); err != nil {
		return err
	}
	if err := validateStoreName(imageName); err != nil {
		return fmt.Errorf("image %q: %w", imageName, err)
	}
	if backend == "" {
		var err error
		backend, err = defaultBackend()
		if err != nil {
			return err
		}
	}
	if err := validateBackend(backend); err != nil {
		return err
	}

	imageDir := filepath.Join(m.ImageStore, imageName)
	image, err := spindimage.ReadMetadata(imageDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("image %q: %w", imageName, ErrNotFound)
		}
		return fmt.Errorf("read image %q: %w", imageName, err)
	}
	if image.Name == "" {
		image.Name = imageName
	}

	vmDir := filepath.Join(m.VMStore, name)
	if err := os.MkdirAll(m.VMStore, 0o755); err != nil {
		return fmt.Errorf("create VM store: %w", err)
	}
	if err := os.Mkdir(vmDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("VM %q: %w", name, ErrAlreadyExists)
		}
		return fmt.Errorf("create VM directory: %w", err)
	}

	if err := spindimage.CopyRequiredFiles(imageDir, vmDir, image); err != nil {
		_ = os.RemoveAll(vmDir)
		return err
	}

	privateKey, publicKey, err := generateVMSSHKey()
	if err != nil {
		_ = os.RemoveAll(vmDir)
		return err
	}
	if err := os.WriteFile(filepath.Join(vmDir, vmSSHPrivateKeyName), privateKey, 0o600); err != nil {
		_ = os.RemoveAll(vmDir)
		return fmt.Errorf("write VM SSH private key: %w", err)
	}
	publicKeyPath := filepath.Join(vmDir, vmSSHPublicKeyName)
	if err := os.WriteFile(publicKeyPath, publicKey, 0o644); err != nil {
		_ = os.RemoveAll(vmDir)
		return fmt.Errorf("write VM SSH public key: %w", err)
	}
	if image.SSHAuthorizedKeyCommandLineParam == "" {
		if err := injectAuthorizedKey(filepath.Join(vmDir, imageDiskName), publicKeyPath, publicKey, image.ExecUser); err != nil {
			_ = os.RemoveAll(vmDir)
			return err
		}
	}

	metadata := spindvm.Metadata{
		Name:         name,
		Image:        imageName,
		ImageType:    image.ImageType,
		Backend:      backend,
		Architecture: image.Architecture,
		ExecUser:     image.ExecUser,
		CreatedAt:    time.Now().UTC(),
	}

	switch backend {
	case BackendVirtualizationFramework:
		config := runnerConfig(name, vmDir, image)
		applyCreateOptionsToResources(&config.CPUCount, &config.MemoryMiB, options)
		applyImageAuthorizedKeyCommandLine(&config.KernelCommandLine, image, publicKey)
		metadata.CPUCount = config.CPUCount
		metadata.MemoryMiB = config.MemoryMiB
		metadata.ExecPort = config.ExecPort
		if m.RunnerPath != "" {
			machineIdentifier, err := m.runnerMachineIdentifier(ctx)
			if err != nil {
				_ = os.RemoveAll(vmDir)
				return fmt.Errorf("generate Virtualization.framework machine identifier: %w", err)
			}
			config.MachineIdentifier = machineIdentifier
		}
		if err := writeJSON(filepath.Join(vmDir, vmConfigName), config, 0o644); err != nil {
			_ = os.RemoveAll(vmDir)
			return fmt.Errorf("write runner config: %w", err)
		}
	case BackendCloudHypervisor:
		config := cloudHypervisorConfig(name, vmDir, image)
		applyCreateOptionsToResources(&config.CPUCount, &config.MemoryMiB, options)
		applyImageAuthorizedKeyCommandLine(&config.KernelCommandLine, image, publicKey)
		metadata.CPUCount = config.CPUCount
		metadata.MemoryMiB = config.MemoryMiB
		metadata.ExecPort = config.ExecPort
		if err := writeJSON(filepath.Join(vmDir, cloudHypervisorConfigName), config, 0o644); err != nil {
			_ = os.RemoveAll(vmDir)
			return fmt.Errorf("write Cloud Hypervisor config: %w", err)
		}
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		_ = os.RemoveAll(vmDir)
		return fmt.Errorf("write VM metadata: %w", err)
	}

	if err := writeState(vmDir, spindvm.State{Status: "stopped", Backend: backend, UpdatedAt: time.Now().UTC()}); err != nil {
		_ = os.RemoveAll(vmDir)
		return err
	}

	if backend == BackendVirtualizationFramework && m.RunnerPath != "" {
		if err := m.runRunner(ctx, "validate", "--config", filepath.Join(vmDir, vmConfigName)); err != nil {
			_ = os.RemoveAll(vmDir)
			return fmt.Errorf("validate VM with Swift runner: %w", err)
		}
	}

	return nil
}

func validateCreateOptions(options CreateOptions) error {
	if options.CPUCount < 0 {
		return errors.New("CPU count must be positive")
	}
	if options.MemoryMiB < 0 {
		return errors.New("memory must be positive")
	}
	return nil
}

func applyCreateOptionsToResources(cpuCount *int, memoryMiB *int, options CreateOptions) {
	if options.CPUCount != 0 {
		*cpuCount = options.CPUCount
	}
	if options.MemoryMiB != 0 {
		*memoryMiB = options.MemoryMiB
	}
}

func (m *Manager) CreateFromSnapshot(ctx context.Context, name string, snapshotName string, backendOverride string) error {
	if err := validateStoreName(name); err != nil {
		return err
	}
	if err := validateStoreName(snapshotName); err != nil {
		return fmt.Errorf("snapshot %q: %w", snapshotName, err)
	}
	if backendOverride != "" {
		return errors.New("create --snapshot does not accept --backend")
	}
	snapshotDir := filepath.Join(m.SnapshotStore, snapshotName)
	var snapshot spindsnapshot.Metadata
	if err := readJSON(filepath.Join(snapshotDir, snapshotMetadataName), &snapshot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("snapshot %q: %w", snapshotName, ErrNotFound)
		}
		return fmt.Errorf("read snapshot metadata: %w", err)
	}
	if snapshot.ExecUser == "" {
		snapshot.ExecUser = defaultExecUser
	}
	if snapshot.ExecPort == 0 {
		snapshot.ExecPort = defaultExecPort
	}
	if snapshot.CPUCount == 0 {
		snapshot.CPUCount = defaultCPUCount
	}
	if snapshot.MemoryMiB == 0 {
		snapshot.MemoryMiB = defaultMemoryMiB
	}

	vmDir := filepath.Join(m.VMStore, name)
	if err := os.MkdirAll(m.VMStore, 0o755); err != nil {
		return fmt.Errorf("create VM store: %w", err)
	}
	if err := os.Mkdir(vmDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("VM %q: %w", name, ErrAlreadyExists)
		}
		return fmt.Errorf("create VM directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(vmDir)
		}
	}()

	if err := filecopy.Copy(filepath.Join(snapshotDir, vmSSHPrivateKeyName), filepath.Join(vmDir, vmSSHPrivateKeyName), 0o600); err != nil {
		return fmt.Errorf("copy snapshot SSH private key: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(snapshotDir, vmSSHPublicKeyName), filepath.Join(vmDir, vmSSHPublicKeyName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot SSH public key: %w", err)
	}

	metadata := spindvm.Metadata{
		Name:             name,
		Image:            snapshot.Image,
		ImageType:        snapshot.ImageType,
		Backend:          snapshot.Backend,
		Architecture:     snapshot.Architecture,
		ExecUser:         snapshot.ExecUser,
		CreatedAt:        time.Now().UTC(),
		FromSnapshot:     true,
		SourceSnapshot:   snapshotName,
		CPUCount:         snapshot.CPUCount,
		MemoryMiB:        snapshot.MemoryMiB,
		ExecPort:         snapshot.ExecPort,
		RestoreStatePath: snapshotRestoreStatePath(vmDir, snapshot.Backend),
		KindReady:        snapshot.KindReady,
		K8sDistribution:  snapshot.K8sDistribution,
	}
	if snapshot.KindReady {
		metadata.KubeconfigPath = filepath.Join(vmDir, "kubeconfig")
		if err := copyKindSnapshotArtifacts(snapshotDir, vmDir); err != nil {
			return err
		}
	}

	switch snapshot.Backend {
	case BackendCloudHypervisor:
		if err := m.createCloudHypervisorVMFromSnapshot(snapshotDir, name, vmDir); err != nil {
			return err
		}
	case BackendVirtualizationFramework:
		disks := snapshot.Disks
		if len(disks) == 0 {
			disks = []spindimage.DiskMetadata{{Name: imageDiskName}}
		}
		vfDir := filepath.Join(vmDir, snapshotVirtualizationFrameworkDirName)
		if err := os.MkdirAll(vfDir, 0o755); err != nil {
			return fmt.Errorf("create Virtualization.framework restore directory: %w", err)
		}
		if err := filecopy.Copy(filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, "state.vzvmsave"), filepath.Join(vfDir, "state.vzvmsave"), 0o644); err != nil {
			return fmt.Errorf("copy snapshot saved state: %w", err)
		}
		if err := filecopy.Copy(filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageKernelName), filepath.Join(vmDir, imageKernelName), 0o644); err != nil {
			return fmt.Errorf("copy snapshot kernel: %w", err)
		}
		if err := filecopy.Copy(filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageInitramfsName), filepath.Join(vmDir, imageInitramfsName), 0o644); err != nil {
			return fmt.Errorf("copy snapshot initramfs: %w", err)
		}
		for _, disk := range disks {
			if err := filecopy.Copy(filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, disk.Name), filepath.Join(vmDir, disk.Name), 0o644); err != nil {
				return fmt.Errorf("copy snapshot disk %q: %w", disk.Name, err)
			}
		}
		config := runnerConfig(name, vmDir, spindimage.Metadata{
			Name:              snapshot.Image,
			KernelCommandLine: snapshot.KernelCommandLine,
			Disks:             disks,
		})
		config.CPUCount = snapshot.CPUCount
		config.MemoryMiB = snapshot.MemoryMiB
		config.ExecPort = snapshot.ExecPort
		config.RestoreStatePath = metadata.RestoreStatePath
		if snapshot.MachineIdentifier != "" {
			config.MachineIdentifier = snapshot.MachineIdentifier
		}
		if snapshot.NetworkMAC != "" {
			config.NetworkMAC = snapshot.NetworkMAC
		}
		if err := writeJSON(filepath.Join(vmDir, vmConfigName), config, 0o644); err != nil {
			return fmt.Errorf("write runner config: %w", err)
		}
	default:
		return fmt.Errorf("snapshot %q has unsupported backend %q", snapshotName, snapshot.Backend)
	}

	if metadata.Architecture == "" {
		metadata.Architecture = runtime.GOARCH
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		return fmt.Errorf("write VM metadata: %w", err)
	}
	if err := writeState(vmDir, spindvm.State{Status: "stopped", Backend: metadata.Backend, UpdatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (m *Manager) Start(ctx context.Context, name string) (bool, error) {
	if err := validateStoreName(name); err != nil {
		return false, err
	}

	vmDir := filepath.Join(m.VMStore, name)
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("VM %q: %w", name, ErrNotFound)
		}
		return false, fmt.Errorf("read VM metadata: %w", err)
	}
	if metadata.ExecUser == "" {
		metadata.ExecUser = defaultExecUser
	}
	if metadata.Backend == "" {
		metadata.Backend = BackendVirtualizationFramework
	}
	hydrateVMMetadata(&metadata)
	if !m.skipVMStartRequirements {
		requirementsConfig := requirements.ConfigFromVMManager(m.Home, m.ImageStore, m.TemplateStore, m.BuiltinTemplateSource, m.VMStore, m.SnapshotStore, m.RunnerPath, m.CloudHypervisorPath)
		if err := requirements.CheckOrError(requirements.CheckVMStart(ctx, requirementsConfig, requirements.ForVMStart(metadata))); err != nil {
			return false, err
		}
	}

	state, err := readState(vmDir)
	if err != nil {
		return false, err
	}
	if state.Status == "running" && processAlive(state.PID) {
		return false, nil
	}

	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if metadata.FromSnapshot {
			return m.startVirtualizationFrameworkRestore(ctx, name, vmDir, metadata)
		}
		return m.startVirtualizationFramework(ctx, name, vmDir, metadata)
	case BackendCloudHypervisor:
		if metadata.FromSnapshot {
			return m.startCloudHypervisorRestore(ctx, name, vmDir, metadata)
		}
		return m.startCloudHypervisor(ctx, name, vmDir, metadata)
	default:
		return false, fmt.Errorf("VM %q has unsupported backend %q", name, metadata.Backend)
	}
}

func (m *Manager) ListVMs() ([]spindvm.Info, error) {
	entries, err := os.ReadDir(m.VMStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read VM store: %w", err)
	}
	vms := make([]spindvm.Info, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := m.VMStatus(entry.Name())
		if err != nil {
			return nil, err
		}
		vms = append(vms, info)
	}
	sortVMInfos(vms)
	return vms, nil
}

func (m *Manager) ListImages() ([]spindimage.Info, error) {
	return m.imageStore().List()
}

func (m *Manager) VMStatus(name string) (spindvm.Info, error) {
	if err := validateStoreName(name); err != nil {
		return spindvm.Info{}, err
	}
	vmDir := filepath.Join(m.VMStore, name)
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return spindvm.Info{}, fmt.Errorf("VM %q: %w", name, ErrNotFound)
		}
		return spindvm.Info{}, fmt.Errorf("read VM metadata: %w", err)
	}
	hydrateVMMetadata(&metadata)
	state, err := readState(vmDir)
	if err != nil {
		return spindvm.Info{}, err
	}
	status := state.Status
	execReady := state.ExecReady
	if status == "running" && (state.PID == 0 || !processAlive(state.PID)) {
		status = "stopped"
		execReady = false
	}
	restoreMode := "boot"
	if metadata.FromSnapshot {
		restoreMode = "saved-state"
	}
	serialLogPath, backendLogPath, eventLogPath := vmNamedLogPaths(vmDir, metadata.Backend, state)
	dockerPublishedPorts := state.DockerPublishedPorts
	if state.DockerAvailable && state.DockerSocketPath != "" {
		dockerPublishedPorts = spinddocker.PublishedPorts(context.Background(), state.DockerSocketPath)
	}
	return spindvm.Info{
		Name:                          name,
		Status:                        status,
		Backend:                       metadata.Backend,
		PID:                           state.PID,
		ExecReady:                     execReady,
		Image:                         metadata.Image,
		FromSnapshot:                  metadata.FromSnapshot,
		SourceSnapshot:                metadata.SourceSnapshot,
		RestoreMode:                   restoreMode,
		VMDir:                         vmDir,
		StatePath:                     filepath.Join(vmDir, vmStateName),
		ExecSocketPath:                state.ExecSocketPath,
		CloudHypervisorAPISocketPath:  state.CloudHypervisorAPISocketPath,
		CloudHypervisorVsockPath:      state.CloudHypervisorVsockSocketPath,
		DockerSocketPath:              state.DockerSocketPath,
		DockerEndpointURI:             state.DockerEndpointURI,
		DockerRelayPID:                state.DockerRelayPID,
		DockerGuestPort:               state.DockerGuestPort,
		DockerAPISupport:              dockerCapabilitySupport(metadata),
		DockerAPIStatus:               dockerCapabilityStatus(metadata, state.DockerAvailable && state.DockerAPIReady && state.DockerSocketPath != ""),
		DockerAPIReady:                state.DockerAPIReady,
		DockerAvailable:               state.DockerAvailable && state.DockerSocketPath != "",
		DockerLastError:               state.DockerLastError,
		DockerLogPath:                 state.DockerLogPath,
		DockerNetworkSupport:          dockerCapabilitySupport(metadata),
		DockerNetworkStatus:           dockerCapabilityStatus(metadata, state.DockerNetworkReady),
		DockerNetworkReady:            state.DockerNetworkReady,
		DockerNetworkLastError:        state.DockerNetworkLastError,
		DockerGuestIPAddress:          state.DockerGuestIPAddress,
		DockerDefaultRouteReady:       state.DockerDefaultRouteReady,
		DockerDNSReady:                state.DockerDNSReady,
		DockerBridgeReady:             state.DockerBridgeReady,
		DockerPullReady:               state.DockerPullReady,
		DockerTCPForwardSocketPath:    state.DockerTCPForwardSocketPath,
		DockerTCPForwardGuestPort:     state.DockerTCPForwardGuestPort,
		DockerPortRelayPID:            state.DockerPortRelayPID,
		DockerPortRelaySupport:        dockerCapabilitySupport(metadata),
		DockerPortRelayStatus:         dockerCapabilityStatus(metadata, state.DockerPortRelayReady && state.DockerPortRelayPID != 0 && processAlive(state.DockerPortRelayPID)),
		DockerPortRelayReady:          state.DockerPortRelayReady && state.DockerPortRelayPID != 0 && processAlive(state.DockerPortRelayPID),
		DockerPortRelayLogPath:        state.DockerPortRelayLogPath,
		DockerPublishedPorts:          dockerPublishedPorts,
		DockerNetworkBackend:          state.DockerNetworkBackend,
		DockerNetworkBackendPID:       state.DockerNetworkBackendPID,
		DockerNetworkSocketPath:       state.DockerNetworkSocketPath,
		DockerNetworkBackendLogPath:   state.DockerNetworkBackendLogPath,
		DockerTapName:                 state.DockerTapName,
		DockerGuestMAC:                state.DockerGuestMAC,
		HostShareSupport:              hostShareSupport(metadata),
		HostShareStatus:               hostShareStatus(metadata, state),
		HostShareStatusReason:         hostShareStatusReason(metadata, state),
		HostSharePath:                 state.HostSharePath,
		HostShareReady:                state.HostShareReady,
		HostShareLastError:            state.HostShareLastError,
		HostShareMountSocketPath:      state.HostShareMountSocketPath,
		HostShareGuestPort:            state.HostShareGuestPort,
		HostShareVirtioFSPID:          state.HostShareVirtioFSPID,
		HostShareVirtioFSSocketPath:   state.HostShareVirtioFSSocketPath,
		HostShareVirtioFSLogPath:      state.HostShareVirtioFSLogPath,
		KubernetesSupport:             kubernetesSupport(metadata),
		KubernetesStatus:              kubernetesStatus(metadata, state),
		KubernetesReady:               state.KubernetesReady,
		KubernetesLastError:           state.KubernetesLastError,
		KubernetesKubeconfigPath:      firstNonEmpty(state.KubernetesKubeconfigPath, metadata.KubeconfigPath),
		KubernetesContext:             state.KubernetesContext,
		KubernetesAPIServerURL:        state.KubernetesAPIServerURL,
		KubernetesAPIServerPort:       state.KubernetesAPIServerPort,
		KubernetesAPIServerTargetPort: state.KubernetesAPIServerTargetPort,
		KubernetesRelayPID:            state.KubernetesRelayPID,
		KubernetesRelayLogPath:        state.KubernetesRelayLogPath,
		RegistryReady:                 state.RegistryReady,
		RegistryLastError:             state.RegistryLastError,
		RegistryURL:                   state.RegistryURL,
		RegistryPort:                  state.RegistryPort,
		RegistryTargetPort:            state.RegistryTargetPort,
		RegistryRelayPID:              state.RegistryRelayPID,
		RegistryRelayLogPath:          state.RegistryRelayLogPath,
		RegistryHostFromCluster:       state.RegistryHostFromCluster,
		RegistryLocalHostingUpdated:   state.RegistryLocalHostingUpdated,
		SerialLogPath:                 serialLogPath,
		BackendLogPath:                backendLogPath,
		EventLogPath:                  eventLogPath,
		StartedAt:                     state.StartedAt,
		UpdatedAt:                     state.UpdatedAt,
		LastStartDuration:             time.Duration(state.LastStartDurationMS) * time.Millisecond,
		LogPaths:                      vmLogPaths(vmDir, metadata.Backend, state),
	}, nil
}

func (m *Manager) DeleteVM(ctx context.Context, name string, options spindvm.DeleteOptions) (spindvm.DeleteResult, error) {
	if err := validateStoreName(name); err != nil {
		return spindvm.DeleteResult{}, err
	}
	vmDir := filepath.Join(m.VMStore, name)
	stat, err := os.Stat(vmDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return spindvm.DeleteResult{}, fmt.Errorf("VM %q: %w", name, ErrNotFound)
		}
		return spindvm.DeleteResult{}, fmt.Errorf("check VM %q: %w", name, err)
	}
	if !stat.IsDir() {
		return spindvm.DeleteResult{}, fmt.Errorf("VM %q is not a directory", name)
	}

	running, err := m.vmRunning(vmDir)
	if err != nil {
		return spindvm.DeleteResult{}, err
	}
	if running {
		if !options.Force {
			return spindvm.DeleteResult{}, fmt.Errorf("VM %q is running; stop it first or use --force", name)
		}
		if _, err := m.stop(ctx, name, stopModeDelete); err != nil {
			return spindvm.DeleteResult{}, fmt.Errorf("stop VM %q before delete: %w", name, err)
		}
	}

	result := spindvm.DeleteResult{}
	if options.UnmergeKubeconfig {
		paths, err := m.KubeconfigUnmerge(name, KubeconfigUnmergeOptions{})
		if err != nil {
			return spindvm.DeleteResult{}, fmt.Errorf("unmerge kubeconfig for VM %q: %w", name, err)
		}
		result.KubeconfigRemoved = paths
	}
	size, err := directorySize(vmDir)
	if err != nil {
		return spindvm.DeleteResult{}, fmt.Errorf("size VM %q: %w", name, err)
	}
	if err := os.RemoveAll(vmDir); err != nil {
		return spindvm.DeleteResult{}, fmt.Errorf("delete VM %q: %w", name, err)
	}
	result.SizeBytes = size
	return result, nil
}

func (m *Manager) vmRunning(vmDir string) (bool, error) {
	state, err := readState(vmDir)
	if err != nil {
		return false, err
	}
	return state.Status == "running" && state.PID != 0 && processAlive(state.PID), nil
}

func (m *Manager) DeleteImage(name string, options spindimage.DeleteOptions) (spindimage.DeleteResult, error) {
	references, err := m.imageReferences(name)
	if err != nil {
		return spindimage.DeleteResult{}, err
	}
	return m.imageStore().Delete(name, references, options)
}

func (m *Manager) BuildImage(ctx context.Context, name string, options spindimage.BuildOptions) (spindimage.BuildResult, error) {
	return m.imageStore().Build(ctx, name, options)
}

func (m *Manager) imageStore() spindimage.Store {
	return spindimage.Store{
		Home:                  m.Home,
		ImageStore:            m.ImageStore,
		TemplateStore:         m.TemplateStore,
		BuiltinTemplateSource: m.BuiltinTemplateSource,
	}
}

func (m *Manager) imageReferences(imageName string) ([]string, error) {
	entries, err := os.ReadDir(m.VMStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read VM store: %w", err)
	}
	references := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var metadata spindvm.Metadata
		path := filepath.Join(m.VMStore, entry.Name(), vmMetadataName)
		if err := readJSON(path, &metadata); err != nil {
			return nil, fmt.Errorf("read VM metadata %q while checking image references: %w", path, err)
		}
		if metadata.Image == imageName {
			references = append(references, entry.Name())
		}
	}
	sort.Strings(references)
	return references, nil
}

func dockerCapabilitySupport(metadata spindvm.Metadata) string {
	if !isDockerHostImage(metadata) {
		return capabilityUnsupported
	}
	return capabilitySupported
}

func dockerCapabilityStatus(metadata spindvm.Metadata, ready bool) string {
	if dockerCapabilitySupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(ready)
}

func hostShareSupport(metadata spindvm.Metadata) string {
	if !isDockerHostImage(metadata) {
		return capabilityUnsupported
	}
	if metadata.Backend == BackendCloudHypervisor && metadata.FromSnapshot {
		return capabilityUnsupported
	}
	return capabilitySupported
}

func hostShareStatus(metadata spindvm.Metadata, state spindvm.State) string {
	if hostShareSupport(metadata) == capabilityUnsupported {
		return capabilityNotApplicable
	}
	return capabilityStatus(state.HostShareReady)
}

func hostShareStatusReason(metadata spindvm.Metadata, state spindvm.State) string {
	if metadata.Backend == BackendCloudHypervisor && metadata.FromSnapshot && isDockerHostImage(metadata) {
		return "cloud-hypervisor snapshot restore prioritizes saved-state restore speed"
	}
	return state.HostShareLastError
}

func capabilityStatus(ready bool) string {
	if ready {
		return capabilityReady
	}
	return capabilityUnavailable
}

func (m *Manager) ListSnapshots() ([]spindsnapshot.Info, error) {
	return m.snapshotStore().List()
}

func (m *Manager) SnapshotInfo(name string) (spindsnapshot.Info, error) {
	return m.snapshotStore().Info(name)
}

func (m *Manager) DeleteSnapshot(name string) error {
	return m.snapshotStore().Delete(name)
}

func (m *Manager) DeleteSnapshotWithSize(name string) (int64, error) {
	return m.snapshotStore().DeleteWithSize(name)
}

func (m *Manager) PruneSnapshots(olderThan time.Duration, backend string, dryRun bool) ([]spindsnapshot.Info, int64, error) {
	return m.snapshotStore().Prune(olderThan, backend, dryRun)
}

func (m *Manager) snapshotStore() spindsnapshot.Store {
	return spindsnapshot.Store{SnapshotStore: m.SnapshotStore}
}

func sortVMInfos(vms []spindvm.Info) {
	sort.Slice(vms, func(i int, j int) bool {
		return vms[i].Name < vms[j].Name
	})
}

func vmLogPaths(vmDir string, backend string, state spindvm.State) []string {
	candidates := []string{}
	switch backend {
	case BackendCloudHypervisor:
		candidates = append(candidates,
			state.VMMLogPath,
			state.SerialLogPath,
			state.EventLogPath,
			state.HostShareVirtioFSLogPath,
			state.KubernetesRelayLogPath,
			state.RegistryRelayLogPath,
			filepath.Join(vmDir, "cloud-hypervisor.log"),
			filepath.Join(vmDir, "serial.log"),
			filepath.Join(vmDir, "cloud-hypervisor-event.log"),
			filepath.Join(vmDir, "virtiofsd.log"),
		)
	default:
		candidates = append(candidates, state.KubernetesRelayLogPath, state.RegistryRelayLogPath, filepath.Join(vmDir, vmLogName))
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func vmNamedLogPaths(vmDir string, backend string, state spindvm.State) (serial string, backendLog string, event string) {
	switch backend {
	case BackendCloudHypervisor:
		serial = firstNonEmpty(state.SerialLogPath, filepath.Join(vmDir, "serial.log"))
		backendLog = firstNonEmpty(state.VMMLogPath, filepath.Join(vmDir, "cloud-hypervisor.log"))
		event = firstNonEmpty(state.EventLogPath, filepath.Join(vmDir, "cloud-hypervisor-event.log"))
	default:
		backendLog = filepath.Join(vmDir, vmLogName)
	}
	return serial, backendLog, event
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func directorySize(root string) (int64, error) {
	return store.DirectorySize(root)
}

func (m *Manager) startVirtualizationFramework(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata) (bool, error) {
	runnerPath, err := m.resolveVirtualizationRunner(ctx)
	if err != nil {
		return false, err
	}

	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		return false, fmt.Errorf("read runner config: %w", err)
	}
	normalizeRunnerConfig(vmDir, &config)
	prepareHostShareConfig(&config)
	if err := writeJSON(filepath.Join(vmDir, vmConfigName), config, 0o644); err != nil {
		return false, fmt.Errorf("write runner config: %w", err)
	}
	_ = os.Remove(config.ExecSocketPath)
	_ = os.Remove(config.ControlSocketPath)
	_ = os.Remove(config.DockerSocketPath)
	_ = os.Remove(config.TCPForwardPath)
	_ = os.Remove(config.HostShareSocketPath)

	logFile, err := os.OpenFile(filepath.Join(vmDir, vmLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open runner log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, runnerPath, "start", "--config", filepath.Join(vmDir, vmConfigName))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start Swift runner: %w", err)
	}
	pid := cmd.Process.Pid

	now := time.Now().UTC()
	if err := writeState(vmDir, spindvm.State{
		Status:            "running",
		Backend:           metadata.Backend,
		PID:               pid,
		ExecSocketPath:    config.ExecSocketPath,
		ControlSocketPath: config.ControlSocketPath,
		ExecPort:          config.ExecPort,
		ExecReady:         false,
		StartedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return false, err
	}
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return false, fmt.Errorf("release Swift runner process: %w", err)
	}
	if err := waitForExecRelay(config.ExecSocketPath, pid, 15*time.Second); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := waitForExecReady(ctx, vmDir, config.ExecSocketPath, metadata.ExecUser, 60*time.Second); err != nil {
		return true, fmt.Errorf("VM %q started but exec SSH was not ready: %w", name, err)
	}
	state := spindvm.State{
		Status:                   "running",
		Backend:                  metadata.Backend,
		PID:                      pid,
		ExecSocketPath:           config.ExecSocketPath,
		ControlSocketPath:        config.ControlSocketPath,
		ExecPort:                 config.ExecPort,
		ExecReady:                true,
		HostSharePath:            config.HostSharePath,
		HostShareMountSocketPath: config.HostShareSocketPath,
		HostShareGuestPort:       config.HostSharePort,
		StartedAt:                now,
		LastStartDurationMS:      time.Since(now).Milliseconds(),
		UpdatedAt:                time.Now().UTC(),
	}
	state = m.configureHostShare(ctx, metadata, state)
	state = m.configureDockerEndpoint(ctx, name, vmDir, metadata, state)
	state = m.configureKubernetesEndpoint(ctx, name, vmDir, metadata, state)
	state.UpdatedAt = time.Now().UTC()
	if err := writeState(vmDir, state); err != nil {
		return true, err
	}
	return true, nil
}

func (m *Manager) startVirtualizationFrameworkRestore(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata) (bool, error) {
	runnerPath, err := m.resolveVirtualizationRunner(ctx)
	if err != nil {
		return false, err
	}

	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		return false, fmt.Errorf("read runner config: %w", err)
	}
	normalizeRunnerConfig(vmDir, &config)
	prepareHostShareConfig(&config)
	if config.RestoreStatePath == "" {
		config.RestoreStatePath = metadata.RestoreStatePath
	}
	if config.RestoreStatePath == "" {
		config.RestoreStatePath = snapshotRestoreStatePath(vmDir, BackendVirtualizationFramework)
	}
	verifyFiles := []string{
		filepath.Join(vmDir, imageKernelName),
		filepath.Join(vmDir, imageInitramfsName),
	}
	for _, disk := range virtualizationFrameworkDiskMetadata(config) {
		verifyFiles = append(verifyFiles, filepath.Join(vmDir, disk.Name))
	}
	if err := vz.VerifyRestoreFiles(config.RestoreStatePath, verifyFiles); err != nil {
		return false, err
	}
	if err := writeJSON(filepath.Join(vmDir, vmConfigName), config, 0o644); err != nil {
		return false, fmt.Errorf("write runner config: %w", err)
	}
	_ = os.Remove(config.ExecSocketPath)
	_ = os.Remove(config.ControlSocketPath)
	_ = os.Remove(config.DockerSocketPath)
	_ = os.Remove(config.TCPForwardPath)
	_ = os.Remove(config.HostShareSocketPath)

	logFile, err := os.OpenFile(filepath.Join(vmDir, vmLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open runner log: %w", err)
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, runnerPath, "restore", "--config", filepath.Join(vmDir, vmConfigName))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("open Swift runner stdout: %w", err)
	}
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("restore Swift runner: %w", err)
	}
	pid := cmd.Process.Pid

	events := make(chan string, 16)
	go vz.ReadLifecycle(stdout, logFile, events)

	now := time.Now().UTC()
	state := spindvm.State{
		Status:                   "running",
		Backend:                  metadata.Backend,
		PID:                      pid,
		ExecSocketPath:           config.ExecSocketPath,
		ControlSocketPath:        config.ControlSocketPath,
		ExecPort:                 config.ExecPort,
		ExecReady:                false,
		HostSharePath:            config.HostSharePath,
		HostShareMountSocketPath: config.HostShareSocketPath,
		HostShareGuestPort:       config.HostSharePort,
		StartedAt:                now,
		UpdatedAt:                now,
	}
	if err := writeState(vmDir, state); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return false, err
	}
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		return false, fmt.Errorf("release Swift runner process: %w", err)
	}
	for _, event := range []string{"restore-completed", "resume-completed", "exec-relay-ready"} {
		if err := vz.WaitLifecycleEvent(events, func() bool { return processAlive(pid) }, event, 30*time.Second); err != nil {
			_ = signalProcess(pid, syscall.SIGTERM)
			_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: metadata.Backend, UpdatedAt: time.Now().UTC()})
			return false, err
		}
	}
	if err := waitForExecRelay(config.ExecSocketPath, pid, 15*time.Second); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: metadata.Backend, UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := waitForExecReady(ctx, vmDir, config.ExecSocketPath, metadata.ExecUser, 60*time.Second); err != nil {
		return true, fmt.Errorf("VM %q restored but exec SSH was not ready: %w", name, err)
	}
	state.ExecReady = true
	state.LastStartDurationMS = time.Since(now).Milliseconds()
	state = m.configureHostShare(ctx, metadata, state)
	state = m.configureDockerEndpoint(ctx, name, vmDir, metadata, state)
	state = m.configureKubernetesEndpoint(ctx, name, vmDir, metadata, state)
	state.UpdatedAt = time.Now().UTC()
	if err := writeState(vmDir, state); err != nil {
		return true, err
	}
	return true, nil
}

func (m *Manager) Stop(ctx context.Context, name string) (bool, error) {
	return m.stop(ctx, name, stopModeGraceful)
}

func (m *Manager) stop(ctx context.Context, name string, mode stopMode) (bool, error) {
	if err := validateStoreName(name); err != nil {
		return false, err
	}

	vmDir := filepath.Join(m.VMStore, name)
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("VM %q: %w", name, ErrNotFound)
		}
		return false, fmt.Errorf("read VM metadata: %w", err)
	}
	if metadata.Backend == "" {
		metadata.Backend = BackendVirtualizationFramework
	}

	state, err := readState(vmDir)
	if err != nil {
		return false, err
	}
	if state.Status != "running" || state.PID == 0 || !processAlive(state.PID) {
		cleanupHostShare(ctx, state)
		cleanupDockerEndpointForVM(vmDir, state)
		cleanupKubernetesEndpoint(state)
		return false, writeState(vmDir, spindvm.State{Status: "stopped", Backend: metadata.Backend, UpdatedAt: time.Now().UTC()})
	}
	if metadata.Backend == BackendCloudHypervisor {
		unmountHostShare(ctx, state)
		cleanupDockerEndpointForVM(vmDir, state)
		cleanupKubernetesEndpoint(state)
		return m.stopCloudHypervisor(ctx, vmDir, state, mode)
	}

	cleanupHostShare(ctx, state)
	cleanupDockerEndpointForVM(vmDir, state)
	cleanupKubernetesEndpoint(state)

	if m.RunnerPath != "" {
		if err := m.runRunner(ctx, "stop", "--pid", fmt.Sprintf("%d", state.PID)); err != nil {
			return false, fmt.Errorf("stop Swift runner: %w", err)
		}
	} else if err := signalProcess(state.PID, syscall.SIGTERM); err != nil {
		return false, err
	}

	if err := waitForExit(state.PID, 5*time.Second); err != nil {
		return false, err
	}

	return true, writeState(vmDir, spindvm.State{Status: "stopped", Backend: metadata.Backend, UpdatedAt: time.Now().UTC()})
}

func (m *Manager) runRunner(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, m.RunnerPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %w: %s", args, err, string(output))
	}
	return nil
}

func (m *Manager) runnerMachineIdentifier(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, m.RunnerPath, "machine-id")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", errors.New("Swift runner returned empty machine identifier")
	}
	return value, nil
}

func runnerConfig(name string, vmDir string, image spindimage.Metadata) vz.RunnerConfig {
	commandLine := image.KernelCommandLine
	if commandLine == "" {
		commandLine = "console=hvc0"
	}
	memoryMiB := defaultMemoryMiB
	if strings.Contains(image.Name, "docker") {
		memoryMiB = defaultDockerMemoryMiB
	}
	if image.MemoryMiB != 0 {
		memoryMiB = image.MemoryMiB
	}
	cpuCount := defaultCPUCount
	if image.CPUCount != 0 {
		cpuCount = image.CPUCount
	}
	config := vz.RunnerConfig{
		Name:                name,
		KernelPath:          filepath.Join(vmDir, imageKernelName),
		InitramfsPath:       filepath.Join(vmDir, imageInitramfsName),
		LogPath:             filepath.Join(vmDir, vmLogName),
		ExecSocketPath:      filepath.Join(vmDir, "exec.sock"),
		ControlSocketPath:   filepath.Join(vmDir, "control.sock"),
		DockerSocketPath:    filepath.Join(vmDir, "docker.sock"),
		MachineIdentifier:   newGenericMachineIdentifier(),
		TCPForwardPath:      filepath.Join(vmDir, "tcp-forward.sock"),
		HostShareSocketPath: filepath.Join(vmDir, "mount.sock"),
		HostShareTag:        defaultHostShareTag,
		ExecPort:            defaultExecPort,
		DockerPort:          defaultDockerPort,
		TCPForwardPort:      defaultDockerTCPForwardPort,
		HostSharePort:       defaultHostShareMountPort,
		NetworkMAC:          dockerGuestMAC(),
		CPUCount:            cpuCount,
		MemoryMiB:           memoryMiB,
		KernelCommandLine:   commandLine,
	}
	if len(image.Disks) == 0 {
		config.DiskPath = filepath.Join(vmDir, imageDiskName)
	} else {
		config.Disks = runnerConfigDisks(vmDir, image)
	}
	return config
}

func normalizeRunnerConfig(vmDir string, config *vz.RunnerConfig) {
	if config.LogPath == "" {
		config.LogPath = filepath.Join(vmDir, vmLogName)
	}
	if config.ExecSocketPath == "" {
		config.ExecSocketPath = filepath.Join(vmDir, "exec.sock")
	}
	if config.ControlSocketPath == "" {
		config.ControlSocketPath = filepath.Join(vmDir, "control.sock")
	}
	if config.ExecPort == 0 {
		config.ExecPort = defaultExecPort
	}
	if config.DockerSocketPath == "" {
		config.DockerSocketPath = filepath.Join(vmDir, "docker.sock")
	}
	if config.DockerPort == 0 {
		config.DockerPort = defaultDockerPort
	}
	if config.TCPForwardPath == "" {
		config.TCPForwardPath = filepath.Join(vmDir, "tcp-forward.sock")
	}
	if config.TCPForwardPort == 0 {
		config.TCPForwardPort = defaultDockerTCPForwardPort
	}
	if config.HostShareSocketPath == "" {
		config.HostShareSocketPath = filepath.Join(vmDir, "mount.sock")
	}
	if config.HostShareTag == "" {
		config.HostShareTag = defaultHostShareTag
	}
	if config.HostSharePort == 0 {
		config.HostSharePort = defaultHostShareMountPort
	}
	if config.CPUCount == 0 {
		config.CPUCount = defaultCPUCount
	}
	if config.MemoryMiB == 0 {
		config.MemoryMiB = defaultMemoryMiB
	}
	if config.KernelCommandLine == "" {
		config.KernelCommandLine = "console=hvc0"
	}
	if config.MachineIdentifier == "" {
		config.MachineIdentifier = newGenericMachineIdentifier()
	}
	if config.NetworkMAC == "" {
		config.NetworkMAC = dockerGuestMAC()
	}
}

func newGenericMachineIdentifier() string {
	data := []byte{
		0x62, 0x70, 0x6c, 0x69, 0x73, 0x74, 0x30, 0x30,
		0xd1, 0x01, 0x02, 0x54, 0x55, 0x55, 0x49, 0x44,
		0x4f, 0x10, 0x10,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0x08, 0x0b, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x23,
	}
	if _, err := cryptorand.Read(data[19:35]); err != nil {
		now := time.Now().UnixNano()
		for index := 0; index < 16; index++ {
			data[19+index] = byte(now >> (8 * (index % 8)))
		}
	}
	return base64.StdEncoding.EncodeToString(data)
}

func cloudHypervisorConfig(name string, vmDir string, image spindimage.Metadata) cloudhypervisor.Config {
	commandLine := cloudHypervisorKernelCommandLine(image)
	if commandLine == "" {
		commandLine = "console=ttyS0 root=/dev/vda1 rw"
	}
	memoryMiB := defaultMemoryMiB
	if strings.Contains(image.Name, "docker") {
		memoryMiB = defaultDockerMemoryMiB
	}
	if image.MemoryMiB != 0 {
		memoryMiB = image.MemoryMiB
	}
	cpuCount := defaultCPUCount
	if image.CPUCount != 0 {
		cpuCount = image.CPUCount
	}
	config := cloudhypervisor.Config{
		Name:                name,
		KernelPath:          filepath.Join(vmDir, imageKernelName),
		InitramfsPath:       filepath.Join(vmDir, imageInitramfsName),
		DiskPath:            filepath.Join(vmDir, imageDiskName),
		Disks:               cloudHypervisorDisks(vmDir, image),
		APISocketPath:       filepath.Join(vmDir, "cloud-hypervisor-api.sock"),
		VsockSocketPath:     filepath.Join(vmDir, "cloud-hypervisor-vsock.sock"),
		SerialLogPath:       filepath.Join(vmDir, "serial.log"),
		VMMLogPath:          filepath.Join(vmDir, "cloud-hypervisor.log"),
		EventLogPath:        filepath.Join(vmDir, "cloud-hypervisor-event.log"),
		GuestCID:            defaultGuestCID,
		HostShareSocketPath: filepath.Join(vmDir, "cloud-hypervisor-virtiofs.sock"),
		HostShareTag:        defaultHostShareTag,
		HostShareLogPath:    filepath.Join(vmDir, "virtiofsd.log"),
		HostSharePort:       defaultHostShareMountPort,
		ExecPort:            defaultExecPort,
		CPUCount:            cpuCount,
		MemoryMiB:           memoryMiB,
		KernelCommandLine:   commandLine,
	}
	if strings.Contains(image.Name, "docker") {
		config.NetBackend = cloudHypervisorNetworkBackendPasst
		config.NetSocketPath = filepath.Join(vmDir, "passt.sock")
		config.NetLogPath = filepath.Join(vmDir, "passt.log")
		config.NetMAC = dockerGuestMAC()
	}
	return config
}

func ensureCloudHypervisorDockerNetworkConfig(name string, metadata spindvm.Metadata, config *cloudhypervisor.Config) {
	if !isDockerHostImage(metadata) {
		return
	}
	if config.NetBackend == "" {
		config.NetBackend = cloudHypervisorNetworkBackendPasst
	}
	if config.NetBackend == cloudHypervisorNetworkBackendPasst {
		vmDir := filepath.Dir(config.APISocketPath)
		if config.NetSocketPath == "" {
			config.NetSocketPath = filepath.Join(vmDir, "passt.sock")
		}
		if config.NetLogPath == "" {
			config.NetLogPath = filepath.Join(vmDir, "passt.log")
		}
		config.NetTapName = ""
	}
	if config.NetBackend == cloudHypervisorNetworkBackendTap && config.NetTapName == "" {
		config.NetTapName = dockerTapName(name)
	}
	if config.NetMAC == "" {
		config.NetMAC = dockerGuestMAC()
	}
}

func normalizeCloudHypervisorConfig(vmDir string, config *cloudhypervisor.Config) {
	if config.APISocketPath == "" {
		config.APISocketPath = filepath.Join(vmDir, "cloud-hypervisor-api.sock")
	}
	if config.VsockSocketPath == "" {
		config.VsockSocketPath = filepath.Join(vmDir, "cloud-hypervisor-vsock.sock")
	}
	if config.SerialLogPath == "" {
		config.SerialLogPath = filepath.Join(vmDir, "serial.log")
	}
	if config.VMMLogPath == "" {
		config.VMMLogPath = filepath.Join(vmDir, "cloud-hypervisor.log")
	}
	if config.EventLogPath == "" {
		config.EventLogPath = filepath.Join(vmDir, "cloud-hypervisor-event.log")
	}
	if config.NetBackend == cloudHypervisorNetworkBackendPasst {
		if config.NetSocketPath == "" {
			config.NetSocketPath = filepath.Join(vmDir, "passt.sock")
		}
		if config.NetLogPath == "" {
			config.NetLogPath = filepath.Join(vmDir, "passt.log")
		}
	}
	if config.HostShareSocketPath == "" {
		config.HostShareSocketPath = filepath.Join(vmDir, "cloud-hypervisor-virtiofs.sock")
	}
	if config.HostShareTag == "" {
		config.HostShareTag = defaultHostShareTag
	}
	if config.HostShareLogPath == "" {
		config.HostShareLogPath = filepath.Join(vmDir, "virtiofsd.log")
	}
	if config.HostSharePort == 0 {
		config.HostSharePort = defaultHostShareMountPort
	}
	if config.GuestCID == 0 {
		config.GuestCID = defaultGuestCID
	}
	if config.ExecPort == 0 {
		config.ExecPort = defaultExecPort
	}
	if config.CPUCount == 0 {
		config.CPUCount = defaultCPUCount
	}
	if config.MemoryMiB == 0 {
		config.MemoryMiB = defaultMemoryMiB
	}
	if config.KernelCommandLine == "" {
		config.KernelCommandLine = "console=ttyS0 root=/dev/vda1 rw"
	}
}

func dockerTapName(name string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return "spind" + strconv.FormatUint(uint64(hash.Sum32()), 36)
}

func dockerGuestMAC() string {
	buffer := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := cryptorand.Read(buffer[2:]); err != nil {
		now := time.Now().UnixNano()
		for index := 2; index < len(buffer); index++ {
			buffer[index] = byte(now >> (8 * (index - 2)))
		}
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", buffer[0], buffer[1], buffer[2], buffer[3], buffer[4], buffer[5])
}

func cloudHypervisorDisks(vmDir string, image spindimage.Metadata) []cloudhypervisor.Disk {
	if len(image.Disks) == 0 {
		return nil
	}
	disks := make([]cloudhypervisor.Disk, 0, len(image.Disks))
	for _, disk := range image.Disks {
		disks = append(disks, cloudhypervisor.Disk{
			Path:      filepath.Join(vmDir, disk.Name),
			ReadOnly:  disk.ReadOnly,
			ImageType: disk.ImageType,
		})
	}
	return disks
}

func runnerConfigDisks(vmDir string, image spindimage.Metadata) []vz.Disk {
	if len(image.Disks) == 0 {
		return nil
	}
	disks := make([]vz.Disk, 0, len(image.Disks))
	for _, disk := range image.Disks {
		disks = append(disks, vz.Disk{
			Path:     filepath.Join(vmDir, disk.Name),
			ReadOnly: disk.ReadOnly,
		})
	}
	return disks
}

func applyImageAuthorizedKeyCommandLine(commandLine *string, image spindimage.Metadata, publicKey []byte) {
	if image.SSHAuthorizedKeyCommandLineParam == "" {
		return
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey)
	*commandLine = strings.TrimSpace(*commandLine + " " + image.SSHAuthorizedKeyCommandLineParam + "=" + encoded)
}

func cloudHypervisorKernelCommandLine(image spindimage.Metadata) string {
	commandLine := image.KernelCommandLine
	if image.ImageType == "microvm-nix" {
		return strings.TrimSpace(commandLine)
	}
	fields := strings.Fields(commandLine)
	if len(fields) == 0 {
		return "console=ttyS0 root=/dev/vda1 rw"
	}
	filtered := make([]string, 0, len(fields)+3)
	filtered = append(filtered, "console=ttyS0", "root=/dev/vda1", "rw", "spind.ch_net=1", "kido.ch_net=1")
	hasModules := false
	for _, field := range fields {
		if strings.HasPrefix(field, "console=") || strings.HasPrefix(field, "root=") || field == "rw" || field == "ro" || strings.HasPrefix(field, "spind.ch_net=") || strings.HasPrefix(field, "kido.ch_net=") {
			continue
		}
		if strings.HasPrefix(field, "modules=") {
			field = ensureCommaListValue(field, "virtio_net")
			hasModules = true
		}
		filtered = append(filtered, field)
	}
	if !hasModules {
		filtered = append(filtered, "modules=virtio_net")
	}
	return strings.Join(filtered, " ")
}

func ensureCommaListValue(field string, value string) string {
	prefix, values, ok := strings.Cut(field, "=")
	if !ok {
		return field
	}
	for _, existing := range strings.Split(values, ",") {
		if existing == value {
			return field
		}
	}
	if values == "" {
		return prefix + "=" + value
	}
	return prefix + "=" + values + "," + value
}

func generateVMSSHKey() ([]byte, []byte, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate VM SSH key: %w", err)
	}
	privateBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, nil, fmt.Errorf("marshal VM SSH private key: %w", err)
	}
	authorizedKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal VM SSH public key: %w", err)
	}
	return pem.EncodeToMemory(privateBlock), ssh.MarshalAuthorizedKey(authorizedKey), nil
}

func injectAuthorizedKey(diskPath string, publicKeyPath string, publicKey []byte, execUser string) error {
	if _, err := exec.LookPath("debugfs"); err != nil {
		return fmt.Errorf("debugfs is required to inject VM SSH key: %w", err)
	}
	if execUser == "" {
		execUser = defaultExecUser
	}
	if err := validateStoreName(execUser); err != nil {
		return fmt.Errorf("exec user %q: %w", execUser, err)
	}
	sshDir := "/home/" + execUser + "/.ssh"
	authorizedKeysPath := sshDir + "/authorized_keys"
	partition, err := diskimage.FirstPartition(diskPath)
	if err != nil {
		return fmt.Errorf("read root filesystem partition: %w", err)
	}

	workDir, err := os.MkdirTemp(filepath.Dir(diskPath), ".spind-debugfs-*")
	if err != nil {
		return fmt.Errorf("create debugfs workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	rootfsPath := filepath.Join(workDir, "rootfs.img")
	if err := copyDiskPartition(diskPath, rootfsPath, partition); err != nil {
		return err
	}
	commandPath := filepath.Join(workDir, "commands")
	dumpPath := filepath.Join(workDir, "authorized_keys")
	commands := []string{
		"mkdir /home",
		"mkdir /home/" + execUser,
		"mkdir " + sshDir,
		"set_inode_field /home/" + execUser + " uid 1000",
		"set_inode_field /home/" + execUser + " gid 1000",
		"set_inode_field /home/" + execUser + " mode 040755",
		"set_inode_field " + sshDir + " uid 1000",
		"set_inode_field " + sshDir + " gid 1000",
		"set_inode_field " + sshDir + " mode 040700",
		"rm " + authorizedKeysPath,
		fmt.Sprintf("write %s %s", publicKeyPath, authorizedKeysPath),
		"set_inode_field " + authorizedKeysPath + " uid 1000",
		"set_inode_field " + authorizedKeysPath + " gid 1000",
		"set_inode_field " + authorizedKeysPath + " mode 0100600",
	}
	if err := os.WriteFile(commandPath, []byte(strings.Join(commands, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("write debugfs commands: %w", err)
	}
	if output, err := exec.Command("debugfs", "-w", "-f", commandPath, rootfsPath).CombinedOutput(); err != nil {
		return fmt.Errorf("inject VM SSH public key: %w: %s", err, string(output))
	}
	if output, err := exec.Command("debugfs", "-R", fmt.Sprintf("dump %s %s", authorizedKeysPath, dumpPath), rootfsPath).CombinedOutput(); err != nil {
		return fmt.Errorf("verify VM SSH public key: %w: %s", err, string(output))
	}
	dumped, err := os.ReadFile(dumpPath)
	if err != nil {
		return fmt.Errorf("read injected VM SSH public key: %w", err)
	}
	if !bytes.Equal(dumped, publicKey) {
		return errors.New("injected VM SSH public key does not match generated key")
	}
	if err := writeDiskPartition(diskPath, rootfsPath, partition); err != nil {
		return err
	}
	return nil
}

func copyDiskPartition(diskPath string, rootfsPath string, partition diskimage.Partition) error {
	disk, err := os.Open(diskPath)
	if err != nil {
		return fmt.Errorf("open disk image: %w", err)
	}
	defer disk.Close()
	rootfs, err := os.OpenFile(rootfsPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create root filesystem partition image: %w", err)
	}
	defer rootfs.Close()
	if err := rootfs.Truncate(partition.Size); err != nil {
		return fmt.Errorf("size root filesystem partition image: %w", err)
	}
	copied, err := filecopy.CopySparseContentsRange(disk, rootfs, partition.Start, partition.Size)
	if err != nil && !errors.Is(err, filecopy.ErrSparseCopyUnsupported) {
		return fmt.Errorf("copy root filesystem partition: %w", err)
	}
	if !copied {
		reader := io.NewSectionReader(disk, partition.Start, partition.Size)
		if _, err := io.Copy(rootfs, reader); err != nil {
			return fmt.Errorf("copy root filesystem partition: %w", err)
		}
	}
	return rootfs.Sync()
}

func writeDiskPartition(diskPath string, rootfsPath string, partition diskimage.Partition) error {
	rootfs, err := os.Open(rootfsPath)
	if err != nil {
		return fmt.Errorf("open root filesystem partition image: %w", err)
	}
	defer rootfs.Close()
	info, err := rootfs.Stat()
	if err != nil {
		return fmt.Errorf("stat root filesystem partition image: %w", err)
	}
	if info.Size() > partition.Size {
		return fmt.Errorf("root filesystem partition image grew from %d to %d bytes", partition.Size, info.Size())
	}
	disk, err := os.OpenFile(diskPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open disk image for partition writeback: %w", err)
	}
	defer disk.Close()
	if _, err := disk.Seek(partition.Start, io.SeekStart); err != nil {
		return fmt.Errorf("seek root filesystem partition: %w", err)
	}
	if _, err := io.Copy(disk, rootfs); err != nil {
		return fmt.Errorf("write root filesystem partition: %w", err)
	}
	return disk.Sync()
}

func waitForExecReady(ctx context.Context, vmDir string, socketPath string, user string, timeout time.Duration) error {
	signer, err := readSSHSigner(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect exec relay: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))
	sshConn, channels, requests, err := ssh.NewClientConn(conn, "spind-vsock", &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	})
	if err != nil {
		return fmt.Errorf("start SSH session: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, channels, requests)
	return client.Close()
}

func validateStoreName(name string) error {
	if !store.ValidName(name) {
		return fmt.Errorf("%q: %w", name, ErrInvalidName)
	}
	return nil
}

func validateBackend(backend string) error {
	switch backend {
	case BackendVirtualizationFramework, BackendCloudHypervisor:
		return nil
	default:
		return fmt.Errorf("backend %q: %w", backend, ErrInvalidName)
	}
}

func defaultBackend() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return BackendCloudHypervisor, nil
	case "darwin":
		return BackendVirtualizationFramework, nil
	default:
		return "", fmt.Errorf("unsupported operating system %q: cannot choose VM backend", runtime.GOOS)
	}
}

func defaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".spind"), nil
}

func envFirst(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func findExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err == nil {
		return path
	}
	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func readState(vmDir string) (spindvm.State, error) {
	state, err := spindvm.ReadState(vmDir)
	if err != nil {
		return state, err
	}
	return state, nil
}

func writeState(vmDir string, state spindvm.State) error {
	if err := spindvm.WriteState(vmDir, state); err != nil {
		return fmt.Errorf("write VM state: %w", err)
	}
	return nil
}

func readJSON(path string, value any) error {
	return store.ReadJSON(path, value)
}

func writeJSON(path string, value any, mode os.FileMode) error {
	return store.WriteJSON(path, value, mode)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func signalProcess(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}

func waitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reaped, err := reapProcess(pid)
		if err != nil {
			return err
		}
		if reaped {
			return nil
		}
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not stop within %s", pid, timeout)
}

func waitForExecRelay(path string, pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return errors.New("Swift runner exited before exec relay was ready")
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check exec relay: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("exec relay was not ready within %s", timeout)
}

func reapProcess(pid int) (bool, error) {
	var status syscall.WaitStatus
	reapedPID, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if err != nil {
		if errors.Is(err, syscall.ECHILD) {
			return false, nil
		}
		return false, fmt.Errorf("wait for process %d: %w", pid, err)
	}
	return reapedPID == pid, nil
}
