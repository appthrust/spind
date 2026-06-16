package start

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveVirtualizationRunnerUsesConfiguredRunner(t *testing.T) {
	manager := newTestManager(t)
	manager.RunnerPath = writeFakeRunner(t)

	runnerPath, err := manager.resolveVirtualizationRunner(context.Background())
	if err != nil {
		t.Fatalf("resolveVirtualizationRunner returned error: %v", err)
	}
	if runnerPath != manager.RunnerPath {
		t.Fatalf("runnerPath = %q, want configured %q", runnerPath, manager.RunnerPath)
	}
}

func TestResolveVirtualizationRunnerRejectsNonExecutableConfiguredRunner(t *testing.T) {
	manager := newTestManager(t)
	manager.RunnerPath = filepath.Join(t.TempDir(), "runner")
	if err := os.WriteFile(manager.RunnerPath, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := manager.resolveVirtualizationRunner(context.Background())
	if err == nil || !strings.Contains(err.Error(), "is not executable") {
		t.Fatalf("resolveVirtualizationRunner error = %v, want non-executable error", err)
	}
}

func TestEnsureManagedVirtualizationRunnerBuildsAndReusesCachedRunner(t *testing.T) {
	restore := overrideManagedRunnerTools(t)
	defer restore()

	manager := newTestManager(t)
	runnerPath, err := manager.resolveVirtualizationRunner(context.Background())
	if err != nil {
		t.Fatalf("resolveVirtualizationRunner returned error: %v", err)
	}
	if !strings.HasPrefix(runnerPath, filepath.Join(manager.Home, "runners", "spind-vz")) {
		t.Fatalf("runnerPath = %q, want managed cache under home", runnerPath)
	}
	if !isExecutable(runnerPath) {
		t.Fatalf("runnerPath %q is not executable", runnerPath)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(runnerPath), "main.swift")); err != nil {
		t.Fatalf("embedded source was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(runnerPath), "spind-vz.entitlements")); err != nil {
		t.Fatalf("embedded entitlements were not materialized: %v", err)
	}

	secondPath, err := manager.resolveVirtualizationRunner(context.Background())
	if err != nil {
		t.Fatalf("second resolveVirtualizationRunner returned error: %v", err)
	}
	if secondPath != runnerPath {
		t.Fatalf("secondPath = %q, want cached %q", secondPath, runnerPath)
	}
}

func TestPrepareManagedVirtualizationRunnerIgnoresConfiguredRunner(t *testing.T) {
	restore := overrideManagedRunnerTools(t)
	defer restore()

	manager := newTestManager(t)
	manager.RunnerPath = writeFakeRunner(t)

	runnerPath, err := manager.PrepareManagedVirtualizationRunner(context.Background())
	if err != nil {
		t.Fatalf("PrepareManagedVirtualizationRunner returned error: %v", err)
	}
	if runnerPath == manager.RunnerPath {
		t.Fatalf("runnerPath = configured runner %q, want managed runner", runnerPath)
	}
	if !strings.HasPrefix(runnerPath, filepath.Join(manager.Home, "runners", "spind-vz")) {
		t.Fatalf("runnerPath = %q, want managed cache under home", runnerPath)
	}
}

func TestEnsureManagedVirtualizationRunnerReportsMissingSwiftCompiler(t *testing.T) {
	oldSwiftcPath, oldHostGOOS := swiftcPath, hostGOOS
	swiftcPath = filepath.Join(t.TempDir(), "missing-swiftc")
	hostGOOS = "darwin"
	defer func() {
		swiftcPath = oldSwiftcPath
		hostGOOS = oldHostGOOS
	}()

	manager := newTestManager(t)
	_, err := manager.resolveVirtualizationRunner(context.Background())
	if err == nil || !strings.Contains(err.Error(), "xcode-select --install") {
		t.Fatalf("resolveVirtualizationRunner error = %v, want Xcode Command Line Tools guidance", err)
	}
}

func TestResolveVirtualizationRunnerRejectsNonDarwinManagedRunner(t *testing.T) {
	oldHostGOOS := hostGOOS
	hostGOOS = "linux"
	defer func() {
		hostGOOS = oldHostGOOS
	}()

	manager := newTestManager(t)
	_, err := manager.resolveVirtualizationRunner(context.Background())
	if err == nil || !strings.Contains(err.Error(), "requires macOS") {
		t.Fatalf("resolveVirtualizationRunner error = %v, want macOS error", err)
	}
}

func TestWithoutEnvRemovesNamedVariables(t *testing.T) {
	got := withoutEnv([]string{"A=1", "SDKROOT=/tmp/sdk", "B=2", "DEVELOPER_DIR=/tmp/dev"}, "SDKROOT", "DEVELOPER_DIR")
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("withoutEnv length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("withoutEnv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func overrideManagedRunnerTools(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	swiftc := filepath.Join(dir, "swiftc")
	codesign := filepath.Join(dir, "codesign")
	swiftcScript := `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then
    shift
    out="$1"
  fi
  shift
done
if [ -z "$out" ]; then
  echo "missing -o" >&2
  exit 1
fi
printf '#!/bin/sh\necho managed-runner\n' > "$out"
chmod +x "$out"
`
	if err := os.WriteFile(swiftc, []byte(swiftcScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codesign, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldSwiftcPath, oldCodesignPath, oldHostGOOS := swiftcPath, codesignPath, hostGOOS
	swiftcPath = swiftc
	codesignPath = codesign
	hostGOOS = "darwin"
	return func() {
		swiftcPath = oldSwiftcPath
		codesignPath = oldCodesignPath
		hostGOOS = oldHostGOOS
	}
}

func TestRunSwiftCompilerPropagatesCompilerFailure(t *testing.T) {
	dir := t.TempDir()
	swiftc := filepath.Join(dir, "swiftc")
	if err := os.WriteFile(swiftc, []byte("#!/bin/sh\necho boom >&2\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSwiftcPath := swiftcPath
	swiftcPath = swiftc
	defer func() {
		swiftcPath = oldSwiftcPath
	}()

	err := runSwiftCompiler(context.Background(), filepath.Join(dir, "main.swift"), filepath.Join(dir, "runner"))
	if err == nil {
		t.Fatal("runSwiftCompiler returned nil, want error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("runSwiftCompiler error = %v, want compiler output", err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		t.Fatalf("runSwiftCompiler returned raw path error: %v", err)
	}
}
