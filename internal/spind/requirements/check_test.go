package requirements

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/suin/spind/internal/spind/config"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func TestCheckImageBuildRequiresDockerCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	result := CheckImageBuild(context.Background(), config.Config{}, ImageBuildRequest{Name: "docker"})
	check := findCheck(t, result, "image-build.docker")
	if check.Status != StatusMissing || check.Severity != SeverityRequired {
		t.Fatalf("docker check = %#v, want required missing", check)
	}
	if result.FatalError() == nil {
		t.Fatal("FatalError() = nil, want missing docker error")
	}
}

func TestCheckCloudHypervisorPasstSeverityDependsOnSnapshotRestore(t *testing.T) {
	cloudHypervisor := writeExecutable(t, "cloud-hypervisor")
	t.Setenv("PATH", t.TempDir())

	normal := CheckVMStart(context.Background(), config.Config{CloudHypervisorPath: cloudHypervisor}, VMStartRequest{
		Backend: spindvm.BackendCloudHypervisor,
		Image:   "docker",
	})
	normalPasst := findCheck(t, normal, "cloud-hypervisor.passt")
	if normalPasst.Severity != SeverityWarning {
		t.Fatalf("normal passt severity = %q, want warning", normalPasst.Severity)
	}

	restore := CheckVMStart(context.Background(), config.Config{CloudHypervisorPath: cloudHypervisor}, VMStartRequest{
		Backend:      spindvm.BackendCloudHypervisor,
		Image:        "docker",
		FromSnapshot: true,
	})
	restorePasst := findCheck(t, restore, "cloud-hypervisor.passt")
	if restorePasst.Severity != SeverityRequired {
		t.Fatalf("restore passt severity = %q, want required", restorePasst.Severity)
	}
}

func findCheck(t *testing.T, result Result, id string) Check {
	t.Helper()
	for _, check := range result.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %#v", id, result.Checks)
	return Check{}
}

func writeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
