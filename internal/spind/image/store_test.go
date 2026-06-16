package image

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	dockertemplate "github.com/suin/spind/templates/docker"
)

func TestCopyEmbeddedDockerTemplateSource(t *testing.T) {
	targetRoot := t.TempDir()

	if err := copyEmbeddedDockerTemplateSource(dockertemplate.FS, targetRoot); err != nil {
		t.Fatalf("copyEmbeddedDockerTemplateSource returned error: %v", err)
	}
	for _, name := range []string{"flake.nix", "flake.lock"} {
		if _, err := os.Stat(filepath.Join(targetRoot, name)); err != nil {
			t.Fatalf("expected embedded template file %s: %v", name, err)
		}
	}
	if _, err := validateImageTemplateDir(targetRoot); err != nil {
		t.Fatalf("embedded template copy is not valid: %v", err)
	}
}

func TestMaterializeBuiltinDockerTemplateDoesNotRequireHostGuestAgent(t *testing.T) {
	previousWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWorkingDir); err != nil {
			t.Fatal(err)
		}
	})
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "flake.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	store := Store{
		Home:                  home,
		TemplateStore:         filepath.Join(home, "templates"),
		BuiltinTemplateSource: sourceRoot,
	}
	templateDir := filepath.Join(store.TemplateStore, "docker")

	if err := store.materializeBuiltinDockerTemplate(templateDir); err != nil {
		t.Fatalf("materializeBuiltinDockerTemplate returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(templateDir, "flake.nix")); err != nil {
		t.Fatalf("expected materialized flake.nix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(templateDir, "guest-binaries", "spind-guest-agent")); !os.IsNotExist(err) {
		t.Fatalf("host guest agent should not be required or copied, stat error = %v", err)
	}
}

func TestBuildRestoresWorkDirAccessBeforeClearing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permission simulation is Unix-specific")
	}
	if os.Getuid() == 0 {
		t.Skip("permission simulation requires a non-root user")
	}

	logPath := filepath.Join(t.TempDir(), "docker.log")
	installFakeDocker(t, logPath)

	templateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateDir, "flake.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	imageStore := filepath.Join(home, "images")
	workDir := filepath.Join(home, "image-build", "docker")
	staleBuildDir := filepath.Join(workDir, "build")
	if err := os.MkdirAll(staleBuildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleBuildDir, "result"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staleBuildDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(staleBuildDir, 0o700)
		_ = os.Chmod(workDir, 0o700)
	})

	store := Store{
		Home:       home,
		ImageStore: imageStore,
	}
	result, err := store.Build(context.Background(), "docker", BuildOptions{Config: templateDir})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if result.Name != "docker" {
		t.Fatalf("Build result name = %q, want docker", result.Name)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	if !strings.Contains(log, "restore "+workDir+"\n") {
		t.Fatalf("fake docker log missing ownership restore for %s:\n%s", workDir, log)
	}
	if !strings.Contains(log, "build "+workDir+"\n") {
		t.Fatalf("fake docker log missing image build for %s:\n%s", workDir, log)
	}
}

func installFakeDocker(t *testing.T, logPath string) {
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
case "$command" in
  volume)
    if [ "${1:-}" = "create" ]; then
      echo "${2:-}"
      exit 0
    fi
    ;;
  rm)
    exit 0
    ;;
  run)
    work=""
    data_size=""
    build_run=0
    restores_mode=0
    while [ "$#" -gt 0 ]; do
      case "$1" in
        *chmod*)
          restores_mode=1
          ;;
      esac
      case "$1" in
        -v)
          shift
          mount="$1"
          case "$mount" in
            *:/work)
              work="${mount%:/work}"
              ;;
          esac
          ;;
        -w)
          shift
          if [ "$1" = "/work/builder" ]; then
            build_run=1
          fi
          ;;
        -e)
          shift
          case "$1" in
            SPIND_DATA_SIZE=*)
              data_size="${1#SPIND_DATA_SIZE=}"
              ;;
          esac
          ;;
        --name|--label)
          shift
          ;;
      esac
      shift || true
    done
    if [ -z "$work" ]; then
      echo "missing /work mount" >&2
      exit 1
    fi
    if [ "$build_run" -eq 0 ]; then
      if [ "$restores_mode" -eq 1 ]; then
        chmod -R u+rwx "$work" 2>/dev/null || true
      fi
      printf 'restore %s\n' "$work" >> "$SPIND_FAKE_DOCKER_LOG"
      exit 0
    fi
    printf 'build %s\n' "$work" >> "$SPIND_FAKE_DOCKER_LOG"
    out="$work/output/image"
    ;;
  *)
    echo "unexpected docker command: $command" >&2
    exit 1
    ;;
esac
if [ -z "${out:-}" ]; then
  echo "missing output path" >&2
  exit 1
fi
mkdir -p "$out"
printf 'kernel' > "$out/kernel"
printf 'initramfs' > "$out/initramfs"
case "$data_size" in
  "")
    printf 'disk' > "$out/disk.img"
    ;;
  *)
    truncate -s "$data_size" "$out/disk.img"
    ;;
esac
cat > "$out/metadata.json" <<'JSON'
{
  "name": "docker",
  "imageType": "microvm-nix",
  "architecture": "amd64",
  "kernelCommandLine": "console=ttyS0",
  "execUser": "spind"
}
JSON
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(dockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPIND_FAKE_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
