package up

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/store"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func TestLoadProjectUsesYAMLNameBeforeBasename(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "spind.yaml"), "name: custom\nimage: docker\nkind: true\nsetup:\n  - echo setup\n")
	chdir(t, root)

	project, err := loadProject(".")
	if err != nil {
		t.Fatalf("loadProject() error = %v", err)
	}
	if project.Name != "custom" || project.VMName != "custom" || project.ProvisioningName != "custom-provisioning" || project.SnapshotName != "custom-provisioned" {
		t.Fatalf("project names = %#v", project)
	}
	if project.Image != "docker" || !project.Kind || len(project.Setup) != 1 || project.Setup[0] != "echo setup" {
		t.Fatalf("project config = %#v", project)
	}
}

func TestLoadProjectDefaultsNameToBasenameAndImageToDocker(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sample")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "spind.yaml"), "setup: []\n")
	chdir(t, root)

	project, err := loadProject(".")
	if err != nil {
		t.Fatalf("loadProject() error = %v", err)
	}
	if project.Name != "sample" || project.Image != "docker" {
		t.Fatalf("project = %#v, want sample/docker", project)
	}
}

func TestValidateProjectVMRejectsDifferentProjectRoot(t *testing.T) {
	home := t.TempDir()
	cfg := config.Config{VMStore: filepath.Join(home, "vms")}
	vmDir := filepath.Join(cfg.VMStore, "kido")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := spindvm.WriteMetadata(vmDir, spindvm.Metadata{Name: "kido", Architecture: "x86_64", ProjectRoot: "/other"}); err != nil {
		t.Fatal(err)
	}

	err := validateProjectVM(cfg, "kido", "/current")
	if err == nil {
		t.Fatal("validateProjectVM() error = nil, want conflict")
	}
}

func TestStoreNameRejectsInvalidName(t *testing.T) {
	if err := storeName("project", "bad/name"); err == nil {
		t.Fatal("storeName() error = nil, want invalid name")
	} else if err.Error() == store.ErrInvalidName.Error() {
		t.Fatalf("storeName() error = %v, want contextual error", err)
	}
}

func TestRunSetupStepPrintsCommandOutputAndDuration(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{VMStore: filepath.Join(root, "vms")}
	project := project{Root: root, Name: "sample", ProvisioningName: "sample-provisioning"}
	var stdout, stderr bytes.Buffer

	err := runSetupStep(context.Background(), project, cfg, 1, "printf 'command output\\n'", &stdout, &stderr, stepStyle{})
	if err != nil {
		t.Fatalf("runSetupStep() error = %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "step#1: printf 'command output\\n'\ncommand output\nstep#1: Done. ") {
		t.Fatalf("stdout = %q, want command output between step start and done", output)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunSetupStepPrintsFailureBeforeReturningError(t *testing.T) {
	root := t.TempDir()
	cfg := config.Config{VMStore: filepath.Join(root, "vms")}
	project := project{Root: root, Name: "sample", ProvisioningName: "sample-provisioning"}
	var stdout, stderr bytes.Buffer

	err := runSetupStep(context.Background(), project, cfg, 2, "echo broken && exit 7", &stdout, &stderr, stepStyle{})
	if err == nil {
		t.Fatal("runSetupStep() error = nil, want failure")
	}
	output := stdout.String()
	if !strings.Contains(output, "step#2: echo broken && exit 7\nbroken\nstep#2: Failed. ") {
		t.Fatalf("stdout = %q, want failure after command output", output)
	}
	if !strings.Contains(output, "setup command failed \"echo broken && exit 7\": exit status 7") {
		t.Fatalf("stdout = %q, want error message in failure line", output)
	}
}

func TestStepStyleColorsLabelWhenEnabled(t *testing.T) {
	if got := (stepStyle{}).label(3); got != "step#3" {
		t.Fatalf("plain label = %q, want step#3", got)
	}
	if got := (stepStyle{color: true}).label(3); got != "\x1b[36mstep#3\x1b[0m" {
		t.Fatalf("colored label = %q, want ANSI cyan label", got)
	}
	if got := (stepStyle{color: true}).command("echo ok"); got != "\x1b[36mecho ok\x1b[0m" {
		t.Fatalf("colored command = %q, want ANSI cyan command", got)
	}
	if got := (stepStyle{color: true}).done("Done."); got != "\x1b[32mDone.\x1b[0m" {
		t.Fatalf("colored done = %q, want ANSI green done", got)
	}
	if got := (stepStyle{color: true}).failed("Failed."); got != "\x1b[31mFailed.\x1b[0m" {
		t.Fatalf("colored failed = %q, want ANSI red failed", got)
	}
	if got := (stepStyle{color: true}).duration("123ms"); got != "\x1b[90m123ms\x1b[0m" {
		t.Fatalf("colored duration = %q, want ANSI gray duration", got)
	}
	if got := (stepStyle{color: true}).err(os.ErrNotExist); got != "\x1b[31mfile does not exist\x1b[0m" {
		t.Fatalf("colored error = %q, want ANSI red error", got)
	}
}

func TestPrintShellExportsUsesFishSyntaxForFishShell(t *testing.T) {
	var stdout bytes.Buffer
	PrintShellExports(&stdout, spindvm.Info{
		DockerAvailable:          true,
		DockerEndpointURI:        "unix:///tmp/sample/docker.sock",
		KubernetesReady:          true,
		KubernetesKubeconfigPath: "/tmp/sample/kubeconfig",
	}, "/opt/homebrew/bin/fish")

	output := stdout.String()
	if !strings.Contains(output, "set -gx DOCKER_HOST 'unix:///tmp/sample/docker.sock'\n") {
		t.Fatalf("output = %q, want fish DOCKER_HOST export", output)
	}
	if !strings.Contains(output, "set -gx KUBECONFIG '/tmp/sample/kubeconfig'\n") {
		t.Fatalf("output = %q, want fish KUBECONFIG export", output)
	}
}

func TestPrintShellExportsUsesPOSIXSyntaxByDefault(t *testing.T) {
	var stdout bytes.Buffer
	PrintShellExports(&stdout, spindvm.Info{
		DockerAvailable:          true,
		DockerEndpointURI:        "unix:///tmp/sample/docker.sock",
		KubernetesReady:          true,
		KubernetesKubeconfigPath: "/tmp/sample/kubeconfig",
	}, "/bin/zsh")

	output := stdout.String()
	if !strings.Contains(output, "export DOCKER_HOST='unix:///tmp/sample/docker.sock'\n") {
		t.Fatalf("output = %q, want POSIX DOCKER_HOST export", output)
	}
	if !strings.Contains(output, "export KUBECONFIG='/tmp/sample/kubeconfig'\n") {
		t.Fatalf("output = %q, want POSIX KUBECONFIG export", output)
	}
}

func writeFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}
