package start

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/backend/vz"
	"github.com/suin/spind/internal/spind/filecopy"
	spindimage "github.com/suin/spind/internal/spind/image"
	spindkind "github.com/suin/spind/internal/spind/kind"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func (m *Manager) SnapshotCreate(ctx context.Context, snapshotName string, vmName string) error {
	return m.SnapshotCreateWithOptions(ctx, snapshotName, vmName, spindsnapshot.CreateOptions{})
}

func (m *Manager) SnapshotCreateWithOptions(ctx context.Context, snapshotName string, vmName string, options spindsnapshot.CreateOptions) error {
	if err := validateStoreName(snapshotName); err != nil {
		return fmt.Errorf("snapshot %q: %w", snapshotName, err)
	}
	if err := validateStoreName(vmName); err != nil {
		return fmt.Errorf("VM %q: %w", vmName, err)
	}

	vmDir := filepath.Join(m.VMStore, vmName)
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("VM %q: %w", vmName, ErrNotFound)
		}
		return fmt.Errorf("read VM metadata: %w", err)
	}
	hydrateVMMetadata(&metadata)

	state, err := readState(vmDir)
	if err != nil {
		return err
	}
	if state.Status != "running" || state.PID == 0 || !processAlive(state.PID) || !state.ExecReady {
		return fmt.Errorf("VM %q: %w", vmName, ErrNotRunning)
	}
	unmountHostShare(ctx, state)
	state.HostShareReady = false
	state.HostShareLastError = ""

	if err := os.MkdirAll(m.SnapshotStore, 0o755); err != nil {
		return fmt.Errorf("create snapshot store: %w", err)
	}
	snapshotDir := filepath.Join(m.SnapshotStore, snapshotName)
	if _, err := os.Stat(snapshotDir); err == nil {
		return fmt.Errorf("snapshot %q: %w", snapshotName, ErrAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("check snapshot %q: %w", snapshotName, err)
	}
	tmpDir, err := os.MkdirTemp(m.SnapshotStore, "."+snapshotName+"-")
	if err != nil {
		return fmt.Errorf("create temporary snapshot directory: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	kindMetadata, err := prepareKindSnapshot(ctx, tmpDir, vmDir, state, options)
	if err != nil {
		return err
	}

	switch metadata.Backend {
	case BackendCloudHypervisor:
		if err := m.createCloudHypervisorSnapshot(ctx, snapshotName, vmName, vmDir, tmpDir, metadata, state, kindMetadata); err != nil {
			return err
		}
	case BackendVirtualizationFramework:
		if err := m.createVirtualizationFrameworkSnapshot(ctx, snapshotName, vmName, vmDir, tmpDir, metadata, state, kindMetadata); err != nil {
			return err
		}
	default:
		return fmt.Errorf("VM %q has unsupported backend %q", vmName, metadata.Backend)
	}

	if err := os.Rename(tmpDir, snapshotDir); err != nil {
		return fmt.Errorf("publish snapshot: %w", err)
	}
	cleanup = false
	return nil
}

func (m *Manager) createCloudHypervisorSnapshot(ctx context.Context, snapshotName string, vmName string, vmDir string, tmpDir string, metadata spindvm.Metadata, state spindvm.State, kindMetadata *spindkind.Metadata) error {
	if state.CloudHypervisorAPISocketPath == "" {
		return errors.New("Cloud Hypervisor API socket path is missing")
	}
	var config cloudhypervisor.Config
	if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &config); err != nil {
		return fmt.Errorf("read Cloud Hypervisor config: %w", err)
	}
	normalizeCloudHypervisorConfig(vmDir, &config)
	disks := cloudHypervisorDiskMetadata(config)

	chDir := filepath.Join(tmpDir, snapshotCloudHypervisorDirName)
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		return fmt.Errorf("create Cloud Hypervisor snapshot directory: %w", err)
	}

	paused := false
	resumeOnError := true
	defer func() {
		if paused && resumeOnError {
			_ = cloudhypervisor.Request(ctx, state.CloudHypervisorAPISocketPath, "PUT", "/api/v1/vm.resume")
		}
	}()
	if err := cloudhypervisor.Request(ctx, state.CloudHypervisorAPISocketPath, "PUT", "/api/v1/vm.pause"); err != nil {
		return fmt.Errorf("pause Cloud Hypervisor VM: %w", err)
	}
	paused = true
	if err := cloudhypervisor.Snapshot(ctx, state.CloudHypervisorAPISocketPath, chDir); err != nil {
		return fmt.Errorf("create Cloud Hypervisor snapshot: %w", err)
	}
	for _, disk := range disks {
		if err := filecopy.Copy(filepath.Join(vmDir, disk.Name), filepath.Join(chDir, disk.Name), 0o644); err != nil {
			return fmt.Errorf("copy snapshot disk %q: %w", disk.Name, err)
		}
	}
	if err := filecopy.Copy(filepath.Join(vmDir, imageKernelName), filepath.Join(chDir, imageKernelName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot kernel: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, imageInitramfsName), filepath.Join(chDir, imageInitramfsName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot initramfs: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, vmSSHPrivateKeyName), filepath.Join(tmpDir, vmSSHPrivateKeyName), 0o600); err != nil {
		return fmt.Errorf("copy snapshot SSH private key: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, vmSSHPublicKeyName), filepath.Join(tmpDir, vmSSHPublicKeyName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot SSH public key: %w", err)
	}

	snapshot := spindsnapshot.Metadata{
		Name:              snapshotName,
		SourceVM:          vmName,
		Image:             metadata.Image,
		ImageType:         metadata.ImageType,
		CreatedAt:         time.Now().UTC(),
		Backend:           BackendCloudHypervisor,
		Architecture:      metadata.Architecture,
		ExecUser:          metadata.ExecUser,
		CPUCount:          metadata.CPUCount,
		MemoryMiB:         metadata.MemoryMiB,
		ExecPort:          metadata.ExecPort,
		KernelCommandLine: metadataKernelCommandLine(vmDir, metadata),
		Disks:             disks,
	}
	if kindMetadata != nil {
		snapshot.KindReady = true
	}
	if err := writeJSON(filepath.Join(tmpDir, snapshotMetadataName), snapshot, 0o644); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	if err := verifyCloudHypervisorSnapshotFiles(chDir, disks); err != nil {
		return err
	}

	if err := cloudhypervisor.Request(ctx, state.CloudHypervisorAPISocketPath, "PUT", "/api/v1/vmm.shutdown"); err != nil {
		return fmt.Errorf("shutdown Cloud Hypervisor after snapshot: %w", err)
	}
	if err := waitForExit(state.PID, 10*time.Second); err != nil {
		return fmt.Errorf("wait for Cloud Hypervisor shutdown after snapshot: %w", err)
	}
	cleanupDockerEndpointForVM(vmDir, state)
	cleanupCloudHypervisorDockerNetwork(ctx, state)
	cleanupHostShareArtifacts(ctx, state)
	cleanupKubernetesEndpoint(state)
	resumeOnError = false
	return writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()})
}

func (m *Manager) createVirtualizationFrameworkSnapshot(ctx context.Context, snapshotName string, vmName string, vmDir string, tmpDir string, metadata spindvm.Metadata, state spindvm.State, kindMetadata *spindkind.Metadata) error {
	if state.ControlSocketPath == "" {
		state.ControlSocketPath = filepath.Join(vmDir, "control.sock")
	}
	vfDir := filepath.Join(tmpDir, snapshotVirtualizationFrameworkDirName)
	if err := os.MkdirAll(vfDir, 0o755); err != nil {
		return fmt.Errorf("create Virtualization.framework snapshot directory: %w", err)
	}

	statePath := filepath.Join(vfDir, "state.vzvmsave")
	if err := vz.Save(ctx, state.ControlSocketPath, statePath); err != nil {
		return fmt.Errorf("save Virtualization.framework machine state: %w", err)
	}
	if err := waitForExit(state.PID, 30*time.Second); err != nil {
		return fmt.Errorf("wait for Swift runner exit after snapshot: %w", err)
	}
	cleanupDockerEndpointForVM(vmDir, state)
	cleanupHostShareArtifacts(ctx, state)
	cleanupKubernetesEndpoint(state)

	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		return fmt.Errorf("read runner config: %w", err)
	}
	disks := virtualizationFrameworkDiskMetadata(config)
	for _, disk := range disks {
		if err := filecopy.Copy(filepath.Join(vmDir, disk.Name), filepath.Join(vfDir, disk.Name), 0o644); err != nil {
			return fmt.Errorf("copy snapshot disk %q: %w", disk.Name, err)
		}
	}
	if err := filecopy.Copy(filepath.Join(vmDir, imageKernelName), filepath.Join(vfDir, imageKernelName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot kernel: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, imageInitramfsName), filepath.Join(vfDir, imageInitramfsName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot initramfs: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, vmSSHPrivateKeyName), filepath.Join(tmpDir, vmSSHPrivateKeyName), 0o600); err != nil {
		return fmt.Errorf("copy snapshot SSH private key: %w", err)
	}
	if err := filecopy.Copy(filepath.Join(vmDir, vmSSHPublicKeyName), filepath.Join(tmpDir, vmSSHPublicKeyName), 0o644); err != nil {
		return fmt.Errorf("copy snapshot SSH public key: %w", err)
	}

	snapshot := spindsnapshot.Metadata{
		Name:              snapshotName,
		SourceVM:          vmName,
		Image:             metadata.Image,
		ImageType:         metadata.ImageType,
		CreatedAt:         time.Now().UTC(),
		Backend:           BackendVirtualizationFramework,
		Architecture:      metadata.Architecture,
		ExecUser:          metadata.ExecUser,
		CPUCount:          metadata.CPUCount,
		MemoryMiB:         metadata.MemoryMiB,
		ExecPort:          metadata.ExecPort,
		KernelCommandLine: metadataKernelCommandLine(vmDir, metadata),
		Disks:             disks,
	}
	snapshot.MachineIdentifier = config.MachineIdentifier
	snapshot.NetworkMAC = config.NetworkMAC
	if kindMetadata != nil {
		snapshot.KindReady = true
	}
	if err := writeJSON(filepath.Join(tmpDir, snapshotMetadataName), snapshot, 0o644); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	verifyFiles := append([]string{"state.vzvmsave", imageKernelName, imageInitramfsName}, diskNames(disks)...)
	if err := vz.VerifySnapshotFiles(vfDir, verifyFiles); err != nil {
		return err
	}

	return writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendVirtualizationFramework, UpdatedAt: time.Now().UTC()})
}

func (m *Manager) createCloudHypervisorVMFromSnapshot(snapshotDir string, name string, vmDir string) error {
	var snapshot spindsnapshot.Metadata
	if err := readJSON(filepath.Join(snapshotDir, snapshotMetadataName), &snapshot); err != nil {
		return fmt.Errorf("read snapshot metadata: %w", err)
	}
	disks := snapshot.Disks
	if len(disks) == 0 {
		disks = []spindimage.DiskMetadata{{Name: imageDiskName}}
	}

	srcDir := filepath.Join(snapshotDir, snapshotCloudHypervisorDirName)
	dstDir := filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("create Cloud Hypervisor restore directory: %w", err)
	}
	for _, file := range []struct {
		name string
		link bool
	}{
		{name: cloudHypervisorSnapshotConfigName},
		{name: cloudHypervisorMemoryRangesName, link: true},
		{name: cloudHypervisorStateName},
		{name: imageKernelName, link: true},
		{name: imageInitramfsName, link: true},
	} {
		src := filepath.Join(srcDir, file.name)
		dst := filepath.Join(dstDir, file.name)
		if file.link {
			if err := filecopy.LinkOrCopy(src, dst, 0o644); err != nil {
				return fmt.Errorf("copy Cloud Hypervisor snapshot file %q: %w", file.name, err)
			}
			continue
		}
		if err := filecopy.Copy(src, dst, 0o644); err != nil {
			return fmt.Errorf("copy Cloud Hypervisor snapshot file %q: %w", file.name, err)
		}
	}
	for _, disk := range disks {
		src := filepath.Join(srcDir, disk.Name)
		dst := filepath.Join(dstDir, disk.Name)
		if disk.ReadOnly {
			if err := filecopy.LinkOrCopy(src, dst, 0o644); err != nil {
				return fmt.Errorf("copy Cloud Hypervisor snapshot disk %q: %w", disk.Name, err)
			}
			continue
		}
		if err := filecopy.Copy(src, dst, 0o644); err != nil {
			return fmt.Errorf("copy Cloud Hypervisor snapshot disk %q: %w", disk.Name, err)
		}
	}
	for _, name := range append([]string{imageKernelName, imageInitramfsName}, diskNames(disks)...) {
		if err := filecopy.LinkOrCopy(filepath.Join(dstDir, name), filepath.Join(vmDir, name), 0o644); err != nil {
			return fmt.Errorf("copy VM %s from snapshot: %w", name, err)
		}
	}

	config := cloudHypervisorConfig(name, vmDir, spindimage.Metadata{
		Name:              snapshot.Image,
		ImageType:         snapshot.ImageType,
		KernelCommandLine: snapshot.KernelCommandLine,
		Disks:             disks,
	})
	config.CPUCount = snapshot.CPUCount
	config.MemoryMiB = snapshot.MemoryMiB
	config.ExecPort = snapshot.ExecPort
	if err := writeJSON(filepath.Join(vmDir, cloudHypervisorConfigName), config, 0o644); err != nil {
		return fmt.Errorf("write Cloud Hypervisor config: %w", err)
	}
	if err := rewriteCloudHypervisorSnapshotConfig(filepath.Join(dstDir, cloudHypervisorSnapshotConfigName), vmDir, dstDir, config); err != nil {
		return err
	}
	return nil
}

func diskNames(disks []spindimage.DiskMetadata) []string {
	names := make([]string, 0, len(disks))
	for _, disk := range disks {
		names = append(names, disk.Name)
	}
	return names
}

func snapshotRestoreStatePath(vmDir string, backend string) string {
	switch backend {
	case BackendCloudHypervisor:
		return filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName)
	case BackendVirtualizationFramework:
		return filepath.Join(vmDir, snapshotVirtualizationFrameworkDirName, "state.vzvmsave")
	default:
		return ""
	}
}

func hydrateVMMetadata(metadata *spindvm.Metadata) {
	if metadata.Backend == "" {
		metadata.Backend = BackendVirtualizationFramework
	}
	if metadata.Architecture == "" {
		metadata.Architecture = runtime.GOARCH
	}
	if metadata.ExecUser == "" {
		metadata.ExecUser = defaultExecUser
	}
	if metadata.CPUCount == 0 {
		metadata.CPUCount = defaultCPUCount
	}
	if metadata.MemoryMiB == 0 {
		metadata.MemoryMiB = defaultMemoryMiB
	}
	if metadata.ExecPort == 0 {
		metadata.ExecPort = defaultExecPort
	}
}

func metadataKernelCommandLine(vmDir string, metadata spindvm.Metadata) string {
	switch metadata.Backend {
	case BackendCloudHypervisor:
		var config cloudhypervisor.Config
		if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &config); err == nil {
			return config.KernelCommandLine
		}
	case BackendVirtualizationFramework:
		var config vz.RunnerConfig
		if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err == nil {
			return config.KernelCommandLine
		}
	}
	return ""
}

func verifyCloudHypervisorSnapshotFiles(snapshotDir string, disks []spindimage.DiskMetadata) error {
	if len(disks) == 0 {
		disks = []spindimage.DiskMetadata{{Name: imageDiskName}}
	}
	for _, name := range []string{
		cloudHypervisorSnapshotConfigName,
		cloudHypervisorMemoryRangesName,
		cloudHypervisorStateName,
		imageKernelName,
		imageInitramfsName,
	} {
		info, err := os.Stat(filepath.Join(snapshotDir, name))
		if err != nil {
			return fmt.Errorf("verify Cloud Hypervisor snapshot file %q: %w", name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("Cloud Hypervisor snapshot file %q is a directory", name)
		}
	}
	for _, disk := range disks {
		info, err := os.Stat(filepath.Join(snapshotDir, disk.Name))
		if err != nil {
			return fmt.Errorf("verify Cloud Hypervisor snapshot disk %q: %w", disk.Name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("Cloud Hypervisor snapshot disk %q is a directory", disk.Name)
		}
	}
	return nil
}

func cloudHypervisorDiskMetadata(config cloudhypervisor.Config) []spindimage.DiskMetadata {
	disks := cloudHypervisorConfigDisks(config)
	metadata := make([]spindimage.DiskMetadata, 0, len(disks))
	for _, disk := range disks {
		metadata = append(metadata, spindimage.DiskMetadata{
			Name:      filepath.Base(disk.Path),
			ReadOnly:  disk.ReadOnly,
			ImageType: disk.ImageType,
		})
	}
	return metadata
}

func virtualizationFrameworkDiskMetadata(config vz.RunnerConfig) []spindimage.DiskMetadata {
	if len(config.Disks) == 0 {
		return []spindimage.DiskMetadata{{Name: imageDiskName}}
	}
	metadata := make([]spindimage.DiskMetadata, 0, len(config.Disks))
	for _, disk := range config.Disks {
		metadata = append(metadata, spindimage.DiskMetadata{
			Name:      filepath.Base(disk.Path),
			ReadOnly:  disk.ReadOnly,
			ImageType: "raw",
		})
	}
	return metadata
}

func rewriteCloudHypervisorSnapshotConfig(path string, vmDir string, restoreDir string, config cloudhypervisor.Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Cloud Hypervisor snapshot config: %w", err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("parse Cloud Hypervisor snapshot config: %w", err)
	}
	value = rewriteCloudHypervisorSnapshotValue(value, vmDir, restoreDir, config)
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open Cloud Hypervisor snapshot config temp file: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("encode Cloud Hypervisor snapshot config: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close Cloud Hypervisor snapshot config: %w", err)
	}
	return os.Rename(tmp, path)
}

func rewriteCloudHypervisorSnapshotValue(value any, vmDir string, restoreDir string, config cloudhypervisor.Config) any {
	switch typed := value.(type) {
	case map[string]any:
		if rewriteCloudHypervisorSnapshotNetDevice(typed, config) {
			return typed
		}
		for key, child := range typed {
			if key == "fs" {
				typed[key] = nil
				continue
			}
			typed[key] = rewriteCloudHypervisorSnapshotValue(child, vmDir, restoreDir, config)
		}
		return typed
	case []any:
		for index, child := range typed {
			typed[index] = rewriteCloudHypervisorSnapshotValue(child, vmDir, restoreDir, config)
		}
		return typed
	case string:
		return rewriteCloudHypervisorSnapshotPath(typed, vmDir, restoreDir, config)
	default:
		return typed
	}
}

func rewriteCloudHypervisorSnapshotNetDevice(value map[string]any, config cloudhypervisor.Config) bool {
	if !cloudHypervisorSnapshotValueLooksLikeNetDevice(value) {
		return false
	}
	if config.NetMAC != "" {
		value["mac"] = config.NetMAC
	}
	switch config.NetBackend {
	case cloudHypervisorNetworkBackendPasst:
		value["vhost_user"] = true
		value["vhost_socket"] = config.NetSocketPath
		value["vhost_mode"] = "Client"
		delete(value, "tap")
		delete(value, "ip")
		delete(value, "mask")
		delete(value, "host_mac")
		delete(value, "socket")
		delete(value, "fd")
		delete(value, "fds")
	case cloudHypervisorNetworkBackendTap, "":
		if config.NetTapName != "" {
			value["tap"] = config.NetTapName
			delete(value, "host_mac")
			delete(value, "socket")
			delete(value, "vhost_socket")
			delete(value, "vhost_user")
			delete(value, "vhost_mode")
		}
	}
	return true
}

func cloudHypervisorSnapshotValueLooksLikeNetDevice(value map[string]any) bool {
	if _, ok := value["path"]; ok {
		return false
	}
	if _, ok := value["image_type"]; ok {
		return false
	}
	for _, key := range []string{"tap", "host_mac", "ip", "mask", "mtu", "offload_csum", "offload_tso", "offload_ufo", "vhost_mode"} {
		if _, ok := value[key]; ok {
			return true
		}
	}
	if enabled, ok := value["vhost_user"].(bool); ok && enabled {
		return true
	}
	return false
}

func rewriteCloudHypervisorSnapshotPath(value string, vmDir string, restoreDir string, config cloudhypervisor.Config) string {
	clean := strings.TrimPrefix(value, "file://")
	replacements := map[string]string{
		"cloud-hypervisor-vsock.sock":          filepath.Join(vmDir, "cloud-hypervisor-vsock.sock"),
		"cloud-hypervisor-api.sock":            filepath.Join(vmDir, "cloud-hypervisor-api.sock"),
		"cloud-hypervisor-virtiofs.sock":       filepath.Join(vmDir, "cloud-hypervisor-virtiofs.sock"),
		"serial.log":                           filepath.Join(vmDir, "serial.log"),
		"cloud-hypervisor.log":                 filepath.Join(vmDir, "cloud-hypervisor.log"),
		"cloud-hypervisor-event.log":           filepath.Join(vmDir, "cloud-hypervisor-event.log"),
		imageKernelName:                        filepath.Join(restoreDir, imageKernelName),
		imageInitramfsName:                     filepath.Join(restoreDir, imageInitramfsName),
		cloudHypervisorSnapshotConfigName:      filepath.Join(restoreDir, cloudHypervisorSnapshotConfigName),
		cloudHypervisorMemoryRangesName:        filepath.Join(restoreDir, cloudHypervisorMemoryRangesName),
		cloudHypervisorStateName:               filepath.Join(restoreDir, cloudHypervisorStateName),
		vmCloudHypervisorSnapshotDirName + "/": restoreDir,
	}
	for _, disk := range cloudHypervisorConfigDisks(config) {
		name := filepath.Base(disk.Path)
		replacements[name] = filepath.Join(restoreDir, name)
	}
	for suffix, replacement := range replacements {
		if clean == suffix || strings.HasSuffix(clean, string(filepath.Separator)+suffix) {
			if strings.HasPrefix(value, "file://") {
				return "file://" + replacement
			}
			return replacement
		}
	}
	return value
}
