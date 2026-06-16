package start

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	osexec "os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	"github.com/suin/spind/internal/spind/backend/vz"
	"github.com/suin/spind/internal/spind/diskimage"
	spindimage "github.com/suin/spind/internal/spind/image"
	spindkind "github.com/suin/spind/internal/spind/kind"
	"github.com/suin/spind/internal/spind/kubeconfig"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
	"golang.org/x/crypto/ssh"
)

const testNetworkMAC = "02:00:00:00:00:01"

func TestDefaultBackendMatchesHostOS(t *testing.T) {
	backend, err := defaultBackend()
	switch runtime.GOOS {
	case "linux":
		if err != nil || backend != BackendCloudHypervisor {
			t.Fatalf("defaultBackend() = %q, %v; want %q, nil", backend, err, BackendCloudHypervisor)
		}
	case "darwin":
		if err != nil || backend != BackendVirtualizationFramework {
			t.Fatalf("defaultBackend() = %q, %v; want %q, nil", backend, err, BackendVirtualizationFramework)
		}
	default:
		if err == nil {
			t.Fatalf("defaultBackend() = %q, nil; want error", backend)
		}
	}
}

func TestCreateCopiesImageFilesAndWritesState(t *testing.T) {
	manager := newTestManager(t)
	writeTestImage(t, manager.ImageStore, "legacy")

	if err := manager.Create(context.Background(), "base", "legacy", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "base")
	for _, name := range []string{imageKernelName, imageInitramfsName, imageDiskName, vmSSHPrivateKeyName, vmSSHPublicKeyName, vmMetadataName, vmStateName, vmConfigName} {
		if _, err := os.Stat(filepath.Join(vmDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	privateKeyInfo, err := os.Stat(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if privateKeyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %o, want 0600", vmSSHPrivateKeyName, privateKeyInfo.Mode().Perm())
	}
	publicKey, err := os.ReadFile(filepath.Join(vmDir, vmSSHPublicKeyName))
	if err != nil {
		t.Fatal(err)
	}
	injectedKey := dumpDiskFile(t, filepath.Join(vmDir, imageDiskName), "/home/spind/.ssh/authorized_keys")
	if string(injectedKey) != string(publicKey) {
		t.Fatalf("injected authorized_keys = %q, want %q", string(injectedKey), string(publicKey))
	}

	state, err := readState(vmDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "stopped" {
		t.Fatalf("state.Status = %q, want stopped", state.Status)
	}
	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if config.ExecSocketPath == "" || config.ControlSocketPath == "" || config.ExecPort == 0 {
		t.Fatalf("exec connection info was not written: %#v", config)
	}
	if config.ExecPort != defaultExecPort {
		t.Fatalf("config.ExecPort = %d, want %d", config.ExecPort, defaultExecPort)
	}
	if config.DockerSocketPath == "" || config.DockerPort != defaultDockerPort {
		t.Fatalf("Docker relay config = %#v, want socket and port %d", config, defaultDockerPort)
	}
	if config.TCPForwardPath == "" || config.TCPForwardPort != defaultDockerTCPForwardPort {
		t.Fatalf("TCP forward config = %#v, want socket and port %d", config, defaultDockerTCPForwardPort)
	}
	if config.NetworkMAC == "" {
		t.Fatal("config.NetworkMAC is empty")
	}
}

func TestCreateCloudHypervisorWritesBackendMetadataAndConfig(t *testing.T) {
	manager := newTestManager(t)
	writeTestImage(t, manager.ImageStore, "legacy")

	if err := manager.Create(context.Background(), "base", "legacy", BackendCloudHypervisor); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "base")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Backend != BackendCloudHypervisor {
		t.Fatalf("metadata.Backend = %q, want %q", metadata.Backend, BackendCloudHypervisor)
	}
	var config cloudhypervisor.Config
	if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if config.APISocketPath == "" || config.VsockSocketPath == "" || config.GuestCID == 0 {
		t.Fatalf("Cloud Hypervisor config missing socket or CID: %#v", config)
	}
	if config.ExecPort != defaultExecPort {
		t.Fatalf("config.ExecPort = %d, want %d", config.ExecPort, defaultExecPort)
	}
	if !strings.Contains(config.KernelCommandLine, "root=/dev/vda1") {
		t.Fatalf("config.KernelCommandLine = %q, want root=/dev/vda1", config.KernelCommandLine)
	}
	if strings.Contains(config.KernelCommandLine, "root=LABEL=spind-root") {
		t.Fatalf("config.KernelCommandLine = %q, still contains root=LABEL=spind-root", config.KernelCommandLine)
	}
	if !strings.Contains(config.KernelCommandLine, "virtio_net") {
		t.Fatalf("config.KernelCommandLine = %q, want virtio_net module", config.KernelCommandLine)
	}
	if _, err := os.Stat(filepath.Join(vmDir, vmConfigName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Virtualization.framework config exists for Cloud Hypervisor backend: %v", err)
	}
}

func TestCreateDockerImageWritesConfiguredResourcesToMetadata(t *testing.T) {
	manager := newTestManager(t)
	writeTestImage(t, manager.ImageStore, "docker")

	if err := manager.Create(context.Background(), "docker", "docker", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "docker")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if metadata.CPUCount != config.CPUCount || metadata.MemoryMiB != config.MemoryMiB || metadata.ExecPort != config.ExecPort {
		t.Fatalf("metadata resources = cpu:%d memory:%d exec:%d, config = cpu:%d memory:%d exec:%d", metadata.CPUCount, metadata.MemoryMiB, metadata.ExecPort, config.CPUCount, config.MemoryMiB, config.ExecPort)
	}
	if metadata.MemoryMiB != defaultDockerMemoryMiB {
		t.Fatalf("metadata.MemoryMiB = %d, want %d", metadata.MemoryMiB, defaultDockerMemoryMiB)
	}
}

func TestCreateImageOverridesResources(t *testing.T) {
	manager := newTestManager(t)
	writeTestImage(t, manager.ImageStore, "docker")

	if err := manager.CreateFromImageWithOptions(context.Background(), "docker", "docker", BackendVirtualizationFramework, CreateOptions{
		CPUCount:  4,
		MemoryMiB: 8192,
	}); err != nil {
		t.Fatalf("CreateFromImageWithOptions returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "docker")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if metadata.CPUCount != 4 || metadata.MemoryMiB != 8192 {
		t.Fatalf("metadata resources = cpu:%d memory:%d, want cpu:4 memory:8192", metadata.CPUCount, metadata.MemoryMiB)
	}
	if config.CPUCount != 4 || config.MemoryMiB != 8192 {
		t.Fatalf("config resources = cpu:%d memory:%d, want cpu:4 memory:8192", config.CPUCount, config.MemoryMiB)
	}
}

func TestCloudHypervisorArgsIncludeDockerPasstNetwork(t *testing.T) {
	config := cloudHypervisorConfig("docker", t.TempDir(), spindimage.Metadata{Name: "docker"})
	if config.NetBackend != cloudHypervisorNetworkBackendPasst || config.NetSocketPath == "" || config.NetLogPath == "" || config.NetMAC == "" {
		t.Fatalf("Docker network config was not initialized: %#v", config)
	}
	if config.MemoryMiB != defaultDockerMemoryMiB {
		t.Fatalf("config.MemoryMiB = %d, want %d", config.MemoryMiB, defaultDockerMemoryMiB)
	}
	args := cloudHypervisorArgs(config)
	joined := strings.Join(args, " ")
	wantNet := "--net vhost_user=on,socket=" + config.NetSocketPath + ",vhost_mode=client,mac=" + config.NetMAC
	if !strings.Contains(joined, wantNet) {
		t.Fatalf("cloudHypervisorArgs = %q, want %q", joined, wantNet)
	}
	if !strings.Contains(joined, "--memory size=2048M,shared=on") {
		t.Fatalf("cloudHypervisorArgs = %q, want shared memory for passt", joined)
	}
}

func TestCreateVirtualizationFrameworkWritesMultipleDisks(t *testing.T) {
	manager := newTestManager(t)
	imageDir := filepath.Join(manager.ImageStore, "docker")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindimage.Metadata{
		Name:                             "docker",
		Architecture:                     runtime.GOARCH,
		CreatedAt:                        time.Now().UTC(),
		KernelCommandLine:                "root=fstab",
		ExecUser:                         defaultExecUser,
		SSHAuthorizedKeyCommandLineParam: "spind.ssh_authorized_key",
		Disks: []spindimage.DiskMetadata{
			{Name: "nix-store.img", ReadOnly: true, ImageType: "raw"},
			{Name: "docker-data.img", ImageType: "raw"},
		},
	}
	if err := writeJSON(filepath.Join(imageDir, imageMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range []string{imageKernelName, imageInitramfsName, "nix-store.img", "docker-data.img"} {
		if err := os.WriteFile(filepath.Join(imageDir, fileName), []byte(fileName), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := manager.Create(context.Background(), "base", "docker", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(manager.VMStore, "base", vmConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if config.DiskPath != "" {
		t.Fatalf("config.DiskPath = %q, want empty for multiple disks", config.DiskPath)
	}
	if len(config.Disks) != 2 {
		t.Fatalf("config.Disks = %#v, want 2 disks", config.Disks)
	}
	if config.Disks[0].Path != filepath.Join(manager.VMStore, "base", "nix-store.img") || !config.Disks[0].ReadOnly {
		t.Fatalf("first disk = %#v", config.Disks[0])
	}
	if config.Disks[1].Path != filepath.Join(manager.VMStore, "base", "docker-data.img") || config.Disks[1].ReadOnly {
		t.Fatalf("second disk = %#v", config.Disks[1])
	}
}

func TestRunnerConfigUsesDockerMemoryForDockerImage(t *testing.T) {
	config := runnerConfig("docker", t.TempDir(), spindimage.Metadata{Name: "docker"})
	if config.MemoryMiB != defaultDockerMemoryMiB {
		t.Fatalf("config.MemoryMiB = %d, want %d", config.MemoryMiB, defaultDockerMemoryMiB)
	}
}

func TestCloudHypervisorArgsIncludeVirtioFSHostShare(t *testing.T) {
	vmDir := t.TempDir()
	config := cloudHypervisorConfig("docker", vmDir, spindimage.Metadata{Name: "docker"})
	config.HostSharePath = t.TempDir()
	args := cloudHypervisorArgs(config)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--memory size=2048M,shared=on") {
		t.Fatalf("cloudHypervisorArgs = %q, want shared memory", joined)
	}
	wantFS := "--fs tag=spind-cwd,socket=" + filepath.Join(vmDir, "cloud-hypervisor-virtiofs.sock") + ",num_queues=1,queue_size=512"
	if !strings.Contains(joined, wantFS) {
		t.Fatalf("cloudHypervisorArgs = %q, want %q", joined, wantFS)
	}
}

func TestCreateFromSnapshotCopiesVirtualizationFrameworkArtifacts(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := writeTestVirtualizationFrameworkSnapshot(t, manager, "prepared")

	if err := manager.CreateFromSnapshot(context.Background(), "worker", "prepared", ""); err != nil {
		t.Fatalf("CreateFromSnapshot returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "worker")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.FromSnapshot || metadata.SourceSnapshot != "prepared" {
		t.Fatalf("metadata snapshot fields = %#v", metadata)
	}
	if metadata.Backend != BackendVirtualizationFramework {
		t.Fatalf("metadata.Backend = %q, want %q", metadata.Backend, BackendVirtualizationFramework)
	}
	if metadata.RestoreStatePath != filepath.Join(vmDir, snapshotVirtualizationFrameworkDirName, "state.vzvmsave") {
		t.Fatalf("metadata.RestoreStatePath = %q", metadata.RestoreStatePath)
	}
	for _, path := range []string{
		filepath.Join(vmDir, snapshotVirtualizationFrameworkDirName, "state.vzvmsave"),
		filepath.Join(vmDir, imageDiskName),
		filepath.Join(vmDir, imageKernelName),
		filepath.Join(vmDir, imageInitramfsName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected restored file %s: %v", path, err)
		}
	}
	sourceKey, err := os.ReadFile(filepath.Join(snapshotDir, vmSSHPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	vmKey, err := os.ReadFile(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if string(vmKey) != string(sourceKey) {
		t.Fatal("CreateFromSnapshot did not copy snapshot SSH key")
	}
	var config vz.RunnerConfig
	if err := readJSON(filepath.Join(vmDir, vmConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if config.RestoreStatePath != metadata.RestoreStatePath {
		t.Fatalf("config.RestoreStatePath = %q, want %q", config.RestoreStatePath, metadata.RestoreStatePath)
	}
	if config.MachineIdentifier == "" {
		t.Fatal("config.MachineIdentifier is empty")
	}
	if config.NetworkMAC != testNetworkMAC {
		t.Fatalf("config.NetworkMAC = %q, want %q", config.NetworkMAC, testNetworkMAC)
	}
}

func TestCreateFromSnapshotCopiesKindReadyArtifacts(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := writeTestVirtualizationFrameworkSnapshot(t, manager, "kind-ready")
	markTestSnapshotKindReady(t, snapshotDir)

	if err := manager.CreateFromSnapshot(context.Background(), "work", "kind-ready", ""); err != nil {
		t.Fatalf("CreateFromSnapshot returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "work")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.KindReady {
		t.Fatalf("metadata.KindReady = false, metadata = %#v", metadata)
	}
	if metadata.KubeconfigPath != filepath.Join(vmDir, "kubeconfig") {
		t.Fatalf("metadata.KubeconfigPath = %q", metadata.KubeconfigPath)
	}
	for _, name := range []string{
		filepath.Join(snapshotKindDirName, snapshotKindMetadataName),
		filepath.Join(snapshotKindDirName, snapshotKindKubeconfigTemplateName),
	} {
		if _, err := os.Stat(filepath.Join(vmDir, name)); err != nil {
			t.Fatalf("expected copied kind artifact %s: %v", name, err)
		}
	}
}

func TestCreateFromSnapshotCopiesCloudHypervisorArtifacts(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := writeTestCloudHypervisorSnapshot(t, manager, "prepared")
	var snapshot spindsnapshot.Metadata
	if err := readJSON(filepath.Join(snapshotDir, snapshotMetadataName), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Image = "docker"
	if err := writeJSON(filepath.Join(snapshotDir, snapshotMetadataName), snapshot, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := manager.CreateFromSnapshot(context.Background(), "worker", "prepared", ""); err != nil {
		t.Fatalf("CreateFromSnapshot returned error: %v", err)
	}

	vmDir := filepath.Join(manager.VMStore, "worker")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	if !metadata.FromSnapshot || metadata.SourceSnapshot != "prepared" {
		t.Fatalf("metadata snapshot fields = %#v", metadata)
	}
	if metadata.Backend != BackendCloudHypervisor {
		t.Fatalf("metadata.Backend = %q, want %q", metadata.Backend, BackendCloudHypervisor)
	}
	if metadata.RestoreStatePath != filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName) {
		t.Fatalf("metadata.RestoreStatePath = %q", metadata.RestoreStatePath)
	}
	for _, name := range []string{
		cloudHypervisorSnapshotConfigName,
		cloudHypervisorMemoryRangesName,
		cloudHypervisorStateName,
		imageDiskName,
		imageKernelName,
		imageInitramfsName,
	} {
		if _, err := os.Stat(filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName, name)); err != nil {
			t.Fatalf("expected restored snapshot file %s: %v", name, err)
		}
	}
	for _, name := range []string{imageDiskName, imageKernelName, imageInitramfsName} {
		if _, err := os.Stat(filepath.Join(vmDir, name)); err != nil {
			t.Fatalf("expected VM root snapshot artifact %s: %v", name, err)
		}
	}
	sourceKey, err := os.ReadFile(filepath.Join(snapshotDir, vmSSHPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	vmKey, err := os.ReadFile(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		t.Fatal(err)
	}
	if string(vmKey) != string(sourceKey) {
		t.Fatal("CreateFromSnapshot did not copy snapshot SSH key")
	}
	state, err := readState(vmDir)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "stopped" || state.Backend != BackendCloudHypervisor {
		t.Fatalf("state = %#v, want stopped Cloud Hypervisor", state)
	}

	var config map[string]any
	if err := readJSON(filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName, cloudHypervisorSnapshotConfigName), &config); err != nil {
		t.Fatal(err)
	}
	if got := config["disk"]; got != filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName, imageDiskName) {
		t.Fatalf("rewritten disk path = %#v", got)
	}
	vsockConfig, ok := config["vsock"].(map[string]any)
	if !ok {
		t.Fatalf("rewritten vsock config = %#v", config["vsock"])
	}
	if got := vsockConfig["socket"]; got != filepath.Join(vmDir, "cloud-hypervisor-vsock.sock") {
		t.Fatalf("rewritten vsock socket = %#v", got)
	}
	if got := vsockConfig["vhost_user"]; got != nil {
		t.Fatalf("rewritten vsock vhost_user = %#v, want nil", got)
	}
	if got := config["fs"]; got != nil {
		t.Fatalf("rewritten fs = %#v, want nil", got)
	}
	disksConfig, ok := config["disks"].([]any)
	if !ok || len(disksConfig) != 1 {
		t.Fatalf("rewritten disks config = %#v", config["disks"])
	}
	diskDevice, ok := disksConfig[0].(map[string]any)
	if !ok {
		t.Fatalf("rewritten disk device = %#v", disksConfig[0])
	}
	if got := diskDevice["path"]; got != filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName, imageDiskName) {
		t.Fatalf("rewritten disk device path = %#v", got)
	}
	if got := diskDevice["mac"]; got != nil {
		t.Fatalf("rewritten disk device mac = %#v, want nil", got)
	}
	if got := diskDevice["vhost_user"]; got != false {
		t.Fatalf("rewritten disk device vhost_user = %#v, want false", got)
	}
	if got := diskDevice["vhost_socket"]; got != nil {
		t.Fatalf("rewritten disk device vhost_socket = %#v, want nil", got)
	}
	var vmConfig cloudhypervisor.Config
	if err := readJSON(filepath.Join(vmDir, cloudHypervisorConfigName), &vmConfig); err != nil {
		t.Fatal(err)
	}
	netConfig, ok := config["net"].([]any)
	if !ok || len(netConfig) != 1 {
		t.Fatalf("rewritten net config = %#v", config["net"])
	}
	netDevice, ok := netConfig[0].(map[string]any)
	if !ok {
		t.Fatalf("rewritten net device = %#v", netConfig[0])
	}
	if got := netDevice["tap"]; got != nil {
		t.Fatalf("rewritten tap = %#v, want nil", got)
	}
	if got := netDevice["mac"]; got != vmConfig.NetMAC {
		t.Fatalf("rewritten mac = %#v, want %q", got, vmConfig.NetMAC)
	}
	if got := netDevice["vhost_user"]; got != true {
		t.Fatalf("rewritten vhost_user = %#v, want true", got)
	}
	if got := netDevice["socket"]; got != nil {
		t.Fatalf("rewritten socket = %#v, want nil", got)
	}
	if got := netDevice["vhost_socket"]; got != vmConfig.NetSocketPath {
		t.Fatalf("rewritten vhost_socket = %#v, want %q", got, vmConfig.NetSocketPath)
	}
	if got := netDevice["vhost_mode"]; got != "Client" {
		t.Fatalf("rewritten vhost_mode = %#v, want Client", got)
	}
	if got := netDevice["host_mac"]; got != nil {
		t.Fatalf("rewritten host_mac = %#v, want nil", got)
	}

	if err := manager.DeleteSnapshot("prepared"); err != nil {
		t.Fatalf("DeleteSnapshot returned error: %v", err)
	}
	for _, name := range []string{
		cloudHypervisorSnapshotConfigName,
		cloudHypervisorMemoryRangesName,
		cloudHypervisorStateName,
		imageDiskName,
		imageKernelName,
		imageInitramfsName,
	} {
		if _, err := os.Stat(filepath.Join(vmDir, vmCloudHypervisorSnapshotDirName, name)); err != nil {
			t.Fatalf("expected restored snapshot file after snapshot delete %s: %v", name, err)
		}
	}
}

func TestCreateFromSnapshotRejectsBackendOverride(t *testing.T) {
	manager := newTestManager(t)
	writeTestCloudHypervisorSnapshot(t, manager, "prepared")

	if err := manager.CreateFromSnapshot(context.Background(), "worker", "prepared", BackendVirtualizationFramework); err == nil {
		t.Fatal("CreateFromSnapshot returned nil error")
	}
}

func TestSnapshotCreateRejectsStoppedVM(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{Name: "base", Backend: BackendCloudHypervisor}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendCloudHypervisor, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	if err := manager.SnapshotCreate(context.Background(), "prepared", "base"); err == nil {
		t.Fatal("SnapshotCreate returned nil error")
	}
}

func TestListSnapshotsAndDeleteSnapshot(t *testing.T) {
	manager := newTestManager(t)
	writeTestVirtualizationFrameworkSnapshot(t, manager, "prepared")
	writeTestCloudHypervisorSnapshot(t, manager, "linux-prepared")

	snapshots, err := manager.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("len(snapshots) = %d, want 2", len(snapshots))
	}
	if snapshots[0].Name != "linux-prepared" || snapshots[0].Backend != BackendCloudHypervisor {
		t.Fatalf("snapshots[0] = %#v", snapshots[0])
	}
	if snapshots[1].Name != "prepared" || snapshots[1].Backend != BackendVirtualizationFramework {
		t.Fatalf("snapshots[1] = %#v", snapshots[1])
	}
	for _, snapshot := range snapshots {
		if snapshot.Health != "ok" {
			t.Fatalf("snapshot %q Health = %q, want ok: %s", snapshot.Name, snapshot.Health, snapshot.HealthMessage)
		}
		if snapshot.TotalSizeBytes == 0 {
			t.Fatalf("snapshot %q TotalSizeBytes = 0", snapshot.Name)
		}
		if snapshot.DiskSizeBytes == 0 || snapshot.StateSizeBytes == 0 {
			t.Fatalf("snapshot %q sizes = disk %d state %d", snapshot.Name, snapshot.DiskSizeBytes, snapshot.StateSizeBytes)
		}
	}

	info, err := manager.SnapshotInfo("prepared")
	if err != nil {
		t.Fatalf("spindsnapshot.Info returned error: %v", err)
	}
	if info.Name != "prepared" || info.Health != "ok" || len(info.Artifacts) == 0 {
		t.Fatalf("spindsnapshot.Info = %#v, want prepared ok with artifacts", info)
	}

	deletedSize, err := manager.DeleteSnapshotWithSize("prepared")
	if err != nil {
		t.Fatalf("DeleteSnapshotWithSize returned error: %v", err)
	}
	if deletedSize == 0 {
		t.Fatal("DeleteSnapshotWithSize returned size 0")
	}
	if _, err := os.Stat(filepath.Join(manager.SnapshotStore, "prepared")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted snapshot still exists: %v", err)
	}
	if err := manager.DeleteSnapshot("prepared"); err == nil {
		t.Fatal("DeleteSnapshot returned nil error for missing snapshot")
	}
}

func TestDeleteVMDeletesStoppedVM(t *testing.T) {
	manager := newTestManager(t)
	vmDir := writeTestStoppedVM(t, manager, "delete-me")
	if err := os.WriteFile(filepath.Join(vmDir, "marker"), []byte("delete me"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := manager.DeleteVM(context.Background(), "delete-me", spindvm.DeleteOptions{})
	if err != nil {
		t.Fatalf("DeleteVM returned error: %v", err)
	}
	if result.SizeBytes == 0 {
		t.Fatal("DeleteVM returned size 0")
	}
	if _, err := os.Stat(vmDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted VM directory still exists: %v", err)
	}
}

func TestDeleteVMRejectsRunningVMWithoutForce(t *testing.T) {
	manager := newTestManager(t)
	vmDir := writeTestStoppedVM(t, manager, "running")
	if err := writeState(vmDir, spindvm.State{
		Status:    "running",
		Backend:   BackendVirtualizationFramework,
		PID:       os.Getpid(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.DeleteVM(context.Background(), "running", spindvm.DeleteOptions{}); err == nil || !strings.Contains(err.Error(), "is running") {
		t.Fatalf("DeleteVM error = %v, want running VM error", err)
	}
	if _, err := os.Stat(vmDir); err != nil {
		t.Fatalf("running VM directory was removed: %v", err)
	}
}

func TestDeleteVMForceUsesFastCloudHypervisorShutdown(t *testing.T) {
	manager := newTestManager(t)
	vmDir := writeTestStoppedVM(t, manager, "running-cloud-hypervisor")

	cmd := osexec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	apiSocketPath := filepath.Join(vmDir, "cloud-hypervisor-api.sock")
	requests := make(chan string, 4)
	listener, err := net.Listen("unix", apiSocketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- r.URL.Path
			if r.URL.Path == "/api/v1/vmm.shutdown" {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{
		Name:      "running-cloud-hypervisor",
		Backend:   BackendCloudHypervisor,
		CreatedAt: time.Now().UTC(),
	}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{
		Status:                       "running",
		Backend:                      BackendCloudHypervisor,
		PID:                          cmd.Process.Pid,
		CloudHypervisorAPISocketPath: apiSocketPath,
		UpdatedAt:                    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.DeleteVM(context.Background(), "running-cloud-hypervisor", spindvm.DeleteOptions{Force: true}); err != nil {
		t.Fatalf("DeleteVM returned error: %v", err)
	}

	seen := drainRequestPaths(requests)
	if containsString(seen, "/api/v1/vm.shutdown") {
		t.Fatalf("DeleteVM requested graceful VM shutdown: %#v", seen)
	}
	if !containsString(seen, "/api/v1/vmm.shutdown") {
		t.Fatalf("DeleteVM requests = %#v, want VMM shutdown", seen)
	}
}

func TestDeleteVMUnmergesKubeconfig(t *testing.T) {
	manager := newTestManager(t)
	vmDir := writeTestStoppedVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(targetPath, []byte(`apiVersion: v1
kind: Config
clusters:
  - name: spind-work
    cluster:
      server: https://127.0.0.1:49231
users:
  - name: spind-work
    user:
      token: token
contexts:
  - name: spind-work
    context:
      cluster: spind-work
      user: spind-work
current-context: spind-work
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", targetPath)

	result, err := manager.DeleteVM(context.Background(), "work", spindvm.DeleteOptions{UnmergeKubeconfig: true})
	if err != nil {
		t.Fatalf("DeleteVM returned error: %v", err)
	}
	if len(result.KubeconfigRemoved) != 1 || result.KubeconfigRemoved[0] != targetPath {
		t.Fatalf("KubeconfigRemoved = %#v, want %s", result.KubeconfigRemoved, targetPath)
	}
	doc, err := kubeconfig.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if kubeconfig.HasEntry(doc, "spind-work") {
		t.Fatalf("spind-work entry remained after DeleteVM: %#v", doc)
	}
	if _, err := os.Stat(vmDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted VM directory still exists: %v", err)
	}
}

func TestListImages(t *testing.T) {
	manager := newTestManager(t)
	createdAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	writeTinyTestImage(t, manager, "docker", createdAt.Add(time.Hour))
	writeTinyTestImage(t, manager, "base", createdAt)

	images, err := manager.ListImages()
	if err != nil {
		t.Fatalf("ListImages returned error: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("len(images) = %d, want 2: %#v", len(images), images)
	}
	if images[0].Name != "base" || images[1].Name != "docker" {
		t.Fatalf("image order = %#v, want base/docker", images)
	}
	if images[0].Architecture != runtime.GOARCH || images[0].ExecUser != defaultExecUser || !images[0].CreatedAt.Equal(createdAt) {
		t.Fatalf("first image metadata = %#v", images[0])
	}
	if images[0].Health != "ok" || images[0].SizeBytes == 0 || images[0].ImageDir == "" {
		t.Fatalf("first image health/size/path = %#v", images[0])
	}
}

func TestBuildImageMaterializesDockerTemplateAndInstallsImage(t *testing.T) {
	manager := newTestManager(t)
	sourceRoot := t.TempDir()
	manager.BuiltinTemplateSource = sourceRoot
	if err := os.WriteFile(filepath.Join(sourceRoot, "flake.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "flake.lock"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(sourceRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range spindimage.GuestBinaryNames {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installFakeImageBuildTools(t)

	result, err := manager.BuildImage(context.Background(), "docker", spindimage.BuildOptions{DataSize: "1MiB"})
	if err != nil {
		t.Fatalf("BuildImage returned error: %v", err)
	}
	if result.Name != "docker" || result.SizeBytes == 0 || result.ImageDir == "" || result.TemplatePath == "" {
		t.Fatalf("BuildImage result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(manager.TemplateStore, "docker", "flake.nix")); err != nil {
		t.Fatalf("expected materialized template: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.TemplateStore, "docker", "template.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy template.json should not be materialized: %v", err)
	}
	var metadata spindimage.Metadata
	if err := readJSON(filepath.Join(manager.ImageStore, "docker", imageMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "docker" || metadata.ImageType != "microvm-nix" || metadata.CreatedAt.IsZero() {
		t.Fatalf("metadata = %#v", metadata)
	}
	dataInfo, err := os.Stat(filepath.Join(manager.ImageStore, "docker", "docker-data.img"))
	if err != nil {
		t.Fatal(err)
	}
	if dataInfo.Size() != 1024*1024 {
		t.Fatalf("docker-data.img size = %d, want 1MiB", dataInfo.Size())
	}
}

func TestBuildImageRefreshesStaleDockerTemplate(t *testing.T) {
	manager := newTestManager(t)
	sourceRoot := t.TempDir()
	manager.BuiltinTemplateSource = sourceRoot
	if err := os.WriteFile(filepath.Join(sourceRoot, "flake.nix"), []byte("new template\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "flake.lock"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(sourceRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range spindimage.GuestBinaryNames {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	staleTemplateDir := filepath.Join(manager.TemplateStore, "docker")
	if err := os.MkdirAll(filepath.Join(staleTemplateDir, "guest-binaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleTemplateDir, "flake.nix"), []byte("old template\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleTemplateDir, "template.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleTemplateDir, "guest-binaries", "spind-vsock-ssh-proxy"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeImageBuildTools(t)

	if _, err := manager.BuildImage(context.Background(), "docker", spindimage.BuildOptions{}); err != nil {
		t.Fatalf("BuildImage returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staleTemplateDir, "guest-binaries", "spind-guest-agent")); err != nil {
		t.Fatalf("expected refreshed guest agent: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staleTemplateDir, "template.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale template.json should be removed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(staleTemplateDir, "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new template\n" {
		t.Fatalf("flake.nix was not refreshed: %q", string(data))
	}
}

func TestBuildImageRejectsExistingImageWithoutForce(t *testing.T) {
	manager := newTestManager(t)
	writeTinyTestImage(t, manager, "docker", time.Now().UTC())
	if _, err := manager.BuildImage(context.Background(), "docker", spindimage.BuildOptions{}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("BuildImage error = %v, want already exists", err)
	}
}

func TestDeleteImageDeletesImageDirectory(t *testing.T) {
	manager := newTestManager(t)
	imageDir := writeTinyTestImage(t, manager, "docker", time.Now().UTC())
	if err := os.WriteFile(filepath.Join(imageDir, "marker"), []byte("delete me"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := manager.DeleteImage("docker", spindimage.DeleteOptions{})
	if err != nil {
		t.Fatalf("DeleteImage returned error: %v", err)
	}
	if result.SizeBytes == 0 {
		t.Fatal("DeleteImage returned size 0")
	}
	if _, err := os.Stat(imageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted image directory still exists: %v", err)
	}
}

func TestDeleteImageRejectsVMReferenceWithoutForce(t *testing.T) {
	manager := newTestManager(t)
	imageDir := writeTinyTestImage(t, manager, "docker", time.Now().UTC())
	vmDir := writeTestStoppedVM(t, manager, "work")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Image = "docker"
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.DeleteImage("docker", spindimage.DeleteOptions{}); err == nil || !strings.Contains(err.Error(), "referenced by VM") {
		t.Fatalf("DeleteImage error = %v, want VM reference error", err)
	}
	if _, err := os.Stat(imageDir); err != nil {
		t.Fatalf("referenced image directory was removed: %v", err)
	}
}

func TestDeleteImageForceDeletesReferencedImage(t *testing.T) {
	manager := newTestManager(t)
	imageDir := writeTinyTestImage(t, manager, "docker", time.Now().UTC())
	vmDir := writeTestStoppedVM(t, manager, "work")
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Image = "docker"
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := manager.DeleteImage("docker", spindimage.DeleteOptions{Force: true})
	if err != nil {
		t.Fatalf("DeleteImage returned error: %v", err)
	}
	if len(result.ReferencedVMs) != 1 || result.ReferencedVMs[0] != "work" {
		t.Fatalf("ReferencedVMs = %#v, want work", result.ReferencedVMs)
	}
	if _, err := os.Stat(imageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted image directory still exists: %v", err)
	}
	if _, err := os.Stat(vmDir); err != nil {
		t.Fatalf("referencing VM directory was removed: %v", err)
	}
}

func TestListSnapshotsReportsBrokenMetadataHealth(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := filepath.Join(manager.SnapshotStore, "broken")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotDir, snapshotMetadataName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshots, err := manager.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("len(snapshots) = %d, want 1", len(snapshots))
	}
	if snapshots[0].Name != "broken" || snapshots[0].Health != "invalid-metadata" || snapshots[0].HealthMessage == "" {
		t.Fatalf("snapshot = %#v, want invalid-metadata health", snapshots[0])
	}
}

func TestSnapshotInfoReportsMissingArtifactHealth(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := writeTestVirtualizationFrameworkSnapshot(t, manager, "prepared")
	if err := os.Remove(filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageDiskName)); err != nil {
		t.Fatal(err)
	}

	info, err := manager.SnapshotInfo("prepared")
	if err != nil {
		t.Fatalf("spindsnapshot.Info returned error: %v", err)
	}
	if info.Health != "missing-artifact" || !strings.Contains(info.HealthMessage, imageDiskName) {
		t.Fatalf("spindsnapshot.Info = %#v, want missing artifact health", info)
	}
}

func TestSnapshotInfoReportsKindReadyMetadata(t *testing.T) {
	manager := newTestManager(t)
	snapshotDir := writeTestVirtualizationFrameworkSnapshot(t, manager, "kind-ready")
	markTestSnapshotKindReady(t, snapshotDir)

	info, err := manager.SnapshotInfo("kind-ready")
	if err != nil {
		t.Fatalf("spindsnapshot.Info returned error: %v", err)
	}
	if !info.KindReady || !info.KindTemplate {
		t.Fatalf("spindsnapshot.Info kind fields = %#v", info)
	}
	if info.KindMetadata.SourceContext != "kind-dev" || info.KindMetadata.SourceCluster != "kind-dev" {
		t.Fatalf("KindMetadata = %#v", info.KindMetadata)
	}
	foundTemplate := false
	for _, artifact := range info.Artifacts {
		if artifact.Name == filepath.Join(snapshotKindDirName, snapshotKindKubeconfigTemplateName) && artifact.Found {
			foundTemplate = true
		}
	}
	if !foundTemplate {
		t.Fatalf("kind kubeconfig template artifact not found: %#v", info.Artifacts)
	}
}

func TestGenerateVMKubeconfigRewritesServerAndNames(t *testing.T) {
	vmDir := t.TempDir()
	kindDir := filepath.Join(vmDir, snapshotKindDirName)
	if err := os.MkdirAll(kindDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := []byte(`{
  "apiVersion": "v1",
  "kind": "Config",
  "clusters": [{"name": "kind-dev", "cluster": {"server": "https://127.0.0.1:40123", "certificate-authority-data": "ca"}}],
  "users": [{"name": "kind-dev", "user": {"token": "token"}}],
  "contexts": [{"name": "kind-dev", "context": {"cluster": "kind-dev", "user": "kind-dev"}}],
  "current-context": "kind-dev"
}
`)
	if err := os.WriteFile(filepath.Join(kindDir, snapshotKindKubeconfigTemplateName), template, 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(vmDir, "kubeconfig")
	if err := spindkind.GenerateKubeconfig(vmDir, "work", "https://127.0.0.1:49231", outputPath); err != nil {
		t.Fatalf("GenerateKubeconfig returned error: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := kubeconfig.ParseJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.CurrentContext != "spind-work" {
		t.Fatalf("CurrentContext = %q, want spind-work", doc.CurrentContext)
	}
	if len(doc.Clusters) != 1 || doc.Clusters[0].Name != "spind-work" || doc.Clusters[0].Cluster.Server != "https://127.0.0.1:49231" {
		t.Fatalf("Clusters = %#v", doc.Clusters)
	}
	if len(doc.Users) != 1 || doc.Users[0].Name != "spind-work" {
		t.Fatalf("Users = %#v", doc.Users)
	}
	if len(doc.Contexts) != 1 || doc.Contexts[0].Name != "spind-work" || doc.Contexts[0].Context.Cluster != "spind-work" || doc.Contexts[0].Context.User != "spind-work" {
		t.Fatalf("Contexts = %#v", doc.Contexts)
	}
}

func TestGenerateVMKubeconfigIgnoresKUBECONFIGAndKeepsGlobalConfig(t *testing.T) {
	vmDir := t.TempDir()
	kindDir := filepath.Join(vmDir, snapshotKindDirName)
	if err := os.MkdirAll(kindDir, 0o755); err != nil {
		t.Fatal(err)
	}
	template := []byte(`{
  "apiVersion": "v1",
  "kind": "Config",
  "clusters": [{"name": "kind-dev", "cluster": {"server": "https://127.0.0.1:40123"}}],
  "users": [{"name": "kind-dev", "user": {"token": "token"}}],
  "contexts": [{"name": "kind-dev", "context": {"cluster": "kind-dev", "user": "kind-dev"}}],
  "current-context": "kind-dev"
}
`)
	if err := os.WriteFile(filepath.Join(kindDir, snapshotKindKubeconfigTemplateName), template, 0o600); err != nil {
		t.Fatal(err)
	}

	globalKubeconfigPath := filepath.Join(t.TempDir(), "config")
	globalKubeconfig := []byte("apiVersion: v1\nkind: Config\ncurrent-context: existing\n")
	if err := os.WriteFile(globalKubeconfigPath, globalKubeconfig, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", globalKubeconfigPath)

	outputPath := filepath.Join(vmDir, "kubeconfig")
	if err := spindkind.GenerateKubeconfig(vmDir, "work", "https://127.0.0.1:49231", outputPath); err != nil {
		t.Fatalf("GenerateKubeconfig returned error: %v", err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := kubeconfig.ParseJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.CurrentContext != "spind-work" {
		t.Fatalf("CurrentContext = %q, want spind-work", doc.CurrentContext)
	}
	afterGlobal, err := os.ReadFile(globalKubeconfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterGlobal) != string(globalKubeconfig) {
		t.Fatalf("global kubeconfig was modified:\n%s", string(afterGlobal))
	}
}

func TestKubeconfigMergeExplicitPathKeepsCurrentContext(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(targetPath, []byte(`apiVersion: v1
kind: Config
clusters:
  - name: other
    cluster:
      server: https://example.invalid
users:
  - name: other
    user:
      token: other-token
contexts:
  - name: other
    context:
      cluster: other
      user: other
current-context: other
`), 0o600); err != nil {
		t.Fatal(err)
	}

	mergedPath, err := manager.KubeconfigMerge("work", KubeconfigMergeOptions{KubeconfigPath: targetPath})
	if err != nil {
		t.Fatalf("KubeconfigMerge returned error: %v", err)
	}
	if mergedPath != targetPath {
		t.Fatalf("mergedPath = %q, want %q", mergedPath, targetPath)
	}
	doc, err := kubeconfig.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if doc.CurrentContext != "other" {
		t.Fatalf("CurrentContext = %q, want other", doc.CurrentContext)
	}
	if len(kubeconfig.FindClusters(doc, "spind-work")) != 1 || len(kubeconfig.FindUsers(doc, "spind-work")) != 1 || len(kubeconfig.FindContexts(doc, "spind-work")) != 1 {
		t.Fatalf("merged spind-work entry missing: %#v", doc)
	}
	if len(kubeconfig.FindClusters(doc, "other")) != 1 || len(kubeconfig.FindUsers(doc, "other")) != 1 || len(kubeconfig.FindContexts(doc, "other")) != 1 {
		t.Fatalf("existing other entry missing: %#v", doc)
	}
}

func TestKubeconfigMergeRejectsExistingEntryWithoutReplaceAndKeepsFile(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	original := []byte(`apiVersion: v1
kind: Config
clusters:
  - name: spind-work
    cluster:
      server: https://old.invalid
users:
  - name: spind-work
    user:
      token: old-token
contexts:
  - name: spind-work
    context:
      cluster: spind-work
      user: spind-work
current-context: old-current
`)
	if err := os.WriteFile(targetPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.KubeconfigMerge("work", KubeconfigMergeOptions{KubeconfigPath: targetPath}); err == nil {
		t.Fatal("KubeconfigMerge returned nil error for existing entry without replace")
	}
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("target kubeconfig changed after failed merge:\n%s", string(after))
	}
}

func TestKubeconfigMergeReplaceAndSetCurrentContext(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(targetPath, []byte(`apiVersion: v1
kind: Config
clusters:
  - name: spind-work
    cluster:
      server: https://old.invalid
users:
  - name: spind-work
    user:
      token: old-token
contexts:
  - name: spind-work
    context:
      cluster: spind-work
      user: spind-work
current-context: old-current
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.KubeconfigMerge("work", KubeconfigMergeOptions{KubeconfigPath: targetPath, Replace: true, SetCurrentContext: true}); err != nil {
		t.Fatalf("KubeconfigMerge returned error: %v", err)
	}
	doc, err := kubeconfig.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if doc.CurrentContext != "spind-work" {
		t.Fatalf("CurrentContext = %q, want spind-work", doc.CurrentContext)
	}
	clusters := kubeconfig.FindClusters(doc, "spind-work")
	if len(clusters) != 1 || clusters[0].Cluster.Server != "https://127.0.0.1:49231" {
		t.Fatalf("Clusters = %#v", clusters)
	}
}

func TestKubeconfigMergeAndUnmergeKeepUnknownFields(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(targetPath, []byte(`apiVersion: v1
kind: Config
x-top-level: keep-top
clusters:
  - name: other
    x-cluster-entry: keep-cluster-entry
    cluster:
      server: https://example.invalid
      certificate-authority: /tmp/ca.crt
      proxy-url: http://proxy.invalid
      tls-server-name: other.invalid
      x-cluster-ref: keep-cluster-ref
users:
  - name: other
    x-user-entry: keep-user-entry
    user:
      client-certificate: /tmp/client.crt
      client-key: /tmp/client.key
      auth-provider:
        name: oidc
      exec:
        apiVersion: client.authentication.k8s.io/v1
        command: auth-helper
      x-user-ref: keep-user-ref
contexts:
  - name: other
    x-context-entry: keep-context-entry
    context:
      cluster: other
      user: other
      namespace: dev
      x-context-ref: keep-context-ref
current-context: other
`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.KubeconfigMerge("work", KubeconfigMergeOptions{KubeconfigPath: targetPath}); err != nil {
		t.Fatalf("KubeconfigMerge returned error: %v", err)
	}
	assertOtherKubeconfigFields(t, targetPath)

	if _, err := manager.KubeconfigUnmerge("work", KubeconfigUnmergeOptions{KubeconfigPath: targetPath}); err != nil {
		t.Fatalf("KubeconfigUnmerge returned error: %v", err)
	}
	doc, err := kubeconfig.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if kubeconfig.HasEntry(doc, "spind-work") {
		t.Fatalf("spind-work entry remained after unmerge: %#v", doc)
	}
	assertOtherKubeconfigFields(t, targetPath)
}

func TestKubeconfigCandidatePathsAndPathForMerge(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	third := filepath.Join(dir, "third")
	if err := os.WriteFile(second, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBECONFIG", strings.Join([]string{"", first, second, first, third}, string(os.PathListSeparator)))

	paths, err := kubeconfig.CandidatePaths("")
	if err != nil {
		t.Fatalf("kubeconfigCandidatePaths returned error: %v", err)
	}
	want := []string{first, second, third}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	mergePath, err := kubeconfig.PathForMerge(paths)
	if err != nil {
		t.Fatalf("kubeconfigPathForMerge returned error: %v", err)
	}
	if mergePath != second {
		t.Fatalf("mergePath = %q, want %q", mergePath, second)
	}

	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	mergePath, err = kubeconfig.PathForMerge(paths)
	if err != nil {
		t.Fatalf("kubeconfigPathForMerge returned error: %v", err)
	}
	if mergePath != third {
		t.Fatalf("mergePath = %q, want %q", mergePath, third)
	}
}

func TestKubeconfigCandidatePathsKeepsKUBECONFIGPathStrings(t *testing.T) {
	t.Setenv("KUBECONFIG", strings.Join([]string{"./config", "a/../b", "~/config", "./config"}, string(os.PathListSeparator)))

	paths, err := kubeconfig.CandidatePaths("")
	if err != nil {
		t.Fatalf("kubeconfigCandidatePaths returned error: %v", err)
	}
	want := []string{"./config", "a/../b", "~/config"}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestKubeconfigCandidatePathsUsesDefaultAndSingleKUBECONFIG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	paths, err := kubeconfig.CandidatePaths("")
	if err != nil {
		t.Fatalf("kubeconfigCandidatePaths returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != filepath.Join(home, ".kube", "config") {
		t.Fatalf("paths = %#v", paths)
	}

	single := filepath.Join(t.TempDir(), "single")
	t.Setenv("KUBECONFIG", single)
	paths, err = kubeconfig.CandidatePaths("")
	if err != nil {
		t.Fatalf("kubeconfigCandidatePaths returned error: %v", err)
	}
	if len(paths) != 1 || paths[0] != single {
		t.Fatalf("paths = %#v, want %q", paths, single)
	}
}

func TestKubeconfigUnmergeRemovesEntryFromAllCandidatePaths(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
clusters:
  - name: spind-work
    cluster:
      server: https://127.0.0.1:49231
  - name: other
    cluster:
      server: https://example.invalid
users:
  - name: spind-work
    user:
      token: token
  - name: other
    user:
      token: other-token
contexts:
  - name: spind-work
    context:
      cluster: spind-work
      user: spind-work
  - name: other
    context:
      cluster: other
      user: other
current-context: spind-work
`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBECONFIG", strings.Join([]string{first, second}, string(os.PathListSeparator)))

	changed, err := manager.KubeconfigUnmerge("work", KubeconfigUnmergeOptions{})
	if err != nil {
		t.Fatalf("KubeconfigUnmerge returned error: %v", err)
	}
	if strings.Join(changed, "\n") != strings.Join([]string{first, second}, "\n") {
		t.Fatalf("changed = %#v", changed)
	}
	for _, path := range []string{first, second} {
		doc, err := kubeconfig.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if kubeconfig.HasEntry(doc, "spind-work") {
			t.Fatalf("%s still has spind-work entry: %#v", path, doc)
		}
		if doc.CurrentContext != "" {
			t.Fatalf("%s CurrentContext = %q, want empty", path, doc.CurrentContext)
		}
		if len(kubeconfig.FindClusters(doc, "other")) != 1 || len(kubeconfig.FindUsers(doc, "other")) != 1 || len(kubeconfig.FindContexts(doc, "other")) != 1 {
			t.Fatalf("%s lost other entry: %#v", path, doc)
		}
	}
}

func TestKubeconfigUnmergeRejectsEntryReferencedByOtherContext(t *testing.T) {
	manager := newTestManager(t)
	writeTestRunningKindReadyVM(t, manager, "work")
	targetPath := filepath.Join(t.TempDir(), "config")
	original := []byte(`apiVersion: v1
kind: Config
clusters:
  - name: spind-work
    cluster:
      server: https://127.0.0.1:49231
users:
  - name: spind-work
    user:
      token: token
contexts:
  - name: other
    context:
      cluster: spind-work
      user: spind-work
current-context: other
`)
	if err := os.WriteFile(targetPath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.KubeconfigUnmerge("work", KubeconfigUnmergeOptions{KubeconfigPath: targetPath}); err == nil {
		t.Fatal("KubeconfigUnmerge returned nil error for referenced entry")
	}
	after, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("target kubeconfig changed after failed unmerge:\n%s", string(after))
	}
}

func TestPruneSnapshotsFiltersAndDeletes(t *testing.T) {
	manager := newTestManager(t)
	oldDir := writeTestVirtualizationFrameworkSnapshot(t, manager, "old")
	recentDir := writeTestCloudHypervisorSnapshot(t, manager, "recent")
	oldMetadata := spindsnapshot.Metadata{}
	if err := readJSON(filepath.Join(oldDir, snapshotMetadataName), &oldMetadata); err != nil {
		t.Fatal(err)
	}
	oldMetadata.CreatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := writeJSON(filepath.Join(oldDir, snapshotMetadataName), oldMetadata, 0o644); err != nil {
		t.Fatal(err)
	}
	recentMetadata := spindsnapshot.Metadata{}
	if err := readJSON(filepath.Join(recentDir, snapshotMetadataName), &recentMetadata); err != nil {
		t.Fatal(err)
	}
	recentMetadata.CreatedAt = time.Now().UTC()
	if err := writeJSON(filepath.Join(recentDir, snapshotMetadataName), recentMetadata, 0o644); err != nil {
		t.Fatal(err)
	}

	targets, total, err := manager.PruneSnapshots(24*time.Hour, "", true)
	if err != nil {
		t.Fatalf("PruneSnapshots dry run returned error: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "old" || total == 0 {
		t.Fatalf("dry-run targets=%#v total=%d, want old with size", targets, total)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatalf("dry run deleted old snapshot: %v", err)
	}

	targets, total, err = manager.PruneSnapshots(24*time.Hour, "", false)
	if err != nil {
		t.Fatalf("PruneSnapshots returned error: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "old" || total == 0 {
		t.Fatalf("delete targets=%#v total=%d, want old with size", targets, total)
	}
	if _, err := os.Stat(oldDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old snapshot still exists: %v", err)
	}
	if _, err := os.Stat(recentDir); err != nil {
		t.Fatalf("recent snapshot missing: %v", err)
	}
}

func TestVMStatusAndListVMs(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindvm.Metadata{
		Name:           "base",
		Backend:        BackendCloudHypervisor,
		Image:          "docker",
		FromSnapshot:   true,
		SourceSnapshot: "prepared",
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().UTC().Add(-2 * time.Second)
	updatedAt := time.Now().UTC()
	state := spindvm.State{
		Status:              "running",
		Backend:             BackendCloudHypervisor,
		PID:                 os.Getpid(),
		ExecReady:           true,
		StartedAt:           startedAt,
		UpdatedAt:           updatedAt,
		LastStartDurationMS: 1234,
		VMMLogPath:          filepath.Join(vmDir, "cloud-hypervisor.log"),
		DockerSocketPath:    filepath.Join(vmDir, "docker.sock"),
		DockerEndpointURI:   "unix://" + filepath.Join(vmDir, "docker.sock"),
		DockerRelayPID:      os.Getpid(),
		DockerGuestPort:     defaultDockerPort,
		DockerAPIReady:      true,
		DockerAvailable:     true,
	}
	if err := writeState(vmDir, state); err != nil {
		t.Fatal(err)
	}

	info, err := manager.VMStatus("base")
	if err != nil {
		t.Fatalf("VMStatus returned error: %v", err)
	}
	if info.Name != "base" || info.Status != "running" || info.Backend != BackendCloudHypervisor || !info.FromSnapshot || info.SourceSnapshot != "prepared" {
		t.Fatalf("info = %#v", info)
	}
	if info.LastStartDuration != 1234*time.Millisecond {
		t.Fatalf("LastStartDuration = %s, want 1.234s", info.LastStartDuration)
	}
	if !info.DockerAvailable || !info.DockerAPIReady || info.DockerEndpointURI == "" || info.DockerGuestPort != defaultDockerPort {
		t.Fatalf("Docker status = %#v", info)
	}
	if info.DockerAPISupport != capabilitySupported || info.DockerAPIStatus != capabilityReady {
		t.Fatalf("Docker API capability = %s/%s, want supported/ready", info.DockerAPISupport, info.DockerAPIStatus)
	}
	if info.HostShareSupport != capabilityUnsupported || info.HostShareStatus != capabilityNotApplicable {
		t.Fatalf("host share capability = %s/%s, want unsupported/not-applicable", info.HostShareSupport, info.HostShareStatus)
	}
	if info.HostShareStatusReason != "cloud-hypervisor snapshot restore prioritizes saved-state restore speed" {
		t.Fatalf("HostShareStatusReason = %q", info.HostShareStatusReason)
	}
	if len(info.LogPaths) == 0 {
		t.Fatal("LogPaths is empty")
	}

	vms, err := manager.ListVMs()
	if err != nil {
		t.Fatalf("ListVMs returned error: %v", err)
	}
	if len(vms) != 1 || vms[0].Name != "base" {
		t.Fatalf("vms = %#v", vms)
	}
}

func TestVMStatusRepairsDeadRunningProcessForDisplay(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{Name: "base"}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{Status: "running", PID: -1, ExecReady: true, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	info, err := manager.VMStatus("base")
	if err != nil {
		t.Fatalf("VMStatus returned error: %v", err)
	}
	if info.Status != "stopped" || info.ExecReady {
		t.Fatalf("info = %#v, want stopped and execReady=false", info)
	}
}

func TestSnapshotCreateVirtualizationFrameworkSavesArtifactsAndStopsSource(t *testing.T) {
	manager := newTestManager(t)
	manager.RunnerPath = writeFakeRunner(t)
	manager.skipVMStartRequirements = true
	writeTestImage(t, manager.ImageStore, "legacy")
	if err := manager.Create(context.Background(), "base", "legacy", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	started, err := manager.Start(context.Background(), "base")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !started {
		t.Fatal("Start returned started=false")
	}

	if err := manager.SnapshotCreate(context.Background(), "prepared", "base"); err != nil {
		t.Fatalf("SnapshotCreate returned error: %v", err)
	}

	snapshotDir := filepath.Join(manager.SnapshotStore, "prepared")
	for _, path := range []string{
		filepath.Join(snapshotDir, snapshotMetadataName),
		filepath.Join(snapshotDir, vmSSHPrivateKeyName),
		filepath.Join(snapshotDir, vmSSHPublicKeyName),
		filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, "state.vzvmsave"),
		filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageDiskName),
		filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageKernelName),
		filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName, imageInitramfsName),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected snapshot file %s: %v", path, err)
		}
	}
	state, err := readState(filepath.Join(manager.VMStore, "base"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "stopped" || state.Backend != BackendVirtualizationFramework {
		t.Fatalf("source state = %#v, want stopped Virtualization.framework", state)
	}
}

func TestCreateRejectsExistingVM(t *testing.T) {
	manager := newTestManager(t)
	writeTestImage(t, manager.ImageStore, "legacy")

	if err := manager.Create(context.Background(), "base", "legacy", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := manager.Create(context.Background(), "base", "legacy", BackendVirtualizationFramework); err == nil {
		t.Fatal("Create returned nil error for duplicate VM")
	}
}

func TestStartAndStopUseRunnerAndState(t *testing.T) {
	manager := newTestManager(t)
	manager.RunnerPath = writeFakeRunner(t)
	manager.skipVMStartRequirements = true
	writeTestImage(t, manager.ImageStore, "legacy")
	if err := manager.Create(context.Background(), "base", "legacy", BackendVirtualizationFramework); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	started, err := manager.Start(context.Background(), "base")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !started {
		t.Fatal("Start returned started=false")
	}

	started, err = manager.Start(context.Background(), "base")
	if err != nil {
		t.Fatalf("second Start returned error: %v", err)
	}
	if started {
		t.Fatal("second Start returned started=true")
	}

	stopped, err := manager.Stop(context.Background(), "base")
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if !stopped {
		t.Fatal("Stop returned stopped=false")
	}

	stopped, err = manager.Stop(context.Background(), "base")
	if err != nil {
		t.Fatalf("second Stop returned error: %v", err)
	}
	if stopped {
		t.Fatal("second Stop returned stopped=true")
	}
}

func TestStopCleansDefaultDockerSocketWhenStateDoesNotTrackIt(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{Name: "base", Backend: BackendVirtualizationFramework}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{Status: "stopped", Backend: BackendVirtualizationFramework, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(vmDir, "docker.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	stopped, err := manager.Stop(context.Background(), "base")
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if stopped {
		t.Fatal("Stop returned true for already stopped VM")
	}
	if _, err := os.Stat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("docker socket cleanup error = %v, want not exist", err)
	}
}

func TestStartVirtualizationFrameworkRestoreUsesRunnerLifecycle(t *testing.T) {
	manager := newTestManager(t)
	manager.RunnerPath = writeFakeRunner(t)
	manager.skipVMStartRequirements = true
	writeTestVirtualizationFrameworkSnapshot(t, manager, "prepared")
	if err := manager.CreateFromSnapshot(context.Background(), "worker", "prepared", ""); err != nil {
		t.Fatalf("CreateFromSnapshot returned error: %v", err)
	}

	started, err := manager.Start(context.Background(), "worker")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !started {
		t.Fatal("Start returned started=false")
	}
	state, err := readState(filepath.Join(manager.VMStore, "worker"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "running" || !state.ExecReady || state.ControlSocketPath == "" {
		t.Fatalf("state = %#v, want running exec ready with control socket", state)
	}
	if stopped, err := manager.Stop(context.Background(), "worker"); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	} else if !stopped {
		t.Fatal("Stop returned stopped=false")
	}
}

func TestExecTransfersStreamsAndExitCode(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{Name: "base"}, 0o644); err != nil {
		t.Fatal(err)
	}
	privateKey, err := generateSSHPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, vmSSHPrivateKeyName), privateKey, 0o600); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("spind-exec-test-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() {
		_ = os.Remove(socketPath)
	})
	if err := writeState(vmDir, spindvm.State{
		Status:         "running",
		PID:            os.Getpid(),
		ExecSocketPath: socketPath,
		ExecPort:       defaultExecPort,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	done := serveSSHExec(t, socketPath)
	var stdout, stderr strings.Builder
	exitCode, err := manager.Exec(context.Background(), "base", []string{"sh", "-lc", "echo"}, strings.NewReader("input"), &stdout, &stderr)
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}
	<-done
	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	if stdout.String() != "out\n" {
		t.Fatalf("stdout = %q, want out", stdout.String())
	}
	if stderr.String() != "err\n" {
		t.Fatalf("stderr = %q, want err", stderr.String())
	}
}

func TestExecFailsForStoppedVM(t *testing.T) {
	manager := newTestManager(t)
	vmDir := filepath.Join(manager.VMStore, "base")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), spindvm.Metadata{Name: "base"}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{Status: "stopped", UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Exec(context.Background(), "base", []string{"uname"}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("Exec returned nil error for stopped VM")
	}
}

func TestUnknownVMFails(t *testing.T) {
	manager := newTestManager(t)

	if _, err := manager.Start(context.Background(), "missing"); err == nil {
		t.Fatal("Start returned nil error for missing VM")
	}
	if _, err := manager.Stop(context.Background(), "missing"); err == nil {
		t.Fatal("Stop returned nil error for missing VM")
	}
}

func TestFakeRunnerProcess(t *testing.T) {
	if os.Getenv("SPIND_FAKE_RUNNER") != "1" {
		return
	}
	code := fakeRunnerMain()
	os.Exit(code)
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "spind-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(home)
	})
	return &Manager{
		Home:          home,
		ImageStore:    filepath.Join(home, "images"),
		TemplateStore: filepath.Join(home, "templates"),
		VMStore:       filepath.Join(home, "vms"),
		SnapshotStore: filepath.Join(home, "snapshots"),
	}
}

func writeTestRunningKindReadyVM(t *testing.T, manager *Manager, name string) string {
	t.Helper()
	vmDir := filepath.Join(manager.VMStore, name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kubeconfigPath := filepath.Join(vmDir, "kubeconfig")
	metadata := spindvm.Metadata{
		Name:           name,
		Backend:        BackendVirtualizationFramework,
		KindReady:      true,
		KubeconfigPath: kubeconfigPath,
		CreatedAt:      time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{
		Status:                   "running",
		Backend:                  BackendVirtualizationFramework,
		PID:                      os.Getpid(),
		KubernetesReady:          true,
		KubernetesKubeconfigPath: kubeconfigPath,
		KubernetesContext:        "spind-" + name,
		UpdatedAt:                time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: spind-%[1]s
    cluster:
      server: https://127.0.0.1:49231
users:
  - name: spind-%[1]s
    user:
      token: token
contexts:
  - name: spind-%[1]s
    context:
      cluster: spind-%[1]s
      user: spind-%[1]s
current-context: spind-%[1]s
`, name)
	if err := os.WriteFile(kubeconfigPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return vmDir
}

func writeTestStoppedVM(t *testing.T, manager *Manager, name string) string {
	t.Helper()
	vmDir := filepath.Join(manager.VMStore, name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindvm.Metadata{
		Name:      name,
		Backend:   BackendVirtualizationFramework,
		CreatedAt: time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(vmDir, vmMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeState(vmDir, spindvm.State{
		Status:    "stopped",
		Backend:   BackendVirtualizationFramework,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	return vmDir
}

func assertOtherKubeconfigFields(t *testing.T, path string) {
	t.Helper()
	doc, err := kubeconfig.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.OtherFields["x-top-level"] != "keep-top" {
		t.Fatalf("top-level unknown fields = %#v", doc.OtherFields)
	}
	clusters := kubeconfig.FindClusters(doc, "other")
	if len(clusters) != 1 {
		t.Fatalf("other cluster missing: %#v", doc.Clusters)
	}
	if clusters[0].OtherFields["x-cluster-entry"] != "keep-cluster-entry" {
		t.Fatalf("cluster entry unknown fields = %#v", clusters[0].OtherFields)
	}
	for key, want := range map[string]string{
		"certificate-authority": "/tmp/ca.crt",
		"proxy-url":             "http://proxy.invalid",
		"tls-server-name":       "other.invalid",
		"x-cluster-ref":         "keep-cluster-ref",
	} {
		if clusters[0].Cluster.OtherFields[key] != want {
			t.Fatalf("cluster ref field %q = %#v, want %q", key, clusters[0].Cluster.OtherFields[key], want)
		}
	}
	users := kubeconfig.FindUsers(doc, "other")
	if len(users) != 1 {
		t.Fatalf("other user missing: %#v", doc.Users)
	}
	if users[0].OtherFields["x-user-entry"] != "keep-user-entry" {
		t.Fatalf("user entry unknown fields = %#v", users[0].OtherFields)
	}
	for _, key := range []string{"client-certificate", "client-key", "auth-provider", "exec", "x-user-ref"} {
		if _, ok := users[0].User[key]; !ok {
			t.Fatalf("user field %q missing: %#v", key, users[0].User)
		}
	}
	contexts := kubeconfig.FindContexts(doc, "other")
	if len(contexts) != 1 {
		t.Fatalf("other context missing: %#v", doc.Contexts)
	}
	if contexts[0].OtherFields["x-context-entry"] != "keep-context-entry" {
		t.Fatalf("context entry unknown fields = %#v", contexts[0].OtherFields)
	}
	for key, want := range map[string]string{
		"namespace":     "dev",
		"x-context-ref": "keep-context-ref",
	} {
		if contexts[0].Context.OtherFields[key] != want {
			t.Fatalf("context ref field %q = %#v, want %q", key, contexts[0].Context.OtherFields[key], want)
		}
	}
}

func writeTestCloudHypervisorSnapshot(t *testing.T, manager *Manager, name string) string {
	t.Helper()
	snapshotDir := filepath.Join(manager.SnapshotStore, name)
	chDir := filepath.Join(snapshotDir, snapshotCloudHypervisorDirName)
	if err := os.MkdirAll(chDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindsnapshot.Metadata{
		Name:              name,
		SourceVM:          "base",
		Image:             "legacy",
		CreatedAt:         time.Now().UTC(),
		Backend:           BackendCloudHypervisor,
		Architecture:      runtime.GOARCH,
		ExecUser:          defaultExecUser,
		CPUCount:          4,
		MemoryMiB:         2048,
		ExecPort:          12345,
		KernelCommandLine: "console=ttyS0 root=/dev/vda1 rw",
	}
	if err := writeJSON(filepath.Join(snapshotDir, snapshotMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{vmSSHPrivateKeyName, []byte("private-key")},
		{vmSSHPublicKeyName, []byte("public-key")},
		{cloudHypervisorMemoryRangesName, []byte("memory")},
		{cloudHypervisorStateName, []byte("state")},
		{imageDiskName, []byte("disk")},
		{imageKernelName, []byte("kernel")},
		{imageInitramfsName, []byte("initramfs")},
	} {
		dir := snapshotDir
		if file.name == cloudHypervisorMemoryRangesName || file.name == cloudHypervisorStateName || file.name == imageDiskName || file.name == imageKernelName || file.name == imageInitramfsName {
			dir = chDir
		}
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	config := map[string]any{
		"disk": "/old/base/disk.img",
		"disks": []map[string]any{
			{
				"path":         "/old/base/disk.img",
				"vhost_socket": nil,
				"vhost_user":   false,
			},
		},
		"vsock": map[string]any{
			"cid":    42,
			"socket": "/old/base/cloud-hypervisor-vsock.sock",
		},
		"fs": []map[string]any{
			{
				"tag":    "spind-cwd",
				"socket": "/old/base/cloud-hypervisor-virtiofs.sock",
			},
		},
		"net": []map[string]any{
			{
				"host_mac": "16:ed:6f:fc:2b:94",
				"tap":      "old-tap",
				"mac":      "02:00:00:00:00:01",
			},
		},
	}
	if err := writeJSON(filepath.Join(chDir, cloudHypervisorSnapshotConfigName), config, 0o644); err != nil {
		t.Fatal(err)
	}
	return snapshotDir
}

func writeTestVirtualizationFrameworkSnapshot(t *testing.T, manager *Manager, name string) string {
	t.Helper()
	snapshotDir := filepath.Join(manager.SnapshotStore, name)
	vfDir := filepath.Join(snapshotDir, snapshotVirtualizationFrameworkDirName)
	if err := os.MkdirAll(vfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindsnapshot.Metadata{
		Name:              name,
		SourceVM:          "base",
		Image:             "legacy",
		CreatedAt:         time.Now().UTC(),
		Backend:           BackendVirtualizationFramework,
		Architecture:      runtime.GOARCH,
		ExecUser:          defaultExecUser,
		CPUCount:          2,
		MemoryMiB:         1024,
		ExecPort:          defaultExecPort,
		KernelCommandLine: "console=hvc0",
		MachineIdentifier: newGenericMachineIdentifier(),
		NetworkMAC:        testNetworkMAC,
	}
	if err := writeJSON(filepath.Join(snapshotDir, snapshotMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	privateKey, publicKey, err := generateVMSSHKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{vmSSHPrivateKeyName, privateKey},
		{vmSSHPublicKeyName, publicKey},
		{"state.vzvmsave", []byte("state")},
		{imageDiskName, []byte("disk")},
		{imageKernelName, []byte("kernel")},
		{imageInitramfsName, []byte("initramfs")},
	} {
		dir := snapshotDir
		if file.name == "state.vzvmsave" || file.name == imageDiskName || file.name == imageKernelName || file.name == imageInitramfsName {
			dir = vfDir
		}
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return snapshotDir
}

func markTestSnapshotKindReady(t *testing.T, snapshotDir string) {
	t.Helper()
	var metadata spindsnapshot.Metadata
	if err := readJSON(filepath.Join(snapshotDir, snapshotMetadataName), &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.KindReady = true
	if err := writeJSON(filepath.Join(snapshotDir, snapshotMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	kindDir := filepath.Join(snapshotDir, snapshotKindDirName)
	if err := os.MkdirAll(kindDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kindMetadata := spindkind.Metadata{
		KindReady:             true,
		SourceKubeconfigPath:  "/home/test/.kube/config",
		SourceContext:         "kind-dev",
		SourceCluster:         "kind-dev",
		SourceUser:            "kind-dev",
		SourceServer:          "https://127.0.0.1:40123",
		APIServerTargetPort:   40123,
		Nodes:                 []spindkind.NodeSummary{{Name: "dev-control-plane", Ready: true}},
		ReadyCheck:            "ok",
		ReadyCheckCompletedAt: time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(kindDir, snapshotKindMetadataName), kindMetadata, 0o644); err != nil {
		t.Fatal(err)
	}
	template := []byte(`{
  "apiVersion": "v1",
  "kind": "Config",
  "clusters": [{"name": "kind-dev", "cluster": {"server": "https://127.0.0.1:40123"}}],
  "users": [{"name": "kind-dev", "user": {"token": "token"}}],
  "contexts": [{"name": "kind-dev", "context": {"cluster": "kind-dev", "user": "kind-dev"}}],
  "current-context": "kind-dev"
}
`)
	if err := os.WriteFile(filepath.Join(kindDir, snapshotKindKubeconfigTemplateName), template, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTestImage(t *testing.T, imageStore string, name string) {
	t.Helper()
	dir := filepath.Join(imageStore, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindimage.Metadata{
		Name:              name,
		Architecture:      runtime.GOARCH,
		CreatedAt:         time.Now().UTC(),
		KernelCommandLine: "console=hvc0",
		ExecUser:          defaultExecUser,
	}
	file, err := os.Create(filepath.Join(dir, imageMetadataName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(metadata); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range []string{imageKernelName, imageInitramfsName} {
		if err := os.WriteFile(filepath.Join(dir, fileName), []byte(fileName), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTestDiskImage(t, filepath.Join(dir, imageDiskName))
}

func writeTinyTestImage(t *testing.T, manager *Manager, name string, createdAt time.Time) string {
	t.Helper()
	dir := filepath.Join(manager.ImageStore, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	metadata := spindimage.Metadata{
		Name:              name,
		Architecture:      runtime.GOARCH,
		CreatedAt:         createdAt,
		KernelCommandLine: "console=hvc0",
		ExecUser:          defaultExecUser,
	}
	if err := writeJSON(filepath.Join(dir, imageMetadataName), metadata, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, fileName := range []string{imageKernelName, imageInitramfsName, imageDiskName} {
		if err := os.WriteFile(filepath.Join(dir, fileName), []byte(fileName), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func installFakeImageBuildTools(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dockerScript := `#!/bin/sh
set -eu
if [ "$#" -lt 1 ]; then
  echo "missing docker command" >&2
  exit 1
fi
command="$1"
shift
out=""
data_size=""
case "$command" in
  volume)
    if [ "${1:-}" = "create" ]; then
      if [ "${2:-}" != "spind-image-builder-nix-store" ]; then
        echo "unexpected volume: ${2:-}" >&2
        exit 1
      fi
      echo "${2:-}"
      exit 0
    fi
    ;;
  rm)
    exit 0
    ;;
  run)
    work=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --name)
          shift
          case "$1" in
            spind-image-builder-docker-*) ;;
            *)
              echo "unexpected container name: $1" >&2
              exit 1
              ;;
          esac
          ;;
        -v)
          shift
          mount="$1"
          case "$mount" in
            *:/work)
              work="${mount%:/work}"
              ;;
            spind-image-builder-nix-store:/nix)
              ;;
          esac
          ;;
        -e)
          shift
          case "$1" in
            SPIND_DATA_SIZE=*)
              data_size="${1#SPIND_DATA_SIZE=}"
              ;;
          esac
          ;;
      esac
      shift || true
    done
    if [ -z "$work" ]; then
      echo "missing /work mount" >&2
      exit 1
    fi
    out="$work/output/image"
    ;;
  *)
    echo "unexpected docker command: $command" >&2
    exit 1
    ;;
esac
if [ -z "$out" ]; then
  echo "missing output path" >&2
  exit 1
fi
mkdir -p "$out"
printf 'kernel' > "$out/kernel"
printf 'initramfs' > "$out/initramfs"
printf 'store' > "$out/nix-store.img"
case "$data_size" in
  1MiB)
    dd if=/dev/zero of="$out/docker-data.img" bs=1048576 count=1 >/dev/null 2>&1
    ;;
  *)
    printf 'data' > "$out/docker-data.img"
    ;;
esac
cat > "$out/metadata.json" <<'JSON'
{
  "name": "docker",
  "imageType": "microvm-nix",
  "architecture": "amd64",
  "execUser": "spind",
  "cpuCount": 2,
  "memoryMiB": 4096,
  "kernelCommandLine": "console=ttyS0 root=fstab",
  "sshAuthorizedKeyCommandLineParam": "spind.ssh_authorized_key",
  "disks": [
    {
      "name": "nix-store.img",
      "readOnly": true,
      "imageType": "raw"
    },
    {
      "name": "docker-data.img",
      "readOnly": false,
      "imageType": "raw",
      "create": {
        "size": "8GiB",
        "fsType": "ext4",
        "label": "spind-docker"
      }
    }
  ]
}
JSON
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(dockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeFakeRunner(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runner")
	script := fmt.Sprintf("#!/bin/sh\nSPIND_FAKE_RUNNER=1 exec %s -test.run=TestFakeRunnerProcess -- \"$@\"\n", shellSingleQuote(os.Args[0]))
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTestDiskImage(t *testing.T, path string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(filepath.Join(root, "home", "spind", ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootfsPath := filepath.Join(t.TempDir(), "rootfs.img")
	if err := os.WriteFile(rootfsPath, make([]byte, 15*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := osexec.Command("mke2fs", "-q", "-t", "ext4", "-d", root, rootfsPath).CombinedOutput()
	if err != nil {
		t.Fatalf("mke2fs: %v: %s", err, string(output))
	}
	if err := diskimage.CreateMBRDisk(path, rootfsPath, 16*1024*1024, diskimage.DefaultStartSector); err != nil {
		t.Fatal(err)
	}
}

func dumpDiskFile(t *testing.T, diskPath string, guestPath string) []byte {
	t.Helper()
	dumpPath := filepath.Join(t.TempDir(), "dump")
	partition, err := diskimage.FirstPartition(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	rootfsPath := filepath.Join(t.TempDir(), "rootfs.img")
	if err := copyDiskPartition(diskPath, rootfsPath, partition); err != nil {
		t.Fatal(err)
	}
	output, err := osexec.Command("debugfs", "-R", fmt.Sprintf("dump %s %s", guestPath, dumpPath), rootfsPath).CombinedOutput()
	if err != nil {
		t.Fatalf("debugfs dump: %v: %s", err, string(output))
	}
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fakeRunnerMain() int {
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "machine-id":
		fmt.Println(newGenericMachineIdentifier())
		return 0
	case "validate":
		if len(args) != 3 || args[1] != "--config" {
			return 2
		}
		if _, err := os.Stat(args[2]); err != nil {
			return 1
		}
		return 0
	case "start":
		if len(args) != 3 || args[1] != "--config" {
			return 2
		}
		var config vz.RunnerConfig
		if err := readJSON(args[2], &config); err != nil {
			return 1
		}
		listener, err := net.Listen("unix", config.ExecSocketPath)
		if err != nil {
			return 1
		}
		defer listener.Close()
		controlListener, err := net.Listen("unix", config.ControlSocketPath)
		if err != nil {
			return 1
		}
		defer controlListener.Close()
		saved := make(chan struct{})
		go serveOneSSHHandshake(listener)
		go serveFakeRunnerControl(controlListener, saved)
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		select {
		case <-signals:
		case <-saved:
		}
		return 0
	case "restore":
		if len(args) != 3 || args[1] != "--config" {
			return 2
		}
		var config vz.RunnerConfig
		if err := readJSON(args[2], &config); err != nil {
			return 1
		}
		if _, err := os.Stat(config.RestoreStatePath); err != nil {
			return 1
		}
		listener, err := net.Listen("unix", config.ExecSocketPath)
		if err != nil {
			return 1
		}
		defer listener.Close()
		controlListener, err := net.Listen("unix", config.ControlSocketPath)
		if err != nil {
			return 1
		}
		defer controlListener.Close()
		fmt.Println(`{"event":"restore-completed"}`)
		fmt.Println(`{"event":"resume-completed"}`)
		fmt.Println(`{"event":"exec-relay-ready"}`)
		go serveOneSSHHandshake(listener)
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGTERM)
		<-signals
		return 0
	case "stop":
		if len(args) != 3 || args[1] != "--pid" {
			return 2
		}
		pid, err := strconv.Atoi(args[2])
		if err != nil {
			return 2
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		return 0
	default:
		return 2
	}
}

func serveFakeRunnerControl(listener net.Listener, saved chan<- struct{}) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	type saveRequest struct {
		Op        string `json:"op"`
		StatePath string `json:"statePath"`
	}
	type saveResponse struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	var request saveRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(saveResponse{OK: false, Error: err.Error()})
		return
	}
	if request.Op != "save" || request.StatePath == "" {
		_ = json.NewEncoder(conn).Encode(saveResponse{OK: false, Error: "bad request"})
		return
	}
	if err := os.WriteFile(request.StatePath, []byte("state"), 0o644); err != nil {
		_ = json.NewEncoder(conn).Encode(saveResponse{OK: false, Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(saveResponse{OK: true})
	close(saved)
}

func serveOneSSHHandshake(listener net.Listener) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	serverKey, err := generateSSHPrivateKey()
	if err != nil {
		return
	}
	signer, err := ssh.ParsePrivateKey(serverKey)
	if err != nil {
		return
	}
	serverConfig := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() != defaultExecUser {
				return nil, fmt.Errorf("unexpected SSH user %q", conn.User())
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)
	sshConn, _, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		return
	}
	defer sshConn.Close()
	ssh.DiscardRequests(requests)
}

func serveSSHExec(t *testing.T, socketPath string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		serverKey, err := generateSSHPrivateKey()
		if err != nil {
			t.Errorf("generate server key: %v", err)
			return
		}
		serverConfig := &ssh.ServerConfig{
			PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				if conn.User() != defaultExecUser {
					return nil, fmt.Errorf("unexpected SSH user %q", conn.User())
				}
				return nil, nil
			},
		}
		signer, err := ssh.ParsePrivateKey(serverKey)
		if err != nil {
			t.Errorf("parse server key: %v", err)
			return
		}
		serverConfig.AddHostKey(signer)
		sshConn, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
		if err != nil {
			t.Errorf("start SSH server: %v", err)
			return
		}
		defer sshConn.Close()
		go ssh.DiscardRequests(requests)
		for newChannel := range channels {
			if newChannel.ChannelType() != "session" {
				_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel")
				continue
			}
			channel, requests, err := newChannel.Accept()
			if err != nil {
				t.Errorf("accept channel: %v", err)
				return
			}
			for request := range requests {
				if request.Type != "exec" {
					request.Reply(false, nil)
					continue
				}
				command := parseSSHExecCommand(request.Payload)
				request.Reply(true, nil)
				stdin, err := io.ReadAll(channel)
				if err != nil {
					t.Errorf("read stdin: %v", err)
					return
				}
				if command != "sh -lc echo" || string(stdin) != "input" {
					t.Errorf("unexpected command=%q stdin=%q", command, string(stdin))
					return
				}
				if _, err := io.WriteString(channel, "out\n"); err != nil {
					t.Errorf("write stdout: %v", err)
					return
				}
				if _, err := io.WriteString(channel.Stderr(), "err\n"); err != nil {
					t.Errorf("write stderr: %v", err)
					return
				}
				channel.SendRequest("exit-status", false, sshExitStatus(7))
				_ = channel.Close()
				return
			}
		}
	}()
	return done
}

func drainRequestPaths(requests <-chan string) []string {
	var paths []string
	for {
		select {
		case path := <-requests:
			paths = append(paths, path)
		default:
			return paths
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func generateSSHPrivateKey() ([]byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return marshalOpenSSHPrivateKey(privateKey)
}

func marshalOpenSSHPrivateKey(privateKey ed25519.PrivateKey) ([]byte, error) {
	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}

func parseSSHExecCommand(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	size := binary.BigEndian.Uint32(payload[:4])
	if len(payload) < int(4+size) {
		return ""
	}
	return string(payload[4 : 4+size])
}

func sshExitStatus(status uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, status)
	return payload
}
