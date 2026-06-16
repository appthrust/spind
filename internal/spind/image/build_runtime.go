package image

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
)

const (
	defaultImageBuilderRuntimeImage = "nixos/nix:latest"
	imageBuilderContainerPrefix     = "spind-image-builder-"
	imageBuilderNixStoreVolume      = "spind-image-builder-nix-store"
)

type builderRuntime interface {
	Name() string
	EnsureVolume(ctx context.Context, name string) error
	Run(ctx context.Context, options builderRunOptions) (builderRunResult, error)
	RestoreWorkDirAccess(ctx context.Context, path string) error
	RemoveContainer(ctx context.Context, name string) error
}

type builderRunOptions struct {
	ContainerName string
	Image         string
	WorkDir       string
	DataSize      string
	Env           map[string]string
	Labels        map[string]string
	Command       []string
}

type builderRunResult struct {
	Stdout []byte
	Stderr []byte
}

type dockerBuilderRuntime struct{}

func (dockerBuilderRuntime) Name() string {
	return "docker"
}

func (dockerBuilderRuntime) EnsureVolume(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "volume", "create", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create Docker volume %q: %w\n%s", name, err, bytes.TrimSpace(output))
	}
	return nil
}

func (dockerBuilderRuntime) Run(ctx context.Context, options builderRunOptions) (builderRunResult, error) {
	args := []string{
		"run",
		"--rm",
		"--name", options.ContainerName,
		"-e", fmt.Sprintf("SPIND_HOST_UID=%d", os.Getuid()),
		"-e", fmt.Sprintf("SPIND_HOST_GID=%d", os.Getgid()),
		"-v", filepath.Clean(options.WorkDir) + ":/work",
		"-v", imageBuilderNixStoreVolume + ":/nix",
		"-w", "/work/builder",
	}
	if options.DataSize != "" {
		args = append(args, "-e", "SPIND_DATA_SIZE="+options.DataSize)
	}
	envKeys := make([]string, 0, len(options.Env))
	for key := range options.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		args = append(args, "-e", key+"="+options.Env[key])
	}
	for key, value := range options.Labels {
		args = append(args, "--label", key+"="+value)
	}
	args = append(args, options.Image)
	args = append(args, options.Command...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := builderRunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		return result, fmt.Errorf("run Docker image builder %q: %w\n%s%s", options.ContainerName, err, trimOutput(stdout.Bytes()), trimOutput(stderr.Bytes()))
	}
	return result, nil
}

func (dockerBuilderRuntime) RestoreWorkDirAccess(ctx context.Context, path string) error {
	args := []string{
		"run",
		"--rm",
		"-e", fmt.Sprintf("SPIND_HOST_UID=%d", os.Getuid()),
		"-e", fmt.Sprintf("SPIND_HOST_GID=%d", os.Getgid()),
		"-v", filepath.Clean(path) + ":/work",
		defaultImageBuilderRuntimeImage,
		"sh",
		"-lc",
		`chown -R "$SPIND_HOST_UID:$SPIND_HOST_GID" /work && chmod -R u+rwX /work`,
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore Docker image build directory access %q: %w\n%s", path, err, bytes.TrimSpace(output))
	}
	return nil
}

func (dockerBuilderRuntime) RemoveContainer(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove Docker image builder container %q: %w\n%s", name, err, bytes.TrimSpace(output))
	}
	return nil
}

func trimOutput(output []byte) string {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return ""
	}
	return string(output) + "\n"
}

func newImageBuilderContainerName(imageName string) (string, string, error) {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", fmt.Errorf("generate image builder id: %w", err)
	}
	buildID := hex.EncodeToString(random[:])
	return imageBuilderContainerPrefix + imageName + "-" + buildID, buildID, nil
}

func guestAgentInstallVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}
