package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindimage "github.com/suin/spind/internal/spind/image"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	snapshotprune "github.com/suin/spind/internal/spind/snapshot/prune"
	vmexec "github.com/suin/spind/internal/spind/vm/exec"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func TestParseCommandLineAcceptsCreateImage(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "create", "base", "--image", "legacy"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm create <name>" {
		t.Fatalf("ctx.Command() = %q, want vm create <name>", ctx.Command())
	}
	if cli.VM.Create.Name != "base" || cli.VM.Create.Image != "legacy" || cli.VM.Create.Snapshot != "" {
		t.Fatalf("Create = %#v, want base/legacy", cli.VM.Create)
	}
}

func TestParseCommandLineAcceptsUpReprovision(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"up", "--reprovision"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "up" {
		t.Fatalf("ctx.Command() = %q, want up", ctx.Command())
	}
	if !cli.Up.Reprovision {
		t.Fatalf("Up.Reprovision = false, want true")
	}
}

func TestParseCommandLineAcceptsDoctorJSON(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"doctor", "--json"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "doctor" {
		t.Fatalf("ctx.Command() = %q, want doctor", ctx.Command())
	}
	if !cli.Doctor.JSON {
		t.Fatalf("Doctor.JSON = false, want true")
	}
}

func TestParseCommandLineAcceptsCreateImageBeforeName(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "create", "--image=legacy", "base"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm create <name>" {
		t.Fatalf("ctx.Command() = %q, want vm create <name>", ctx.Command())
	}
	if cli.VM.Create.Name != "base" || cli.VM.Create.Image != "legacy" {
		t.Fatalf("Create = %#v, want base/legacy", cli.VM.Create)
	}
}

func TestParseCommandLineAcceptsCreateImageResources(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "create", "base", "--image", "legacy", "--cpu", "4", "--memory", "8GiB"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm create <name>" {
		t.Fatalf("ctx.Command() = %q, want vm create <name>", ctx.Command())
	}
	if cli.VM.Create.Name != "base" || cli.VM.Create.Image != "legacy" || !cli.VM.Create.CPUSet || cli.VM.Create.CPUCount != 4 || !cli.VM.Create.MemorySet || cli.VM.Create.Memory != "8GiB" {
		t.Fatalf("Create = %#v, want base/legacy with resources", cli.VM.Create)
	}
}

func TestParseCommandLineRejectsCreateBackend(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"vm", "create", "base", "--image", "legacy", "--backend=cloud-hypervisor"}, io.Discard, io.Discard)
	if !handled || exitCode != 2 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=2", handled, exitCode)
	}
}

func TestParseCommandLineAcceptsCreateSnapshot(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "create", "worker", "--snapshot", "prepared"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm create <name>" {
		t.Fatalf("ctx.Command() = %q, want vm create <name>", ctx.Command())
	}
	if cli.VM.Create.Name != "worker" || cli.VM.Create.Snapshot != "prepared" || cli.VM.Create.Image != "" {
		t.Fatalf("Create = %#v, want worker/prepared", cli.VM.Create)
	}
}

func TestParseCommandLineRejectsCreateSnapshotResources(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"vm", "create", "worker", "--snapshot", "prepared", "--cpu", "4", "--memory", "8GiB"}, io.Discard, io.Discard)
	if !handled || exitCode != 2 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=2", handled, exitCode)
	}
}

func TestParseCommandLineRejectsCreateImageInvalidResources(t *testing.T) {
	for _, args := range [][]string{
		{"vm", "create", "base", "--image", "legacy", "--cpu", "0"},
		{"vm", "create", "base", "--image", "legacy", "--memory", "0GiB"},
		{"vm", "create", "base", "--image", "legacy", "--memory", "4XB"},
	} {
		_, _, exitCode, handled := Parse(args, io.Discard, io.Discard)
		if !handled || exitCode != 2 {
			t.Fatalf("Parse(%v) handled=%v exitCode=%d, want handled exitCode=2", args, handled, exitCode)
		}
	}
}

func TestParseCommandLineRejectsImageAndSnapshot(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"vm", "create", "worker", "--image", "legacy", "--snapshot", "prepared"}, io.Discard, io.Discard)
	if !handled || exitCode != 2 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=2", handled, exitCode)
	}
}

func TestParseCommandLineRequiresImageOrSnapshot(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"vm", "create", "worker"}, io.Discard, io.Discard)
	if !handled || exitCode != 2 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=2", handled, exitCode)
	}
}

func TestParseCommandLineRejectsRootCreate(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"create", "worker", "--image", "legacy"}, io.Discard, io.Discard)
	if !handled || exitCode != 2 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=2", handled, exitCode)
	}
}

func TestParseCommandLineAcceptsVMStart(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "start", "base"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm start <name>" {
		t.Fatalf("ctx.Command() = %q, want vm start <name>", ctx.Command())
	}
	if cli.VM.Start.Name != "base" {
		t.Fatalf("VM.Start.Name = %q, want base", cli.VM.Start.Name)
	}
}

func TestParseCommandLineAcceptsVMStop(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "stop", "base"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm stop <name>" {
		t.Fatalf("ctx.Command() = %q, want vm stop <name>", ctx.Command())
	}
	if cli.VM.Stop.Name != "base" {
		t.Fatalf("VM.Stop.Name = %q, want base", cli.VM.Stop.Name)
	}
}

func TestParseCommandLineAcceptsSnapshotCreate(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "create", "prepared", "--vm", "base"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot create <snapshot-name>" {
		t.Fatalf("ctx.Command() = %q, want snapshot create <snapshot-name>", ctx.Command())
	}
	if cli.Snapshot.Create.Name != "prepared" || cli.Snapshot.Create.VM != "base" {
		t.Fatalf("Snapshot.Create = %#v, want prepared/base", cli.Snapshot.Create)
	}
}

func TestParseCommandLineAcceptsK8sSnapshotCreate(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "create", "kind-ready", "--vm", "kind-base", "--k8s=kind", "--kubeconfig", "/tmp/config", "--context", "kind-dev"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot create <snapshot-name>" {
		t.Fatalf("ctx.Command() = %q, want snapshot create <snapshot-name>", ctx.Command())
	}
	create := cli.Snapshot.Create
	if create.Name != "kind-ready" || create.VM != "kind-base" || create.K8s != "kind" || create.Kubeconfig != "/tmp/config" || create.Context != "kind-dev" {
		t.Fatalf("Snapshot.Create = %#v", create)
	}
}

func TestParseCommandLineAcceptsKubeconfigMerge(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"kubeconfig", "merge", "kind-work", "--kubeconfig", "/tmp/config", "--replace", "--set-current-context"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "kubeconfig merge <name>" {
		t.Fatalf("ctx.Command() = %q, want kubeconfig merge <name>", ctx.Command())
	}
	merge := cli.Kubeconfig.Merge
	if merge.Name != "kind-work" || merge.Kubeconfig != "/tmp/config" || !merge.Replace || !merge.SetCurrentContext {
		t.Fatalf("Kubeconfig.Merge = %#v", merge)
	}
}

func TestParseCommandLineAcceptsKubeconfigUnmerge(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"kubeconfig", "unmerge", "kind-work", "--kubeconfig", "/tmp/config"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "kubeconfig unmerge <name>" {
		t.Fatalf("ctx.Command() = %q, want kubeconfig unmerge <name>", ctx.Command())
	}
	unmerge := cli.Kubeconfig.Unmerge
	if unmerge.Name != "kind-work" || unmerge.Kubeconfig != "/tmp/config" {
		t.Fatalf("Kubeconfig.Unmerge = %#v", unmerge)
	}
}

func TestParseCommandLineAcceptsSnapshotList(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "list", "--json", "--strict"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot list" {
		t.Fatalf("ctx.Command() = %q, want snapshot list", ctx.Command())
	}
	if !cli.Snapshot.List.JSON || !cli.Snapshot.List.Strict {
		t.Fatalf("Snapshot.List = %#v, want json/strict", cli.Snapshot.List)
	}
}

func TestParseCommandLineAcceptsSnapshotInspect(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "inspect", "prepared", "--json"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot inspect <snapshot-name>" {
		t.Fatalf("ctx.Command() = %q, want snapshot inspect <snapshot-name>", ctx.Command())
	}
	if cli.Snapshot.Inspect.Name != "prepared" || !cli.Snapshot.Inspect.JSON {
		t.Fatalf("Snapshot.Inspect = %#v, want prepared/json", cli.Snapshot.Inspect)
	}
}

func TestParseCommandLineAcceptsSnapshotDelete(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "delete", "prepared"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot delete <snapshot-name>" {
		t.Fatalf("ctx.Command() = %q, want snapshot delete <snapshot-name>", ctx.Command())
	}
	if cli.Snapshot.Delete.Name != "prepared" {
		t.Fatalf("Snapshot.Delete.Name = %q, want prepared", cli.Snapshot.Delete.Name)
	}
}

func TestParseCommandLineAcceptsDelete(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "delete", "base", "work", "--force", "--unmerge-kubeconfig"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm delete <name>..." {
		t.Fatalf("ctx.Command() = %q, want vm delete <name>...", ctx.Command())
	}
	if strings.Join(cli.VM.Delete.Names, ",") != "base,work" || !cli.VM.Delete.Force || !cli.VM.Delete.UnmergeKubeconfig {
		t.Fatalf("Delete = %#v, want base/work force unmerge", cli.VM.Delete)
	}
}

func TestParseCommandLineAcceptsSnapshotPrune(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "prune", "--dry-run", "--older-than", "24h", "--backend", "cloud-hypervisor"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot prune" {
		t.Fatalf("ctx.Command() = %q, want snapshot prune", ctx.Command())
	}
	if !cli.Snapshot.Prune.DryRun || cli.Snapshot.Prune.OlderThan != "24h" || cli.Snapshot.Prune.Backend != "cloud-hypervisor" {
		t.Fatalf("Snapshot.Prune = %#v, want dry-run/24h/cloud-hypervisor", cli.Snapshot.Prune)
	}
}

func TestParseCommandLineAcceptsSnapshotPruneAll(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"snapshot", "prune", "--all", "--dry-run"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "snapshot prune" {
		t.Fatalf("ctx.Command() = %q, want snapshot prune", ctx.Command())
	}
	if !cli.Snapshot.Prune.All || !cli.Snapshot.Prune.DryRun {
		t.Fatalf("Snapshot.Prune = %#v, want all dry-run", cli.Snapshot.Prune)
	}
}

func TestValidateSnapshotPruneSelection(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command snapshotprune.Options
		wantErr bool
	}{
		{name: "all", command: snapshotprune.Options{All: true}},
		{name: "older than", command: snapshotprune.Options{OlderThan: "24h"}},
		{name: "backend", command: snapshotprune.Options{Backend: spindvm.BackendCloudHypervisor}},
		{name: "missing selector", command: snapshotprune.Options{}, wantErr: true},
		{name: "dry run only", command: snapshotprune.Options{DryRun: true}, wantErr: true},
		{name: "all with older than", command: snapshotprune.Options{All: true, OlderThan: "24h"}, wantErr: true},
		{name: "all with backend", command: snapshotprune.Options{All: true, Backend: spindvm.BackendCloudHypervisor}, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := snapshotprune.ValidateSelection(tt.command)
			if (err != nil) != tt.wantErr {
				t.Fatalf("snapshotprune.ValidateSelection() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestParseCommandLineAcceptsVMList(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "list", "base", "--json"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm list [name]" {
		t.Fatalf("ctx.Command() = %q, want vm list [name]", ctx.Command())
	}
	if cli.VM.List.Name != "base" || !cli.VM.List.JSON {
		t.Fatalf("VM.List = %#v, want base/json", cli.VM.List)
	}
}

func TestParseCommandLineAcceptsImageList(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"image", "list", "--json"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "image list" {
		t.Fatalf("ctx.Command() = %q, want image list", ctx.Command())
	}
	if !cli.Image.List.JSON {
		t.Fatalf("Image.List = %#v, want json", cli.Image.List)
	}
}

func TestParseCommandLineAcceptsImageBuild(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"image", "build", "docker", "--config", "template://docker", "--force", "--data-size", "16GiB"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "image build <name>" {
		t.Fatalf("ctx.Command() = %q, want image build <name>", ctx.Command())
	}
	if cli.Image.Build.Name != "docker" || cli.Image.Build.Config != "template://docker" || !cli.Image.Build.Force || cli.Image.Build.DataSize != "16GiB" {
		t.Fatalf("Image.Build = %#v", cli.Image.Build)
	}
}

func TestParseCommandLineAcceptsImageDelete(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"image", "delete", "legacy", "docker", "--force"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "image delete <name>" {
		t.Fatalf("ctx.Command() = %q, want image delete <name>", ctx.Command())
	}
	if strings.Join(cli.Image.Delete.Names, ",") != "legacy,docker" || !cli.Image.Delete.Force {
		t.Fatalf("Image.Delete = %#v, want legacy/docker force", cli.Image.Delete)
	}
}

func TestParseCommandLineAcceptsDockerRelay(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"docker-relay", "base", "--endpoint", "/tmp/docker.sock", "--vsock", "/tmp/vsock.sock", "--port", "10240"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "docker-relay <vm-name>" {
		t.Fatalf("ctx.Command() = %q, want docker-relay <vm-name>", ctx.Command())
	}
	if cli.Relay.Docker.VMName != "base" || cli.Relay.Docker.Endpoint != "/tmp/docker.sock" || cli.Relay.Docker.Vsock != "/tmp/vsock.sock" || cli.Relay.Docker.Port != 10240 {
		t.Fatalf("DockerRelay = %#v", cli.Relay.Docker)
	}
}

func TestParseCommandLineAcceptsDockerPortRelay(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"docker-port-relay", "base", "--docker-endpoint", "/tmp/docker.sock", "--guest-ip", "192.168.64.2", "--tcp-forward", "/tmp/tcp-forward.sock", "--guest-port", "10241"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "docker-port-relay <vm-name>" {
		t.Fatalf("ctx.Command() = %q, want docker-port-relay <vm-name>", ctx.Command())
	}
	if cli.Relay.DockerPort.VMName != "base" || cli.Relay.DockerPort.DockerEndpoint != "/tmp/docker.sock" || cli.Relay.DockerPort.GuestIP != "192.168.64.2" || cli.Relay.DockerPort.TCPForward != "/tmp/tcp-forward.sock" || cli.Relay.DockerPort.GuestPort != 10241 {
		t.Fatalf("DockerPortRelay = %#v", cli.Relay.DockerPort)
	}
}

func TestParseCommandLineAcceptsKubernetesRelay(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"kubernetes-relay", "work", "--listen-port", "49321", "--target-port", "40123", "--vsock", "/tmp/vsock.sock", "--guest-port", "10241"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "kubernetes-relay <vm-name>" {
		t.Fatalf("ctx.Command() = %q, want kubernetes-relay <vm-name>", ctx.Command())
	}
	relay := cli.Relay.Kubernetes
	if relay.VMName != "work" || relay.ListenPort != 49321 || relay.TargetPort != 40123 || relay.Vsock != "/tmp/vsock.sock" || relay.GuestPort != 10241 {
		t.Fatalf("KubernetesRelay = %#v", relay)
	}
}

func TestPrintImageListAlignsColumnsWithoutTabs(t *testing.T) {
	var stdout bytes.Buffer
	createdAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	images := []spindimage.Info{
		{
			Name:         "legacy",
			Architecture: "amd64",
			SizeBytes:    1024 * 1024,
			CreatedAt:    createdAt,
			Health:       "ok",
		},
		{
			Name:         "docker-host-image-with-long-name",
			Architecture: "amd64",
			SizeBytes:    8 * 1024 * 1024 * 1024,
			CreatedAt:    createdAt.Add(time.Hour),
			Health:       "unhealthy",
		},
	}

	output.PrintImageList(&stdout, images)

	output := stdout.String()
	if strings.Contains(output, "\t") {
		t.Fatalf("output.PrintImageList output contains tab characters:\n%s", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3:\n%s", len(lines), output)
	}
	for _, column := range []struct {
		header string
		values []string
	}{
		{header: "ARCH", values: []string{"amd64", "amd64"}},
		{header: "SIZE", values: []string{"1.0 MiB", "8.0 GiB"}},
		{header: "CREATED_AT", values: []string{"2026-06-09T01:02:03Z", "2026-06-09T02:02:03Z"}},
		{header: "HEALTH", values: []string{"ok", "unhealthy"}},
	} {
		headerIndex := strings.Index(lines[0], column.header)
		if headerIndex < 0 {
			t.Fatalf("header %q missing:\n%s", column.header, output)
		}
		for rowIndex, value := range column.values {
			if len(lines[rowIndex+1]) < headerIndex || !strings.HasPrefix(lines[rowIndex+1][headerIndex:], value) {
				t.Fatalf("column %q row %d is not aligned at %d:\n%s", column.header, rowIndex+1, headerIndex, output)
			}
		}
	}
}

func TestPrintVMListAlignsColumnsWithoutTabs(t *testing.T) {
	var stdout bytes.Buffer
	startedAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	vms := []spindvm.Info{
		{
			Name:      "base",
			Backend:   spindvm.BackendCloudHypervisor,
			Status:    "stopped",
			ExecReady: false,
		},
		{
			Name:              "docker-host-ch-passt3-from-snapshot",
			Backend:           spindvm.BackendCloudHypervisor,
			Status:            "running",
			ExecReady:         true,
			DockerAvailable:   true,
			DockerEndpointURI: "unix:///tmp/spind/vms/docker-host-ch-passt3-from-snapshot/docker.sock",
			FromSnapshot:      true,
			SourceSnapshot:    "docker-host-ch-passt3-snap",
			StartedAt:         startedAt,
		},
	}

	output.PrintVMList(&stdout, vms)

	output := stdout.String()
	if strings.Contains(output, "\t") {
		t.Fatalf("output.PrintVMList output contains tab characters:\n%s", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3:\n%s", len(lines), output)
	}
	for _, column := range []struct {
		header string
		values []string
	}{
		{header: "BACKEND", values: []string{spindvm.BackendCloudHypervisor, spindvm.BackendCloudHypervisor}},
		{header: "STATUS", values: []string{"stopped", "running"}},
		{header: "EXEC_READY", values: []string{"false", "true"}},
		{header: "DOCKER", values: []string{"unavailable", "unix:///tmp/spind/vms/docker-host-ch-passt3-from-snapshot/docker.sock"}},
		{header: "FROM_SNAPSHOT", values: []string{"false", "true"}},
		{header: "SOURCE_SNAPSHOT", values: []string{"-", "docker-host-ch-passt3-snap"}},
		{header: "STARTED_AT", values: []string{"", "2026-06-09T01:02:03Z"}},
	} {
		headerIndex := strings.Index(lines[0], column.header)
		if headerIndex < 0 {
			t.Fatalf("header %q missing:\n%s", column.header, output)
		}
		for rowIndex, value := range column.values {
			if value == "" {
				continue
			}
			if len(lines[rowIndex+1]) < headerIndex || !strings.HasPrefix(lines[rowIndex+1][headerIndex:], value) {
				t.Fatalf("column %q row %d is not aligned at %d:\n%s", column.header, rowIndex+1, headerIndex, output)
			}
		}
	}
}

func TestPrintSnapshotListAlignsColumnsWithoutTabs(t *testing.T) {
	var stdout bytes.Buffer
	createdAt := time.Date(2026, 6, 9, 1, 2, 3, 0, time.UTC)
	snapshots := []spindsnapshot.Info{
		{
			Name:           "prepared",
			Backend:        spindvm.BackendCloudHypervisor,
			SourceVM:       "base",
			CreatedAt:      createdAt,
			CPUCount:       2,
			MemoryMiB:      1024,
			DiskSizeBytes:  1024 * 1024 * 1024,
			StateSizeBytes: 1024 * 1024 * 1024,
			Health:         "ok",
		},
		{
			Name:           "docker-host-ch-passt3-snap",
			Backend:        spindvm.BackendCloudHypervisor,
			SourceVM:       "docker-host-ch-passt3",
			CreatedAt:      createdAt.Add(time.Hour),
			CPUCount:       2,
			MemoryMiB:      2048,
			DiskSizeBytes:  8 * 1024 * 1024 * 1024,
			StateSizeBytes: 2 * 1024 * 1024 * 1024,
			Health:         "ok",
		},
	}

	output.PrintSnapshotList(&stdout, snapshots)

	output := stdout.String()
	if strings.Contains(output, "\t") {
		t.Fatalf("output.PrintSnapshotList output contains tab characters:\n%s", output)
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3:\n%s", len(lines), output)
	}
	for _, column := range []struct {
		header string
		values []string
	}{
		{header: "BACKEND", values: []string{spindvm.BackendCloudHypervisor, spindvm.BackendCloudHypervisor}},
		{header: "SOURCE_VM", values: []string{"base", "docker-host-ch-passt3"}},
		{header: "CREATED_AT", values: []string{"2026-06-09T01:02:03Z", "2026-06-09T02:02:03Z"}},
		{header: "CPU", values: []string{"2", "2"}},
		{header: "MEMORY", values: []string{"1.0 GiB", "2.0 GiB"}},
		{header: "DISK", values: []string{"1.0 GiB", "8.0 GiB"}},
		{header: "STATE", values: []string{"1.0 GiB", "2.0 GiB"}},
		{header: "HEALTH", values: []string{"ok", "ok"}},
	} {
		headerIndex := strings.Index(lines[0], column.header)
		if headerIndex < 0 {
			t.Fatalf("header %q missing:\n%s", column.header, output)
		}
		for rowIndex, value := range column.values {
			if len(lines[rowIndex+1]) < headerIndex || !strings.HasPrefix(lines[rowIndex+1][headerIndex:], value) {
				t.Fatalf("column %q row %d is not aligned at %d:\n%s", column.header, rowIndex+1, headerIndex, output)
			}
		}
	}
}

func TestPrintVMInfoShowsUnsupportedHostShareCapability(t *testing.T) {
	var stdout bytes.Buffer
	info := spindvm.Info{
		Name:                   "docker-from-snapshot",
		Status:                 "running",
		Backend:                spindvm.BackendCloudHypervisor,
		ExecReady:              true,
		RestoreMode:            "saved-state",
		VMDir:                  "/tmp/spind/vms/docker-from-snapshot",
		StatePath:              "/tmp/spind/vms/docker-from-snapshot/state.json",
		DockerAPISupport:       "supported",
		DockerAPIStatus:        "ready",
		DockerAvailable:        true,
		DockerEndpointURI:      "unix:///tmp/spind/vms/docker-from-snapshot/docker.sock",
		DockerNetworkSupport:   "supported",
		DockerNetworkStatus:    "ready",
		DockerNetworkReady:     true,
		DockerPortRelaySupport: "supported",
		DockerPortRelayStatus:  "ready",
		DockerPortRelayReady:   true,
		HostShareSupport:       "unsupported",
		HostShareStatus:        "not-applicable",
		HostShareStatusReason:  "cloud-hypervisor snapshot restore prioritizes saved-state restore speed",
	}

	output.PrintVMInfo(&stdout, info)

	text := stdout.String()
	for _, want := range []string{
		"dockerApiSupport: supported\n",
		"dockerApiStatus: ready\n",
		"dockerNetworkSupport: supported\n",
		"dockerNetworkStatus: ready\n",
		"dockerPortRelaySupport: supported\n",
		"dockerPortRelayStatus: ready\n",
		"shared path: unsupported\n",
		"shared path reason: cloud-hypervisor snapshot restore prioritizes saved-state restore speed\n",
		"hostShareSupport: unsupported\n",
		"hostShareStatus: not-applicable\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output.PrintVMInfo output missing %q:\n%s", want, text)
		}
	}

	jsonOutput := output.NewVMInfo(info)
	if jsonOutput.Docker.SharedPath.Support != "unsupported" || jsonOutput.Docker.SharedPath.Ready != "not-applicable" {
		t.Fatalf("JSON sharedPath = %#v, want unsupported/not-applicable", jsonOutput.Docker.SharedPath)
	}
	if jsonOutput.Docker.SharedPath.Reason != "cloud-hypervisor snapshot restore prioritizes saved-state restore speed" {
		t.Fatalf("JSON sharedPath reason = %q", jsonOutput.Docker.SharedPath.Reason)
	}
	if jsonOutput.Docker.API.Endpoint != "unix:///tmp/spind/vms/docker-from-snapshot/docker.sock" {
		t.Fatalf("JSON docker api endpoint = %q", jsonOutput.Docker.API.Endpoint)
	}
}

func TestPrintVMInfoShowsUnavailableHostShareReasonAndLog(t *testing.T) {
	var stdout bytes.Buffer
	info := spindvm.Info{
		Name:                     "docker-host",
		Status:                   "running",
		Backend:                  spindvm.BackendCloudHypervisor,
		ExecReady:                true,
		RestoreMode:              "boot",
		VMDir:                    "/tmp/spind/vms/docker-host",
		StatePath:                "/tmp/spind/vms/docker-host/state.json",
		HostShareSupport:         "supported",
		HostShareStatus:          "unavailable",
		HostShareStatusReason:    "virtiofsd not found",
		HostShareVirtioFSLogPath: "/tmp/spind/vms/docker-host/virtiofsd.log",
	}

	output.PrintVMInfo(&stdout, info)

	text := stdout.String()
	for _, want := range []string{
		"shared path: unavailable\n",
		"shared path reason: virtiofsd not found\n",
		"log: /tmp/spind/vms/docker-host/virtiofsd.log\n",
		"hostShareSupport: supported\n",
		"hostShareStatus: unavailable\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output.PrintVMInfo output missing %q:\n%s", want, text)
		}
	}

	jsonOutput := output.NewVMInfo(info)
	if jsonOutput.Docker.SharedPath.Reason != "virtiofsd not found" {
		t.Fatalf("JSON sharedPath reason = %q", jsonOutput.Docker.SharedPath.Reason)
	}
	if jsonOutput.Docker.SharedPath.Log != "/tmp/spind/vms/docker-host/virtiofsd.log" {
		t.Fatalf("JSON sharedPath log = %q", jsonOutput.Docker.SharedPath.Log)
	}
}

func TestPrintVMInfoShowsKubernetesStatus(t *testing.T) {
	var stdout bytes.Buffer
	info := spindvm.Info{
		Name:                     "kind-work",
		Status:                   "running",
		Backend:                  spindvm.BackendVirtualizationFramework,
		ExecReady:                true,
		RestoreMode:              "saved-state",
		VMDir:                    "/tmp/spind/vms/kind-work",
		StatePath:                "/tmp/spind/vms/kind-work/state.json",
		KubernetesSupport:        "supported",
		KubernetesStatus:         "ready",
		KubernetesReady:          true,
		KubernetesKubeconfigPath: "/tmp/spind/vms/kind-work/kubeconfig",
		KubernetesContext:        "spind-kind-work",
		KubernetesAPIServerURL:   "https://127.0.0.1:49321",
		KubernetesRelayPID:       1234,
	}

	output.PrintVMInfo(&stdout, info)
	text := stdout.String()
	for _, want := range []string{
		"kubernetes: ready\n",
		"kubeconfig: /tmp/spind/vms/kind-work/kubeconfig\n",
		"context: spind-kind-work\n",
		"api server: https://127.0.0.1:49321\n",
		"kubernetesRelayPid: 1234\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output.PrintVMInfo output missing %q:\n%s", want, text)
		}
	}
	jsonOutput := output.NewVMInfo(info)
	if jsonOutput.Kubernetes.Support != "supported" || jsonOutput.Kubernetes.Ready != "ready" {
		t.Fatalf("JSON Kubernetes = %#v", jsonOutput.Kubernetes)
	}
	if jsonOutput.Kubernetes.Kubeconfig != "/tmp/spind/vms/kind-work/kubeconfig" {
		t.Fatalf("JSON Kubernetes kubeconfig = %q", jsonOutput.Kubernetes.Kubeconfig)
	}
}

func TestParseExecCommand(t *testing.T) {
	cli, ctx, exitCode, handled := Parse([]string{"vm", "exec", "base", "--", "uname", "-a"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if ctx.Command() != "vm exec <vm-name> -- <command> [args...]" {
		t.Fatalf("ctx.Command() = %q, want vm exec <vm-name> -- <command> [args...]", ctx.Command())
	}
	vmName, command, err := vmexec.ParseCommand(cli.VM.Exec)
	if err != nil {
		t.Fatalf("vmexec.ParseCommand returned error: %v", err)
	}
	if vmName != "base" {
		t.Fatalf("vmName = %q, want base", vmName)
	}
	if len(command) != 2 || command[0] != "uname" || command[1] != "-a" {
		t.Fatalf("command = %#v, want uname -a", command)
	}
}

func TestParseExecCommandKeepsHyphenCommandArgs(t *testing.T) {
	cli, _, exitCode, handled := Parse([]string{"vm", "exec", "base", "--", "sh", "-lc", "echo hello"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	_, command, err := vmexec.ParseCommand(cli.VM.Exec)
	if err != nil {
		t.Fatalf("vmexec.ParseCommand returned error: %v", err)
	}
	if len(command) != 3 || command[0] != "sh" || command[1] != "-lc" || command[2] != "echo hello" {
		t.Fatalf("command = %#v, want sh -lc echo hello", command)
	}
}

func TestParseExecCommandRequiresSeparator(t *testing.T) {
	cli, _, exitCode, handled := Parse([]string{"vm", "exec", "base", "uname"}, io.Discard, io.Discard)
	if handled {
		t.Fatalf("parseCommandLine handled with exitCode=%d", exitCode)
	}
	if _, _, err := vmexec.ParseCommand(cli.VM.Exec); err == nil {
		t.Fatal("vmexec.ParseCommand returned nil error")
	}
}

func TestParseCommandLineHelpReturnsZero(t *testing.T) {
	_, _, exitCode, handled := Parse([]string{"--help"}, io.Discard, io.Discard)
	if !handled || exitCode != 0 {
		t.Fatalf("parseCommandLine handled=%v exitCode=%d, want handled exitCode=0", handled, exitCode)
	}
}

func TestRootCommandProvidesCobraCompletionCommand(t *testing.T) {
	var stdout bytes.Buffer
	cli := Options{}
	runtime := cliruntime.New(context.Background(), config.Config{}, strings.NewReader(""), &stdout, io.Discard, false)
	root := NewRoot(&cli, runtime)
	root.SetArgs([]string{"completion", "zsh", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("completion zsh --help returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "spind completion zsh") {
		t.Fatalf("completion help missing command usage:\n%s", stdout.String())
	}
}
