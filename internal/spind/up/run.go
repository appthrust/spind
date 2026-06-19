package up

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindimage "github.com/suin/spind/internal/spind/image"
	"github.com/suin/spind/internal/spind/requirements"
	spindsnapshot "github.com/suin/spind/internal/spind/snapshot"
	"github.com/suin/spind/internal/spind/store"
	vmstart "github.com/suin/spind/internal/spind/vm/start"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
	"gopkg.in/yaml.v3"
)

const (
	configFileName = "spind.yaml"
	defaultImage   = "docker"

	roleWork         = "work"
	roleProvisioning = "provisioning"
	roleProvisioned  = "provisioned"
)

type projectConfig struct {
	Name  string   `yaml:"name"`
	Image string   `yaml:"image"`
	K8s   string   `yaml:"k8s"`
	Setup []string `yaml:"setup"`
}

type project struct {
	Root             string
	ConfigPath       string
	Name             string
	Image            string
	K8s              string
	Setup            []string
	VMName           string
	ProvisioningName string
	SnapshotName     string
}

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	if err := run(ctx, cfg, options, stdout, stderr); err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	return 0
}

func run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) error {
	project, err := loadProject(".")
	if err != nil {
		return err
	}
	manager := vmstart.NewManagerFromConfig(cfg)
	requirementsResult := requirements.CheckVMStart(ctx, cfg, requirements.VMStartRequest{
		Image:        project.Image,
		FromSnapshot: true,
	})
	if _, err := os.Stat(filepath.Join(cfg.ImageStore, project.Image)); errors.Is(err, os.ErrNotExist) {
		requirementsResult = requirements.Merge(requirementsResult, requirements.CheckImageBuild(ctx, cfg, requirements.ImageBuildRequest{Name: project.Image}))
	}
	if err := requirements.CheckOrError(requirementsResult); err != nil {
		return err
	}

	if err := validateProjectObjects(cfg, project); err != nil {
		return err
	}
	if options.Reprovision {
		if err := deleteProjectVMIfExists(ctx, manager, cfg, project.VMName, project.Root); err != nil {
			return err
		}
		if err := deleteProjectSnapshotIfExists(cfg, project.SnapshotName, project.Root); err != nil {
			return err
		}
	}

	if _, err := os.Stat(filepath.Join(cfg.ImageStore, project.Image)); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check image %q: %w", project.Image, err)
		}
		result, err := manager.BuildImage(ctx, project.Image, spindimage.BuildOptions{})
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "built image %q (%s)\n", result.Name, output.FormatSize(result.SizeBytes))
	}

	if _, err := os.Stat(filepath.Join(cfg.SnapshotStore, project.SnapshotName)); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check snapshot %q: %w", project.SnapshotName, err)
		}
		if err := provision(ctx, manager, cfg, project, stdout, stderr); err != nil {
			return err
		}
	}

	if _, err := os.Stat(filepath.Join(cfg.VMStore, project.VMName)); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check VM %q: %w", project.VMName, err)
		}
		if err := manager.CreateFromSnapshot(ctx, project.VMName, project.SnapshotName, ""); err != nil {
			return err
		}
		if err := markVMProject(cfg, project.VMName, project, roleWork); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "created VM %q from snapshot %q\n", project.VMName, project.SnapshotName)
	}

	started, err := manager.Start(ctx, project.VMName)
	if err != nil {
		return err
	}
	if started {
		fmt.Fprintf(stdout, "started VM %q\n", project.VMName)
	} else {
		fmt.Fprintf(stdout, "VM %q is already running\n", project.VMName)
	}
	if info, err := manager.VMStatus(project.VMName); err == nil {
		output.PrintDockerStartLine(stdout, stderr, info)
		output.PrintKubernetesStartLine(stdout, stderr, info)
		output.PrintRegistryStartLine(stdout, stderr, info)
		PrintShellExports(stdout, info, os.Getenv("SHELL"))
	}
	return nil
}

func PrintShellExports(stdout io.Writer, info spindvm.Info, shellPath string) {
	values := shellExportValues(info)
	if len(values) == 0 {
		return
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "use in current shell:")
	if shellKind(shellPath) == "fish" {
		for _, value := range values {
			fmt.Fprintf(stdout, "set -gx %s %s\n", value.Name, fishQuote(value.Value))
		}
		return
	}
	for _, value := range values {
		fmt.Fprintf(stdout, "export %s=%s\n", value.Name, posixQuote(value.Value))
	}
}

type shellExportValue struct {
	Name  string
	Value string
}

func shellExportValues(info spindvm.Info) []shellExportValue {
	values := []shellExportValue{}
	if info.DockerAvailable && info.DockerEndpointURI != "" {
		values = append(values, shellExportValue{Name: "DOCKER_HOST", Value: info.DockerEndpointURI})
	}
	if info.KubernetesReady && info.KubernetesKubeconfigPath != "" {
		values = append(values, shellExportValue{Name: "KUBECONFIG", Value: info.KubernetesKubeconfigPath})
	}
	if info.RegistryReady && info.RegistryURL != "" {
		values = append(values, shellExportValue{Name: "REGISTRY", Value: info.RegistryURL})
	}
	return values
}

func shellKind(shellPath string) string {
	if filepath.Base(shellPath) == "fish" {
		return "fish"
	}
	return "posix"
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func fishQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "\\'")
	return "'" + value + "'"
}

func provision(ctx context.Context, manager *vmstart.Manager, cfg config.Config, project project, stdout io.Writer, stderr io.Writer) error {
	_ = deleteProjectVMIfExists(ctx, manager, cfg, project.ProvisioningName, project.Root)
	if err := manager.CreateFromImage(ctx, project.ProvisioningName, project.Image, ""); err != nil {
		return err
	}
	if err := markVMProject(cfg, project.ProvisioningName, project, roleProvisioning); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created provisioning VM %q from image %q\n", project.ProvisioningName, project.Image)

	if _, err := manager.Start(ctx, project.ProvisioningName); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "started provisioning VM %q\n", project.ProvisioningName)

	stepStyle := newStepStyle(stdout)
	for index, command := range project.Setup {
		if err := runSetupStep(ctx, project, cfg, index+1, command, stdout, stderr, stepStyle); err != nil {
			return err
		}
	}

	createOptions := spindsnapshot.CreateOptions{K8s: project.K8s}
	if project.K8s != "" {
		createOptions.KubeconfigPath = provisioningKubeconfigPath(cfg, project)
	}
	if err := manager.SnapshotCreateWithOptions(ctx, project.SnapshotName, project.ProvisioningName, createOptions); err != nil {
		return err
	}
	if err := markSnapshotProject(cfg, project.SnapshotName, project, roleProvisioned); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created snapshot %q from VM %q\n", project.SnapshotName, project.ProvisioningName)

	if _, err := manager.DeleteVM(ctx, project.ProvisioningName, spindvm.DeleteOptions{Force: true}); err != nil {
		return fmt.Errorf("delete provisioning VM %q: %w", project.ProvisioningName, err)
	}
	fmt.Fprintf(stdout, "deleted provisioning VM %q\n", project.ProvisioningName)
	return nil
}

func runSetupStep(ctx context.Context, project project, cfg config.Config, number int, command string, stdout io.Writer, stderr io.Writer, style stepStyle) error {
	startedAt := time.Now()
	fmt.Fprintf(stdout, "%s: %s\n", style.label(number), style.command(command))
	if err := runSetupCommand(ctx, project, cfg, command, stdout, stderr); err != nil {
		duration := output.FormatDuration(time.Since(startedAt))
		fmt.Fprintf(stdout, "%s: %s %s %s\n", style.label(number), style.failed("Failed."), style.duration(duration), style.err(err))
		return err
	}
	duration := output.FormatDuration(time.Since(startedAt))
	fmt.Fprintf(stdout, "%s: %s %s\n", style.label(number), style.done("Done."), style.duration(duration))
	return nil
}

func runSetupCommand(ctx context.Context, project project, cfg config.Config, command string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = project.Root
	dockerHost := "unix://" + filepath.Join(cfg.VMStore, project.ProvisioningName, "docker.sock")
	kubeconfig := provisioningKubeconfigPath(cfg, project)
	cmd.Env = append(os.Environ(),
		"DOCKER_HOST="+dockerHost,
		"KUBECONFIG="+kubeconfig,
		"SPIND_DOCKER_HOST="+dockerHost,
		"SPIND_KUBECONFIG="+kubeconfig,
		"SPIND_PROJECT_NAME="+project.Name,
		"SPIND_PROJECT_ROOT="+project.Root,
		"SPIND_VM_NAME="+project.ProvisioningName,
	)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("setup command failed %q: %w", command, err)
	}
	return nil
}

type stepStyle struct {
	color bool
}

func newStepStyle(stdout io.Writer) stepStyle {
	file, ok := stdout.(*os.File)
	if !ok {
		return stepStyle{}
	}
	info, err := file.Stat()
	if err != nil {
		return stepStyle{}
	}
	return stepStyle{color: info.Mode()&os.ModeCharDevice != 0}
}

func (s stepStyle) label(number int) string {
	label := fmt.Sprintf("step#%d", number)
	return s.colorize(label, "\x1b[36m")
}

func (s stepStyle) command(command string) string {
	return s.colorize(command, "\x1b[36m")
}

func (s stepStyle) done(value string) string {
	return s.colorize(value, "\x1b[32m")
}

func (s stepStyle) failed(value string) string {
	return s.colorize(value, "\x1b[31m")
}

func (s stepStyle) duration(value string) string {
	return s.colorize(value, "\x1b[90m")
}

func (s stepStyle) err(err error) string {
	return s.colorize(err.Error(), "\x1b[31m")
}

func (s stepStyle) colorize(value string, color string) string {
	if !s.color {
		return value
	}
	return color + value + "\x1b[0m"
}

func provisioningKubeconfigPath(cfg config.Config, project project) string {
	return filepath.Join(cfg.VMStore, project.ProvisioningName, "kubeconfig")
}

func loadProject(start string) (project, error) {
	root, configPath, err := findProjectRoot(start)
	if err != nil {
		return project{}, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return project{}, fmt.Errorf("read %s: %w", configPath, err)
	}
	var cfg projectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return project{}, fmt.Errorf("parse %s: %w", configPath, err)
	}
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	image := strings.TrimSpace(cfg.Image)
	if image == "" {
		image = defaultImage
	}
	if err := storeName("project", name); err != nil {
		return project{}, err
	}
	if err := storeName("image", image); err != nil {
		return project{}, err
	}
	k8s := strings.TrimSpace(cfg.K8s)
	if k8s != "" && k8s != "kind" && k8s != "k3d" {
		return project{}, fmt.Errorf("k8s %q: supported values are kind or k3d", k8s)
	}
	return project{
		Root:             root,
		ConfigPath:       configPath,
		Name:             name,
		Image:            image,
		K8s:              k8s,
		Setup:            cfg.Setup,
		VMName:           name,
		ProvisioningName: name + "-provisioning",
		SnapshotName:     name + "-provisioned",
	}, nil
}

func findProjectRoot(start string) (string, string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", "", fmt.Errorf("resolve current directory: %w", err)
	}
	for {
		path := filepath.Join(dir, configFileName)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return dir, path, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("check %s: %w", path, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", fmt.Errorf("%s not found", configFileName)
		}
		dir = parent
	}
}

func storeName(kind string, name string) error {
	if !store.ValidName(name) {
		return fmt.Errorf("%s name %q: %w", kind, name, store.ErrInvalidName)
	}
	return nil
}

func validateProjectObjects(cfg config.Config, project project) error {
	for _, name := range []string{project.VMName, project.ProvisioningName} {
		if err := validateProjectVM(cfg, name, project.Root); err != nil {
			return err
		}
	}
	return validateProjectSnapshot(cfg, project.SnapshotName, project.Root)
}

func validateProjectVM(cfg config.Config, name string, root string) error {
	vmDir := filepath.Join(cfg.VMStore, name)
	if _, err := os.Stat(vmDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check VM %q: %w", name, err)
	}
	metadata, err := spindvm.ReadMetadata(vmDir)
	if err != nil {
		return fmt.Errorf("VM %q already exists and is not managed by this project", name)
	}
	if metadata.ProjectRoot != root {
		return projectConflict("VM", name, metadata.ProjectRoot, root)
	}
	return nil
}

func validateProjectSnapshot(cfg config.Config, name string, root string) error {
	snapshotDir := filepath.Join(cfg.SnapshotStore, name)
	if _, err := os.Stat(snapshotDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check snapshot %q: %w", name, err)
	}
	var metadata spindsnapshot.Metadata
	if err := store.ReadJSON(filepath.Join(snapshotDir, spindsnapshot.MetadataName), &metadata); err != nil {
		return fmt.Errorf("snapshot %q already exists and is not managed by this project", name)
	}
	if metadata.ProjectRoot != root {
		return projectConflict("snapshot", name, metadata.ProjectRoot, root)
	}
	return nil
}

func projectConflict(kind string, name string, existingRoot string, currentRoot string) error {
	if existingRoot == "" {
		return fmt.Errorf("%s %q already exists and is not managed by this project", kind, name)
	}
	return fmt.Errorf("%s %q is already used by %s; current project root is %s; set a unique name in spind.yaml", kind, name, existingRoot, currentRoot)
}

func markVMProject(cfg config.Config, name string, project project, role string) error {
	vmDir := filepath.Join(cfg.VMStore, name)
	metadata, err := spindvm.ReadMetadata(vmDir)
	if err != nil {
		return fmt.Errorf("read VM metadata: %w", err)
	}
	metadata.ProjectName = project.Name
	metadata.ProjectRoot = project.Root
	metadata.ProjectRole = role
	if err := spindvm.WriteMetadata(vmDir, metadata); err != nil {
		return fmt.Errorf("write VM metadata: %w", err)
	}
	return nil
}

func markSnapshotProject(cfg config.Config, name string, project project, role string) error {
	path := filepath.Join(cfg.SnapshotStore, name, spindsnapshot.MetadataName)
	var metadata spindsnapshot.Metadata
	if err := store.ReadJSON(path, &metadata); err != nil {
		return fmt.Errorf("read snapshot metadata: %w", err)
	}
	metadata.ProjectName = project.Name
	metadata.ProjectRoot = project.Root
	metadata.ProjectRole = role
	if err := store.WriteJSON(path, metadata, 0o644); err != nil {
		return fmt.Errorf("write snapshot metadata: %w", err)
	}
	return nil
}

func deleteProjectVMIfExists(ctx context.Context, manager *vmstart.Manager, cfg config.Config, name string, root string) error {
	if err := validateProjectVM(cfg, name, root); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(cfg.VMStore, name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check VM %q: %w", name, err)
	}
	if _, err := manager.DeleteVM(ctx, name, spindvm.DeleteOptions{Force: true}); err != nil {
		return err
	}
	return nil
}

func deleteProjectSnapshotIfExists(cfg config.Config, name string, root string) error {
	if err := validateProjectSnapshot(cfg, name, root); err != nil {
		return err
	}
	store := spindsnapshot.Store{SnapshotStore: cfg.SnapshotStore}
	if err := store.Delete(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return err
	}
	return nil
}
