package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	spindimage "github.com/suin/spind/internal/spind/image"
	spindkind "github.com/suin/spind/internal/spind/kind"
	"github.com/suin/spind/internal/spind/store"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

const (
	MetadataName                      = "metadata.json"
	VMSSHPrivateKeyName               = "ssh_key"
	VMSSHPublicKeyName                = "ssh_key.pub"
	VirtualizationFrameworkDirName    = "vf"
	CloudHypervisorDirName            = "cloud-hypervisor"
	KindDirName                       = spindkind.DirName
	KindMetadataName                  = spindkind.MetadataName
	KindKubeconfigTemplateName        = spindkind.KubeconfigTemplateName
	CloudHypervisorSnapshotConfigName = "config.json"
	CloudHypervisorMemoryRangesName   = "memory-ranges"
	CloudHypervisorStateName          = "state.json"
	VirtualizationFrameworkStateName  = "state.vzvmsave"
)

type Store struct {
	SnapshotStore string
}

func (s Store) List() ([]Info, error) {
	entries, err := os.ReadDir(s.SnapshotStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshot store: %w", err)
	}
	snapshots := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		snapshots = append(snapshots, Inspect(filepath.Join(s.SnapshotStore, entry.Name()), entry.Name()))
	}
	sort.Slice(snapshots, func(i int, j int) bool {
		return snapshots[i].Name < snapshots[j].Name
	})
	return snapshots, nil
}

func (s Store) Info(name string) (Info, error) {
	if err := ValidateName(name); err != nil {
		return Info{}, err
	}
	snapshotDir := filepath.Join(s.SnapshotStore, name)
	if _, err := os.Stat(snapshotDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{}, fmt.Errorf("snapshot %q: %w", name, store.ErrNotFound)
		}
		return Info{}, fmt.Errorf("check snapshot %q: %w", name, err)
	}
	return Inspect(snapshotDir, name), nil
}

func (s Store) Delete(name string) error {
	_, err := s.DeleteWithSize(name)
	return err
}

func (s Store) DeleteWithSize(name string) (int64, error) {
	if err := ValidateName(name); err != nil {
		return 0, err
	}
	snapshotDir := filepath.Join(s.SnapshotStore, name)
	if _, err := os.Stat(snapshotDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("snapshot %q: %w", name, store.ErrNotFound)
		}
		return 0, fmt.Errorf("check snapshot %q: %w", name, err)
	}
	size, err := store.DirectorySize(snapshotDir)
	if err != nil {
		return 0, fmt.Errorf("size snapshot %q: %w", name, err)
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		return 0, fmt.Errorf("delete snapshot %q: %w", name, err)
	}
	return size, nil
}

func (s Store) Prune(olderThan time.Duration, backend string, dryRun bool) ([]Info, int64, error) {
	if backend != "" {
		if err := ValidateBackend(backend); err != nil {
			return nil, 0, err
		}
	}
	snapshots, err := s.List()
	if err != nil {
		return nil, 0, err
	}
	cutoff := time.Time{}
	if olderThan > 0 {
		cutoff = time.Now().UTC().Add(-olderThan)
	}
	targets := []Info{}
	var total int64
	for _, snapshot := range snapshots {
		if backend != "" && snapshot.Backend != backend {
			continue
		}
		if !cutoff.IsZero() && (snapshot.CreatedAt.IsZero() || !snapshot.CreatedAt.Before(cutoff)) {
			continue
		}
		targets = append(targets, snapshot)
		total += snapshot.TotalSizeBytes
	}
	if dryRun {
		return targets, total, nil
	}
	for _, snapshot := range targets {
		size, err := s.DeleteWithSize(snapshot.Name)
		if err != nil {
			return targets, total, err
		}
		if snapshot.TotalSizeBytes == 0 {
			total += size
		}
	}
	return targets, total, nil
}

func ValidateName(name string) error {
	if !store.ValidName(name) {
		return fmt.Errorf("%q: %w", name, store.ErrInvalidName)
	}
	return nil
}

func ValidateBackend(backend string) error {
	switch backend {
	case spindvm.BackendVirtualizationFramework, spindvm.BackendCloudHypervisor:
		return nil
	default:
		return fmt.Errorf("backend %q: %w", backend, store.ErrInvalidName)
	}
}

func Inspect(snapshotDir string, name string) Info {
	info := Info{
		Name:        name,
		SnapshotDir: snapshotDir,
		Health:      "ok",
	}
	totalSize, err := store.DirectorySize(snapshotDir)
	if err == nil {
		info.TotalSizeBytes = totalSize
	}

	var metadata Metadata
	if err := store.ReadJSON(filepath.Join(snapshotDir, MetadataName), &metadata); err != nil {
		info.Health = "invalid-metadata"
		info.HealthMessage = err.Error()
		info.Artifacts = InspectArtifacts(snapshotDir, commonArtifacts())
		return info
	}
	info.SourceVM = metadata.SourceVM
	info.Backend = metadata.Backend
	info.CreatedAt = metadata.CreatedAt
	info.CPUCount = metadata.CPUCount
	info.MemoryMiB = metadata.MemoryMiB
	info.ExecPort = metadata.ExecPort
	info.KernelCommand = metadata.KernelCommandLine
	info.KindReady = metadata.KindReady
	if metadata.KindReady {
		if kindMetadata, err := spindkind.ReadMetadata(snapshotDir); err == nil {
			info.KindMetadata = kindMetadata
		}
		if stat, err := os.Stat(filepath.Join(snapshotDir, KindDirName, KindKubeconfigTemplateName)); err == nil && !stat.IsDir() {
			info.KindTemplate = true
		}
	}
	if metadata.Backend == "" || metadata.SourceVM == "" || metadata.CreatedAt.IsZero() || metadata.CPUCount == 0 || metadata.MemoryMiB == 0 {
		info.Health = "invalid-metadata"
		info.HealthMessage = "required metadata field is missing"
	}

	artifacts := commonArtifacts()
	if metadata.KindReady {
		artifacts = append(artifacts,
			filepath.Join(KindDirName, KindMetadataName),
			filepath.Join(KindDirName, KindKubeconfigTemplateName),
		)
	}
	switch metadata.Backend {
	case spindvm.BackendVirtualizationFramework:
		artifacts = append(artifacts,
			filepath.Join(VirtualizationFrameworkDirName, VirtualizationFrameworkStateName),
			filepath.Join(VirtualizationFrameworkDirName, spindimage.KernelName),
			filepath.Join(VirtualizationFrameworkDirName, spindimage.InitramfsName),
			filepath.Join(VirtualizationFrameworkDirName, spindimage.DiskName),
		)
	case spindvm.BackendCloudHypervisor:
		artifacts = append(artifacts,
			filepath.Join(CloudHypervisorDirName, CloudHypervisorSnapshotConfigName),
			filepath.Join(CloudHypervisorDirName, CloudHypervisorMemoryRangesName),
			filepath.Join(CloudHypervisorDirName, CloudHypervisorStateName),
			filepath.Join(CloudHypervisorDirName, spindimage.KernelName),
			filepath.Join(CloudHypervisorDirName, spindimage.InitramfsName),
		)
		disks := metadata.Disks
		if len(disks) == 0 {
			disks = []spindimage.DiskMetadata{{Name: spindimage.DiskName}}
		}
		for _, disk := range disks {
			artifacts = append(artifacts, filepath.Join(CloudHypervisorDirName, disk.Name))
		}
	default:
		info.Health = "unsupported-backend"
		info.HealthMessage = fmt.Sprintf("unsupported backend %q", metadata.Backend)
	}
	info.Artifacts = InspectArtifacts(snapshotDir, artifacts)
	for _, artifact := range info.Artifacts {
		if !artifact.Found && info.Health == "ok" {
			info.Health = "missing-artifact"
			info.HealthMessage = fmt.Sprintf("missing artifact %s", artifact.Name)
		}
		switch filepath.Base(artifact.Name) {
		case spindimage.DiskName:
			if info.DiskSizeBytes == 0 {
				info.DiskSizeBytes = artifact.Size
			}
		case VirtualizationFrameworkStateName, CloudHypervisorMemoryRangesName:
			if info.StateSizeBytes == 0 {
				info.StateSizeBytes = artifact.Size
			}
		}
	}
	return info
}

func InspectArtifacts(root string, names []string) []ArtifactInfo {
	artifacts := make([]ArtifactInfo, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		artifact := ArtifactInfo{Name: name, Path: path}
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			artifact.Found = true
			artifact.Size = stat.Size()
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

func commonArtifacts() []string {
	return []string{MetadataName, VMSSHPrivateKeyName, VMSSHPublicKeyName}
}
