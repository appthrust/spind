package start

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
	"golang.org/x/crypto/ssh"
)

type cloudHypervisorDockerNetworkState struct {
	Backend    string
	PID        int
	SocketPath string
	LogPath    string
	TapName    string
	GuestMAC   string
}

const (
	cloudHypervisorGracefulShutdownTimeout = 10 * time.Second
	cloudHypervisorShutdownTimeout         = 5 * time.Second
	cloudHypervisorDeleteShutdownTimeout   = 1 * time.Second
)

func (m *Manager) startCloudHypervisor(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata) (bool, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, errors.New("Cloud Hypervisor backend requires /dev/kvm")
		}
		return false, fmt.Errorf("check /dev/kvm: %w", err)
	}
	if m.CloudHypervisorPath == "" {
		return false, errors.New("cloud-hypervisor binary not found; set SPIND_CLOUD_HYPERVISOR or install cloud-hypervisor")
	}

	var config cloudhypervisor.Config
	if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &config); err != nil {
		return false, fmt.Errorf("read Cloud Hypervisor config: %w", err)
	}
	normalizeCloudHypervisorConfig(vmDir, &config)
	ensureCloudHypervisorDockerNetworkConfig(name, metadata, &config)
	hostShareErr := ""
	hostShareVirtioFSPID := 0
	hostShareDeviceReady := false
	if err := writeJSON(filepath.Join(vmDir, cloudHypervisorConfigName), config, 0o644); err != nil {
		return false, fmt.Errorf("write Cloud Hypervisor config: %w", err)
	}
	startConfig := config
	dockerNetworkErr := ""
	dockerNetworkReady := false
	dockerNetworkState := cloudHypervisorDockerNetworkState{}
	if isDockerHostImage(metadata) {
		networkState, err := prepareCloudHypervisorDockerNetwork(ctx, config)
		if err != nil {
			dockerNetworkErr = err.Error()
			disableCloudHypervisorDockerNetwork(&startConfig)
		} else {
			dockerNetworkState = networkState
			dockerNetworkReady = true
		}
	}
	if isDockerHostImage(metadata) {
		if err := prepareCloudHypervisorHostShareConfig(&startConfig); err != nil {
			hostShareErr = err.Error()
			startConfig.HostSharePath = ""
		}
		if startConfig.HostSharePath != "" {
			pid, err := startCloudHypervisorVirtioFS(ctx, startConfig)
			if err != nil {
				hostShareErr = err.Error()
			} else {
				hostShareVirtioFSPID = pid
				hostShareDeviceReady = true
			}
		}
	}
	for _, path := range []string{
		config.APISocketPath,
		config.VsockSocketPath,
		config.SerialLogPath,
		config.VMMLogPath,
		config.EventLogPath,
	} {
		_ = os.Remove(path)
	}

	argsConfig := startConfig
	if !hostShareDeviceReady {
		argsConfig.HostShareSocketPath = ""
	}
	args := cloudHypervisorArgs(argsConfig)
	cmd := exec.CommandContext(ctx, m.CloudHypervisorPath, args...)
	logFile, err := os.OpenFile(config.VMMLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open Cloud Hypervisor log: %w", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		cleanupCloudHypervisorDockerNetwork(ctx, cloudHypervisorDockerNetworkVMState(dockerNetworkState))
		cleanupHostShareArtifacts(ctx, spindvm.State{
			HostShareVirtioFSPID:        hostShareVirtioFSPID,
			HostShareVirtioFSSocketPath: startConfig.HostShareSocketPath,
		})
		return false, fmt.Errorf("start Cloud Hypervisor: %w", err)
	}
	pid := cmd.Process.Pid

	now := time.Now().UTC()
	state := spindvm.State{
		Status:                         "running",
		Backend:                        BackendCloudHypervisor,
		PID:                            pid,
		ExecSocketPath:                 config.VsockSocketPath,
		ExecPort:                       config.ExecPort,
		CloudHypervisorAPISocketPath:   config.APISocketPath,
		CloudHypervisorVsockSocketPath: config.VsockSocketPath,
		GuestCID:                       config.GuestCID,
		SerialLogPath:                  config.SerialLogPath,
		VMMLogPath:                     config.VMMLogPath,
		EventLogPath:                   config.EventLogPath,
		ExecReady:                      false,
		DockerNetworkReady:             dockerNetworkReady,
		DockerNetworkLastError:         dockerNetworkErr,
		DockerNetworkBackend:           dockerNetworkState.Backend,
		DockerNetworkBackendPID:        dockerNetworkState.PID,
		DockerNetworkSocketPath:        dockerNetworkState.SocketPath,
		DockerNetworkBackendLogPath:    dockerNetworkState.LogPath,
		DockerTapName:                  dockerNetworkState.TapName,
		DockerGuestMAC:                 startConfig.NetMAC,
		HostSharePath:                  startConfig.HostSharePath,
		HostShareLastError:             hostShareErr,
		HostShareGuestPort:             startConfig.HostSharePort,
		HostShareVirtioFSPID:           hostShareVirtioFSPID,
		HostShareVirtioFSSocketPath:    startConfig.HostShareSocketPath,
		HostShareVirtioFSLogPath:       startConfig.HostShareLogPath,
		StartedAt:                      now,
		UpdatedAt:                      now,
	}
	if err := writeState(vmDir, state); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		return false, err
	}
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		return false, fmt.Errorf("release Cloud Hypervisor process: %w", err)
	}
	if err := cloudhypervisor.WaitForAPI(ctx, pid, config.APISocketPath, 15*time.Second, processAlive); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := cloudhypervisor.WaitForSocketFile(config.VsockSocketPath, pid, 15*time.Second, processAlive); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := waitForCloudHypervisorExecReady(ctx, vmDir, config, metadata.ExecUser, 60*time.Second); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, fmt.Errorf("VM %q started but exec SSH was not ready: %w", name, err)
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

func (m *Manager) startCloudHypervisorRestore(ctx context.Context, name string, vmDir string, metadata spindvm.Metadata) (bool, error) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, errors.New("Cloud Hypervisor backend requires /dev/kvm")
		}
		return false, fmt.Errorf("check /dev/kvm: %w", err)
	}
	if m.CloudHypervisorPath == "" {
		return false, errors.New("cloud-hypervisor binary not found; set SPIND_CLOUD_HYPERVISOR or install cloud-hypervisor")
	}

	var config cloudhypervisor.Config
	if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &config); err != nil {
		return false, fmt.Errorf("read Cloud Hypervisor config: %w", err)
	}
	normalizeCloudHypervisorConfig(vmDir, &config)
	ensureCloudHypervisorDockerNetworkConfig(name, metadata, &config)
	hostShareErr := ""
	hostShareVirtioFSPID := 0
	if err := writeJSON(filepath.Join(vmDir, cloudHypervisorConfigName), config, 0o644); err != nil {
		return false, fmt.Errorf("write Cloud Hypervisor config: %w", err)
	}
	startConfig := config
	dockerNetworkErr := ""
	dockerNetworkReady := false
	dockerNetworkState := cloudHypervisorDockerNetworkState{}
	if isDockerHostImage(metadata) {
		networkState, err := prepareCloudHypervisorDockerNetwork(ctx, config)
		if err != nil {
			dockerNetworkErr = err.Error()
			if config.NetBackend == cloudHypervisorNetworkBackendPasst {
				return false, fmt.Errorf("prepare Cloud Hypervisor Docker network: %w", err)
			}
		} else {
			dockerNetworkState = networkState
			dockerNetworkReady = true
		}
		if !metadata.FromSnapshot {
			if err := prepareCloudHypervisorHostShareConfig(&startConfig); err != nil {
				hostShareErr = err.Error()
				startConfig.HostSharePath = ""
			} else {
				if startConfig.HostSharePath != "" {
					pid, err := startCloudHypervisorVirtioFS(ctx, startConfig)
					if err != nil {
						hostShareErr = err.Error()
					} else {
						hostShareVirtioFSPID = pid
					}
				}
			}
		}
	}
	for _, path := range []string{
		config.APISocketPath,
		config.VsockSocketPath,
		config.SerialLogPath,
		config.VMMLogPath,
		config.EventLogPath,
	} {
		_ = os.Remove(path)
	}
	restoreDir := metadata.RestoreStatePath
	if restoreDir == "" {
		restoreDir = filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName)
	}
	if err := verifyCloudHypervisorSnapshotFiles(restoreDir, cloudHypervisorDiskMetadata(config)); err != nil {
		cleanupCloudHypervisorDockerNetwork(ctx, cloudHypervisorDockerNetworkVMState(dockerNetworkState))
		cleanupHostShareArtifacts(ctx, spindvm.State{
			HostShareVirtioFSPID:        hostShareVirtioFSPID,
			HostShareVirtioFSSocketPath: startConfig.HostShareSocketPath,
		})
		return false, err
	}
	if isDockerHostImage(metadata) {
		if err := rewriteCloudHypervisorSnapshotConfig(filepath.Join(restoreDir, cloudHypervisorSnapshotConfigName), vmDir, restoreDir, config); err != nil {
			cleanupCloudHypervisorDockerNetwork(ctx, cloudHypervisorDockerNetworkVMState(dockerNetworkState))
			cleanupHostShareArtifacts(ctx, spindvm.State{
				HostShareVirtioFSPID:        hostShareVirtioFSPID,
				HostShareVirtioFSSocketPath: startConfig.HostShareSocketPath,
			})
			return false, err
		}
	}

	args := []string{
		"--restore", fmt.Sprintf("source_url=file://%s,memory_restore_mode=ondemand", restoreDir),
		"--api-socket", fmt.Sprintf("path=%s", config.APISocketPath),
		"--log-file", config.VMMLogPath,
		"--event-monitor", fmt.Sprintf("path=%s", config.EventLogPath),
	}
	cmd := exec.CommandContext(ctx, m.CloudHypervisorPath, args...)
	logFile, err := os.OpenFile(config.VMMLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("open Cloud Hypervisor log: %w", err)
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		cleanupCloudHypervisorDockerNetwork(ctx, cloudHypervisorDockerNetworkVMState(dockerNetworkState))
		cleanupHostShareArtifacts(ctx, spindvm.State{
			HostShareVirtioFSPID:        hostShareVirtioFSPID,
			HostShareVirtioFSSocketPath: startConfig.HostShareSocketPath,
		})
		return false, fmt.Errorf("restore Cloud Hypervisor: %w", err)
	}
	pid := cmd.Process.Pid

	now := time.Now().UTC()
	state := spindvm.State{
		Status:                         "running",
		Backend:                        BackendCloudHypervisor,
		PID:                            pid,
		ExecSocketPath:                 config.VsockSocketPath,
		ExecPort:                       config.ExecPort,
		CloudHypervisorAPISocketPath:   config.APISocketPath,
		CloudHypervisorVsockSocketPath: config.VsockSocketPath,
		GuestCID:                       config.GuestCID,
		SerialLogPath:                  config.SerialLogPath,
		VMMLogPath:                     config.VMMLogPath,
		EventLogPath:                   config.EventLogPath,
		ExecReady:                      false,
		DockerNetworkReady:             dockerNetworkReady,
		DockerNetworkLastError:         dockerNetworkErr,
		DockerNetworkBackend:           dockerNetworkState.Backend,
		DockerNetworkBackendPID:        dockerNetworkState.PID,
		DockerNetworkSocketPath:        dockerNetworkState.SocketPath,
		DockerNetworkBackendLogPath:    dockerNetworkState.LogPath,
		DockerTapName:                  dockerNetworkState.TapName,
		DockerGuestMAC:                 config.NetMAC,
		HostSharePath:                  startConfig.HostSharePath,
		HostShareLastError:             hostShareErr,
		HostShareGuestPort:             startConfig.HostSharePort,
		HostShareVirtioFSPID:           hostShareVirtioFSPID,
		HostShareVirtioFSSocketPath:    startConfig.HostShareSocketPath,
		HostShareVirtioFSLogPath:       startConfig.HostShareLogPath,
		StartedAt:                      now,
		UpdatedAt:                      now,
	}
	if err := writeState(vmDir, state); err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		return false, err
	}
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		return false, fmt.Errorf("release Cloud Hypervisor process: %w", err)
	}
	if err := cloudhypervisor.WaitForAPI(ctx, pid, config.APISocketPath, 15*time.Second, processAlive); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := cloudhypervisor.Request(ctx, config.APISocketPath, http.MethodPut, "/api/v1/vm.resume"); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, fmt.Errorf("resume restored Cloud Hypervisor VM: %w", err)
	}
	if err := cloudhypervisor.WaitForSocketFile(config.VsockSocketPath, pid, 15*time.Second, processAlive); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, err
	}
	if err := waitForCloudHypervisorExecReady(ctx, vmDir, config, metadata.ExecUser, 60*time.Second); err != nil {
		_ = signalProcess(pid, syscall.SIGTERM)
		cleanupCloudHypervisorDockerNetwork(ctx, state)
		cleanupHostShareArtifacts(ctx, state)
		_ = writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		return false, fmt.Errorf("VM %q restored but exec SSH was not ready: %w", name, err)
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

func (m *Manager) stopCloudHypervisor(ctx context.Context, vmDir string, state spindvm.State, mode stopMode) (bool, error) {
	if state.CloudHypervisorAPISocketPath != "" {
		if mode == stopModeGraceful {
			_ = cloudhypervisor.Request(ctx, state.CloudHypervisorAPISocketPath, http.MethodPut, "/api/v1/vm.shutdown")
			if err := waitForExit(state.PID, cloudHypervisorGracefulShutdownTimeout); err == nil {
				cleanupCloudHypervisorDockerNetwork(ctx, state)
				cleanupHostShareArtifacts(ctx, state)
				return true, writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
			}
		}
		requestCtx, cancel := cloudHypervisorRequestContext(ctx, mode)
		_ = cloudhypervisor.Request(requestCtx, state.CloudHypervisorAPISocketPath, http.MethodPut, "/api/v1/vmm.shutdown")
		cancel()
		if err := waitForExit(state.PID, cloudHypervisorShutdownWait(mode)); err == nil {
			cleanupCloudHypervisorDockerNetwork(ctx, state)
			cleanupHostShareArtifacts(ctx, state)
			return true, writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
		}
	}
	if err := signalProcess(state.PID, syscall.SIGTERM); err != nil {
		return false, err
	}
	if err := waitForExit(state.PID, cloudHypervisorShutdownWait(mode)); err != nil {
		_ = signalProcess(state.PID, syscall.SIGKILL)
		if killErr := waitForExit(state.PID, cloudHypervisorShutdownWait(mode)); killErr != nil {
			return false, err
		}
	}
	cleanupCloudHypervisorDockerNetwork(ctx, state)
	cleanupHostShareArtifacts(ctx, state)
	return true, writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
}

func cloudHypervisorRequestContext(ctx context.Context, mode stopMode) (context.Context, context.CancelFunc) {
	if mode == stopModeDelete {
		return context.WithTimeout(ctx, cloudHypervisorDeleteShutdownTimeout)
	}
	return ctx, func() {}
}

func cloudHypervisorShutdownWait(mode stopMode) time.Duration {
	if mode == stopModeDelete {
		return cloudHypervisorDeleteShutdownTimeout
	}
	return cloudHypervisorShutdownTimeout
}

func cloudHypervisorArgs(config cloudhypervisor.Config) []string {
	memoryArg := fmt.Sprintf("size=%dM", config.MemoryMiB)
	if cloudHypervisorRequiresSharedMemory(config) {
		memoryArg += ",shared=on"
	}
	args := []string{
		"--kernel", config.KernelPath,
		"--initramfs", config.InitramfsPath,
		"--cmdline", config.KernelCommandLine,
		"--cpus", fmt.Sprintf("boot=%d", config.CPUCount),
		"--memory", memoryArg,
		"--rng", "src=/dev/urandom",
		"--vsock", fmt.Sprintf("cid=%d,socket=%s", config.GuestCID, config.VsockSocketPath),
		"--api-socket", fmt.Sprintf("path=%s", config.APISocketPath),
		"--serial", fmt.Sprintf("file=%s", config.SerialLogPath),
		"--console", "off",
		"--log-file", config.VMMLogPath,
		"--event-monitor", fmt.Sprintf("path=%s", config.EventLogPath),
	}
	for _, disk := range cloudHypervisorConfigDisks(config) {
		args = append(args, "--disk", cloudHypervisorDiskArg(disk))
	}
	switch config.NetBackend {
	case cloudHypervisorNetworkBackendPasst:
		if config.NetSocketPath != "" {
			netArg := "vhost_user=on,socket=" + config.NetSocketPath + ",vhost_mode=client"
			if config.NetMAC != "" {
				netArg += ",mac=" + config.NetMAC
			}
			args = append(args, "--net", netArg)
		}
	case cloudHypervisorNetworkBackendTap:
		if config.NetTapName != "" {
			netArg := "tap=" + config.NetTapName
			if config.NetMAC != "" {
				netArg += ",mac=" + config.NetMAC
			}
			args = append(args, "--net", netArg)
		}
	default:
		// Legacy configs from before explicit network backends used NetTapName only.
		if config.NetTapName != "" {
			netArg := "tap=" + config.NetTapName
			if config.NetMAC != "" {
				netArg += ",mac=" + config.NetMAC
			}
			args = append(args, "--net", netArg)
		}
	}
	if config.HostSharePath != "" && config.HostShareSocketPath != "" {
		tag := config.HostShareTag
		if tag == "" {
			tag = defaultHostShareTag
		}
		fsArg := fmt.Sprintf("tag=%s,socket=%s,num_queues=1,queue_size=512", tag, config.HostShareSocketPath)
		args = append(args, "--fs", fsArg)
	}
	return args
}

func cloudHypervisorConfigDisks(config cloudhypervisor.Config) []cloudhypervisor.Disk {
	if len(config.Disks) > 0 {
		return config.Disks
	}
	return []cloudhypervisor.Disk{{Path: config.DiskPath}}
}

func cloudHypervisorDiskArg(disk cloudhypervisor.Disk) string {
	readOnly := "off"
	if disk.ReadOnly {
		readOnly = "on"
	}
	imageType := disk.ImageType
	if imageType == "" {
		imageType = "raw"
	}
	arg := fmt.Sprintf("path=%s,readonly=%s", disk.Path, readOnly)
	arg += ",image_type=" + imageType
	return arg
}

func cloudHypervisorRequiresSharedMemory(config cloudhypervisor.Config) bool {
	if config.HostSharePath != "" && config.HostShareSocketPath != "" {
		return true
	}
	return config.NetBackend == cloudHypervisorNetworkBackendPasst && config.NetSocketPath != ""
}

func disableCloudHypervisorDockerNetwork(config *cloudhypervisor.Config) {
	config.NetBackend = ""
	config.NetSocketPath = ""
	config.NetLogPath = ""
	config.NetTapName = ""
	config.NetMAC = ""
}

func prepareCloudHypervisorDockerNetwork(ctx context.Context, config cloudhypervisor.Config) (cloudHypervisorDockerNetworkState, error) {
	switch config.NetBackend {
	case cloudHypervisorNetworkBackendPasst:
		return startCloudHypervisorPasst(ctx, config)
	case cloudHypervisorNetworkBackendTap:
		return prepareCloudHypervisorDockerTapNetwork(ctx, config)
	default:
		if config.NetTapName == "" {
			return cloudHypervisorDockerNetworkState{}, errors.New("Cloud Hypervisor Docker network backend is missing")
		}
		return prepareCloudHypervisorDockerTapNetwork(ctx, config)
	}
}

func startCloudHypervisorPasst(ctx context.Context, config cloudhypervisor.Config) (cloudHypervisorDockerNetworkState, error) {
	state := cloudHypervisorDockerNetworkState{
		Backend:    cloudHypervisorNetworkBackendPasst,
		SocketPath: config.NetSocketPath,
		LogPath:    config.NetLogPath,
		GuestMAC:   config.NetMAC,
	}
	if config.NetSocketPath == "" {
		return state, errors.New("Cloud Hypervisor Docker passt socket path is missing")
	}
	if config.NetLogPath == "" {
		return state, errors.New("Cloud Hypervisor Docker passt log path is missing")
	}
	passtPath, err := passtExecutablePath()
	if err != nil {
		return state, err
	}
	_ = os.Remove(config.NetSocketPath)
	logFile, err := os.OpenFile(config.NetLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return state, fmt.Errorf("open passt log: %w", err)
	}
	defer logFile.Close()
	args := []string{
		"--vhost-user",
		"--socket", config.NetSocketPath,
		"--foreground",
		"--one-off",
		"--log-file", config.NetLogPath,
		"--runas", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
	}
	cmd := exec.CommandContext(ctx, passtPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return state, fmt.Errorf("start passt: %w", err)
	}
	state.PID = cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		_ = signalProcess(state.PID, syscall.SIGTERM)
		return state, fmt.Errorf("release passt process: %w", err)
	}
	if err := cloudhypervisor.WaitForSocketFile(config.NetSocketPath, state.PID, 5*time.Second, processAlive); err != nil {
		cleanupCloudHypervisorDockerNetwork(ctx, cloudHypervisorDockerNetworkVMState(state))
		return state, fmt.Errorf("wait for passt socket: %w", err)
	}
	return state, nil
}

func passtExecutablePath() (string, error) {
	if path := envFirst("SPIND_PASST", "KIDO_PASST"); path != "" {
		return path, nil
	}
	path, err := exec.LookPath("passt")
	if err != nil {
		return "", errors.New("passt binary not found; set SPIND_PASST or install passt")
	}
	return path, nil
}

func prepareCloudHypervisorDockerTapNetwork(ctx context.Context, config cloudhypervisor.Config) (cloudHypervisorDockerNetworkState, error) {
	state := cloudHypervisorDockerNetworkState{
		Backend:  cloudHypervisorNetworkBackendTap,
		TapName:  config.NetTapName,
		GuestMAC: config.NetMAC,
	}
	if config.NetTapName == "" {
		return state, errors.New("Cloud Hypervisor Docker tap name is missing")
	}
	cleanupStaleCloudHypervisorDockerTaps(ctx, config.NetTapName)
	_ = runNetworkCommand(ctx, "ip", "link", "delete", config.NetTapName)
	if err := runNetworkCommand(ctx, "ip", "tuntap", "add", "dev", config.NetTapName, "mode", "tap"); err != nil {
		return state, fmt.Errorf("create tap %s: %w", config.NetTapName, err)
	}
	_ = runNetworkCommand(ctx, "ip", "addr", "add", "192.168.127.1/30", "dev", config.NetTapName)
	if err := runNetworkCommand(ctx, "ip", "link", "set", config.NetTapName, "up"); err != nil {
		return state, fmt.Errorf("bring tap %s up: %w", config.NetTapName, err)
	}
	_ = runNetworkCommand(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1")
	if err := runNetworkCommand(ctx, "iptables", "-t", "nat", "-C", "POSTROUTING", "-s", "192.168.127.0/30", "-j", "MASQUERADE"); err != nil {
		_ = runNetworkCommand(ctx, "iptables", "-t", "nat", "-A", "POSTROUTING", "-s", "192.168.127.0/30", "-j", "MASQUERADE")
	}
	return state, nil
}

func cleanupStaleCloudHypervisorDockerTaps(ctx context.Context, keep string) {
	command := "ip"
	args := []string{"-o", "link", "show"}
	if os.Geteuid() != 0 {
		args = append([]string{"-n", command}, args...)
		command = "sudo"
	}
	output, err := exec.CommandContext(ctx, command, args...).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		if name == keep || (!strings.HasPrefix(name, "spind") && !strings.HasPrefix(name, "kido")) {
			continue
		}
		_ = runNetworkCommand(ctx, "ip", "link", "delete", name)
	}
}

func runNetworkCommand(ctx context.Context, command string, args ...string) error {
	cmdArgs := args
	if os.Geteuid() != 0 {
		cmdArgs = append([]string{"-n", command}, args...)
		command = "sudo"
	}
	cmd := exec.CommandContext(ctx, command, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(append([]string{command}, cmdArgs...), " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func cleanupCloudHypervisorDockerNetwork(ctx context.Context, state spindvm.State) {
	if state.DockerNetworkBackendPID != 0 && processAlive(state.DockerNetworkBackendPID) {
		_ = signalProcess(state.DockerNetworkBackendPID, syscall.SIGTERM)
		if err := waitForExit(state.DockerNetworkBackendPID, 3*time.Second); err != nil {
			_ = signalProcess(state.DockerNetworkBackendPID, syscall.SIGKILL)
			_ = waitForExit(state.DockerNetworkBackendPID, 3*time.Second)
		}
	}
	if state.DockerNetworkSocketPath != "" {
		_ = os.Remove(state.DockerNetworkSocketPath)
	}
	if state.DockerTapName != "" {
		_ = runNetworkCommand(ctx, "ip", "link", "delete", state.DockerTapName)
	}
}

func cloudHypervisorDockerNetworkVMState(state cloudHypervisorDockerNetworkState) spindvm.State {
	return spindvm.State{
		DockerNetworkBackend:        state.Backend,
		DockerNetworkBackendPID:     state.PID,
		DockerNetworkSocketPath:     state.SocketPath,
		DockerNetworkBackendLogPath: state.LogPath,
		DockerTapName:               state.TapName,
		DockerGuestMAC:              state.GuestMAC,
	}
}

func waitForCloudHypervisorExecReady(ctx context.Context, vmDir string, config cloudhypervisor.Config, user string, timeout time.Duration) error {
	signer, err := readSSHSigner(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := cloudhypervisor.DialVsock(ctx, config.VsockSocketPath, config.ExecPort)
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		sshConn, channels, requests, err := ssh.NewClientConn(conn, "spind-cloud-hypervisor-vsock", &ssh.ClientConfig{
			User: user,
			Auth: []ssh.AuthMethod{
				ssh.PublicKeys(signer),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
		if err != nil {
			_ = conn.Close()
			lastErr = fmt.Errorf("start SSH session: %w", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = conn.SetDeadline(time.Time{})
		client := ssh.NewClient(sshConn, channels, requests)
		return client.Close()
	}
	if lastErr != nil {
		return fmt.Errorf("Cloud Hypervisor exec SSH was not ready within %s: %w", timeout, lastErr)
	}
	return fmt.Errorf("Cloud Hypervisor exec SSH was not ready within %s", timeout)
}
