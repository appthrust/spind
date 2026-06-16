package output

import (
	"fmt"
	"io"

	spindkind "github.com/suin/spind/internal/spind/kind"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
)

type SnapshotInfo struct {
	Name           string             `json:"name"`
	SourceVM       string             `json:"sourceVM,omitempty"`
	Backend        string             `json:"backend,omitempty"`
	CreatedAt      string             `json:"createdAt,omitempty"`
	CPUCount       int                `json:"cpuCount,omitempty"`
	MemoryMiB      int                `json:"memoryMiB,omitempty"`
	ExecPort       uint32             `json:"execPort,omitempty"`
	KernelCommand  string             `json:"kernelCommand,omitempty"`
	DiskSizeBytes  int64              `json:"diskSizeBytes"`
	StateSizeBytes int64              `json:"stateSizeBytes"`
	TotalSizeBytes int64              `json:"totalSizeBytes"`
	Health         string             `json:"health"`
	HealthMessage  string             `json:"healthMessage,omitempty"`
	SnapshotDir    string             `json:"snapshotDir"`
	KindReady      bool               `json:"kindReady,omitempty"`
	KindMetadata   spindkind.Metadata `json:"kindMetadata,omitempty"`
	KindTemplate   bool               `json:"kindKubeconfigTemplate,omitempty"`
	Artifacts      []SnapshotArtifact `json:"artifacts,omitempty"`
}

type SnapshotArtifact struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Found bool   `json:"found"`
}

func NewSnapshotInfo(info spindsnapshot.Info) SnapshotInfo {
	artifacts := make([]SnapshotArtifact, 0, len(info.Artifacts))
	for _, artifact := range info.Artifacts {
		artifacts = append(artifacts, SnapshotArtifact{
			Name:  artifact.Name,
			Path:  artifact.Path,
			Size:  artifact.Size,
			Found: artifact.Found,
		})
	}
	return SnapshotInfo{
		Name:           info.Name,
		SourceVM:       info.SourceVM,
		Backend:        info.Backend,
		CreatedAt:      FormatTime(info.CreatedAt),
		CPUCount:       info.CPUCount,
		MemoryMiB:      info.MemoryMiB,
		ExecPort:       info.ExecPort,
		KernelCommand:  info.KernelCommand,
		DiskSizeBytes:  info.DiskSizeBytes,
		StateSizeBytes: info.StateSizeBytes,
		TotalSizeBytes: info.TotalSizeBytes,
		Health:         info.Health,
		HealthMessage:  info.HealthMessage,
		SnapshotDir:    info.SnapshotDir,
		KindReady:      info.KindReady,
		KindMetadata:   info.KindMetadata,
		KindTemplate:   info.KindTemplate,
		Artifacts:      artifacts,
	}
}

func PrintSnapshotList(stdout io.Writer, snapshots []spindsnapshot.Info) {
	rows := make([][]string, 0, len(snapshots))
	for _, info := range snapshots {
		rows = append(rows, []string{
			info.Name,
			DisplayValue(info.Backend),
			DisplayValue(info.SourceVM),
			FormatTime(info.CreatedAt),
			fmt.Sprintf("%d", info.CPUCount),
			FormatMiB(info.MemoryMiB),
			FormatSize(info.DiskSizeBytes),
			FormatSize(info.StateSizeBytes),
			info.Health,
		})
	}
	printTable(stdout,
		[]string{"NAME", "BACKEND", "SOURCE_VM", "CREATED_AT", "CPU", "MEMORY", "DISK", "STATE", "HEALTH"},
		rows,
	)
}

func PrintSnapshotPruneList(stdout io.Writer, snapshots []spindsnapshot.Info) {
	rows := make([][]string, 0, len(snapshots))
	for _, info := range snapshots {
		rows = append(rows, []string{
			info.Name,
			DisplayValue(info.Backend),
			FormatTime(info.CreatedAt),
			FormatSize(info.TotalSizeBytes),
			info.Health,
		})
	}
	printTable(stdout,
		[]string{"NAME", "BACKEND", "CREATED_AT", "SIZE", "HEALTH"},
		rows,
	)
}

func PrintSnapshotInfo(stdout io.Writer, info spindsnapshot.Info) {
	fmt.Fprintf(stdout, "name: %s\n", info.Name)
	fmt.Fprintf(stdout, "backend: %s\n", DisplayValue(info.Backend))
	fmt.Fprintf(stdout, "sourceVM: %s\n", DisplayValue(info.SourceVM))
	fmt.Fprintf(stdout, "createdAt: %s\n", FormatTime(info.CreatedAt))
	fmt.Fprintf(stdout, "cpu: %d\n", info.CPUCount)
	fmt.Fprintf(stdout, "memory: %s\n", FormatMiB(info.MemoryMiB))
	fmt.Fprintf(stdout, "diskSize: %s\n", FormatSize(info.DiskSizeBytes))
	fmt.Fprintf(stdout, "stateSize: %s\n", FormatSize(info.StateSizeBytes))
	fmt.Fprintf(stdout, "totalSize: %s\n", FormatSize(info.TotalSizeBytes))
	fmt.Fprintf(stdout, "health: %s\n", info.Health)
	if info.HealthMessage != "" {
		fmt.Fprintf(stdout, "healthMessage: %s\n", info.HealthMessage)
	}
	fmt.Fprintf(stdout, "snapshotDir: %s\n", info.SnapshotDir)
	if info.KindReady {
		fmt.Fprintln(stdout, "kindReady: true")
		fmt.Fprintf(stdout, "kindContext: %s\n", DisplayValue(info.KindMetadata.SourceContext))
		fmt.Fprintf(stdout, "kindCluster: %s\n", DisplayValue(info.KindMetadata.SourceCluster))
		fmt.Fprintf(stdout, "kindKubeconfigTemplate: %t\n", info.KindTemplate)
		if len(info.KindMetadata.Nodes) > 0 {
			fmt.Fprintln(stdout, "kindNodes:")
			for _, node := range info.KindMetadata.Nodes {
				fmt.Fprintf(stdout, "  %s\tready=%t\n", node.Name, node.Ready)
			}
		}
	}
	if len(info.Artifacts) > 0 {
		fmt.Fprintln(stdout, "artifacts:")
		fmt.Fprintln(stdout, "  NAME\tFOUND\tSIZE\tPATH")
		for _, artifact := range info.Artifacts {
			fmt.Fprintf(stdout, "  %s\t%t\t%s\t%s\n",
				artifact.Name,
				artifact.Found,
				FormatSize(artifact.Size),
				artifact.Path,
			)
		}
	}
}

func PrintSnapshotWarnings(stderr io.Writer, snapshots []spindsnapshot.Info) {
	for _, info := range snapshots {
		if info.Health == "ok" {
			continue
		}
		fmt.Fprintf(stderr, "warning: snapshot %q health is %s: %s\n", info.Name, info.Health, DisplayValue(info.HealthMessage))
	}
}

func SnapshotsHaveProblems(snapshots []spindsnapshot.Info) bool {
	for _, info := range snapshots {
		if info.Health != "ok" {
			return true
		}
	}
	return false
}
