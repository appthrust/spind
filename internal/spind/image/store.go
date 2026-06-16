package image

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/suin/spind/internal/spind/filecopy"
	"github.com/suin/spind/internal/spind/store"
	dockertemplate "github.com/suin/spind/templates/docker"
)

const (
	MetadataName    = "metadata.json"
	KernelName      = "kernel"
	InitramfsName   = "initramfs"
	DiskName        = "disk.img"
	DefaultExecUser = "spind"
)

var GuestBinaryNames = []string{
	"spind-guest-agent",
}

type Store struct {
	Home                  string
	ImageStore            string
	TemplateStore         string
	BuiltinTemplateSource string
}

func ValidateName(name string) error {
	if !store.ValidName(name) {
		return fmt.Errorf("%q: %w", name, store.ErrInvalidName)
	}
	return nil
}

func (s Store) List() ([]Info, error) {
	entries, err := os.ReadDir(s.ImageStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read image store: %w", err)
	}
	images := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		images = append(images, Inspect(filepath.Join(s.ImageStore, entry.Name()), entry.Name()))
	}
	sort.Slice(images, func(i int, j int) bool {
		return images[i].Name < images[j].Name
	})
	return images, nil
}

func (s Store) Delete(name string, references []string, options DeleteOptions) (DeleteResult, error) {
	if err := ValidateName(name); err != nil {
		return DeleteResult{}, err
	}
	imageDir := filepath.Join(s.ImageStore, name)
	stat, err := os.Stat(imageDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeleteResult{}, fmt.Errorf("image %q: %w", name, store.ErrNotFound)
		}
		return DeleteResult{}, fmt.Errorf("check image %q: %w", name, err)
	}
	if !stat.IsDir() {
		return DeleteResult{}, fmt.Errorf("image %q is not a directory", name)
	}
	if len(references) > 0 && !options.Force {
		return DeleteResult{}, fmt.Errorf("image %q is referenced by VM(s): %s; delete those VMs first or use --force", name, strings.Join(references, ", "))
	}
	size, err := store.DirectorySize(imageDir)
	if err != nil {
		return DeleteResult{}, fmt.Errorf("size image %q: %w", name, err)
	}
	if err := os.RemoveAll(imageDir); err != nil {
		return DeleteResult{}, fmt.Errorf("delete image %q: %w", name, err)
	}
	return DeleteResult{SizeBytes: size, ReferencedVMs: references}, nil
}

func ReadMetadata(imageDir string) (Metadata, error) {
	var metadata Metadata
	if err := store.ReadJSON(filepath.Join(imageDir, MetadataName), &metadata); err != nil {
		return metadata, err
	}
	if metadata.Architecture == "" {
		metadata.Architecture = runtime.GOARCH
	}
	if metadata.ExecUser == "" {
		metadata.ExecUser = DefaultExecUser
	}
	return metadata, nil
}

func CopyRequiredFiles(imageDir string, vmDir string, image Metadata) error {
	for _, name := range RequiredFileNames(image) {
		src := filepath.Join(imageDir, name)
		dst := filepath.Join(vmDir, name)
		if err := filecopy.Copy(src, dst, 0o644); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("image file %q: %w", name, store.ErrNotFound)
			}
			return fmt.Errorf("copy image file %q: %w", name, err)
		}
	}
	return nil
}

func RequiredFileNames(image Metadata) []string {
	names := []string{KernelName, InitramfsName}
	if len(image.Disks) == 0 {
		return append(names, DiskName)
	}
	for _, disk := range image.Disks {
		names = append(names, disk.Name)
	}
	return names
}

func Inspect(imageDir string, name string) Info {
	info := Info{
		Name:     name,
		ImageDir: imageDir,
		Health:   "ok",
	}
	problems := []string{}

	metadata, err := ReadMetadata(imageDir)
	if err != nil {
		problems = append(problems, "read metadata: "+err.Error())
	} else {
		info.Architecture = metadata.Architecture
		info.CreatedAt = metadata.CreatedAt
		info.KernelCommandLine = metadata.KernelCommandLine
		info.ExecUser = metadata.ExecUser
	}

	requiredFiles := []string{MetadataName, KernelName, InitramfsName, DiskName}
	if err == nil {
		requiredFiles = append([]string{MetadataName}, RequiredFileNames(metadata)...)
	}
	for _, fileName := range requiredFiles {
		stat, err := os.Stat(filepath.Join(imageDir, fileName))
		switch {
		case err != nil:
			problems = append(problems, "missing "+fileName)
		case stat.IsDir():
			problems = append(problems, fileName+" is a directory")
		}
	}

	size, err := store.DirectorySize(imageDir)
	if err != nil {
		problems = append(problems, "size: "+err.Error())
	} else {
		info.SizeBytes = size
	}

	if len(problems) > 0 {
		info.Health = "unhealthy"
		info.HealthMessage = strings.Join(problems, "; ")
	}
	return info
}

func (s Store) Build(ctx context.Context, name string, options BuildOptions) (BuildResult, error) {
	if err := ValidateName(name); err != nil {
		return BuildResult{}, err
	}
	imageDir := filepath.Join(s.ImageStore, name)
	if _, err := os.Stat(imageDir); err == nil && !options.Force {
		return BuildResult{}, fmt.Errorf("image %q already exists; use --force to replace it", name)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return BuildResult{}, fmt.Errorf("check image %q: %w", name, err)
	}

	config := options.Config
	if config == "" {
		config = "template://" + name
	}
	templateDir, err := s.resolveImageBuildConfig(config)
	if err != nil {
		return BuildResult{}, err
	}

	buildRoot := filepath.Join(s.Home, "image-build")
	workDir := filepath.Join(buildRoot, name)
	templateWorkDir := filepath.Join(workDir, "template")
	builderWorkDir := filepath.Join(workDir, "builder")
	stagingDir := filepath.Join(workDir, "output", "image")
	runtime := s.imageBuilderRuntime()
	if err := clearImageBuildWorkDir(ctx, runtime, workDir); err != nil {
		return BuildResult{}, fmt.Errorf("clear image build directory: %w", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("create image build directory: %w", err)
	}
	if err := os.CopyFS(templateWorkDir, os.DirFS(templateDir)); err != nil {
		return BuildResult{}, fmt.Errorf("copy image template into build workspace: %w", err)
	}
	if err := copyImageBuilderAssets(builderWorkDir); err != nil {
		return BuildResult{}, err
	}

	if err := s.runContainerImageBuild(ctx, runtime, name, workDir, options); err != nil {
		return BuildResult{}, err
	}

	resultMetadata, err := ReadMetadata(stagingDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("read built image metadata: %w", err)
	}
	for _, fileName := range RequiredFileNames(resultMetadata) {
		src := filepath.Join(stagingDir, fileName)
		if _, err := os.Stat(src); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return BuildResult{}, fmt.Errorf("built image is missing required file %q", fileName)
			}
			return BuildResult{}, fmt.Errorf("check built image file %q: %w", fileName, err)
		}
	}

	createdAt := time.Now().UTC().Truncate(time.Second)
	resultMetadata.Name = name
	resultMetadata.CreatedAt = createdAt
	if err := store.WriteJSON(filepath.Join(stagingDir, MetadataName), resultMetadata, 0o644); err != nil {
		return BuildResult{}, fmt.Errorf("write image metadata: %w", err)
	}
	info := Inspect(stagingDir, name)
	if info.Health != "ok" {
		return BuildResult{}, fmt.Errorf("built image %q is unhealthy: %s", name, info.HealthMessage)
	}

	if err := os.MkdirAll(s.ImageStore, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("create image store: %w", err)
	}
	tmpImageDir := filepath.Join(s.ImageStore, "."+name+".tmp")
	_ = os.RemoveAll(tmpImageDir)
	if err := os.Rename(stagingDir, tmpImageDir); err != nil {
		return BuildResult{}, fmt.Errorf("stage image %q: %w", name, err)
	}
	if options.Force {
		if err := os.RemoveAll(imageDir); err != nil {
			_ = os.RemoveAll(tmpImageDir)
			return BuildResult{}, fmt.Errorf("replace image %q: %w", name, err)
		}
	}
	if err := os.Rename(tmpImageDir, imageDir); err != nil {
		_ = os.RemoveAll(tmpImageDir)
		return BuildResult{}, fmt.Errorf("install image %q: %w", name, err)
	}
	size, err := store.DirectorySize(imageDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("size image %q: %w", name, err)
	}
	return BuildResult{
		Name:         name,
		ImageDir:     imageDir,
		TemplatePath: templateDir,
		SizeBytes:    size,
		CreatedAt:    createdAt,
	}, nil
}

func (s Store) resolveImageBuildConfig(config string) (string, error) {
	if strings.HasPrefix(config, "template://") {
		name := strings.TrimPrefix(config, "template://")
		if err := ValidateName(name); err != nil {
			return "", fmt.Errorf("template %q: %w", name, err)
		}
		return s.resolveNamedImageTemplate(name)
	}
	if strings.HasPrefix(config, "file://") {
		parsed, err := url.Parse(config)
		if err != nil {
			return "", fmt.Errorf("parse image build config %q: %w", config, err)
		}
		return validateImageTemplateDir(parsed.Path)
	}
	if strings.HasPrefix(config, "http://") || strings.HasPrefix(config, "https://") || strings.HasPrefix(config, "github://") {
		return "", fmt.Errorf("image build config %q is not supported yet", config)
	}
	return validateImageTemplateDir(config)
}

func (s Store) resolveNamedImageTemplate(name string) (string, error) {
	templateStore := s.TemplateStore
	if templateStore == "" {
		templateStore = filepath.Join(s.Home, "templates")
	}
	templateDir := filepath.Join(templateStore, name)
	if path, err := validateImageTemplateDir(templateDir); err == nil {
		if name == "docker" && shouldRefreshBuiltinDockerTemplate(path) {
			if err := s.replaceBuiltinDockerTemplate(templateDir); err != nil {
				return "", err
			}
			return validateImageTemplateDir(templateDir)
		}
		return path, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if name != "docker" {
		return "", fmt.Errorf("template %q: %w", name, store.ErrNotFound)
	}
	if err := s.materializeBuiltinDockerTemplate(templateDir); err != nil {
		return "", err
	}
	return validateImageTemplateDir(templateDir)
}

func shouldRefreshBuiltinDockerTemplate(templateDir string) bool {
	if _, err := os.Stat(filepath.Join(templateDir, "template.json")); err == nil {
		return true
	}
	return false
}

func (s Store) replaceBuiltinDockerTemplate(templateDir string) error {
	newDir := templateDir + ".new"
	oldDir := templateDir + ".old"
	_ = os.RemoveAll(newDir)
	_ = os.RemoveAll(oldDir)
	if err := s.materializeBuiltinDockerTemplate(newDir); err != nil {
		return err
	}
	if err := os.Rename(templateDir, oldDir); err != nil {
		_ = os.RemoveAll(newDir)
		return fmt.Errorf("replace stale builtin template \"docker\": %w", err)
	}
	if err := os.Rename(newDir, templateDir); err != nil {
		_ = os.Rename(oldDir, templateDir)
		_ = os.RemoveAll(newDir)
		return fmt.Errorf("install refreshed builtin template \"docker\": %w", err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

func (s Store) materializeBuiltinDockerTemplate(templateDir string) error {
	sourceRoot := s.BuiltinTemplateSource
	if sourceRoot == "" {
		sourceRoot = FindDefaultTemplateSource()
	}
	var templateFS fs.FS
	if sourceRoot == "" {
		templateFS = dockertemplate.FS
	} else {
		var err error
		sourceRoot, err = normalizeBuiltinDockerTemplateSource(sourceRoot)
		if err != nil {
			return err
		}
	}
	guestBinaryDir := findGuestBinarySourceDir(sourceRoot)

	if err := os.MkdirAll(filepath.Dir(templateDir), 0o755); err != nil {
		return fmt.Errorf("create template store: %w", err)
	}
	tmpDir := templateDir + ".tmp"
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(filepath.Join(tmpDir, "guest-binaries"), 0o755); err != nil {
		return fmt.Errorf("create template directory: %w", err)
	}
	var err error
	if templateFS != nil {
		err = copyEmbeddedDockerTemplateSource(templateFS, tmpDir)
	} else {
		err = copyImageTemplateSource(sourceRoot, tmpDir)
	}
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return err
	}
	if guestBinaryDir != "" {
		for _, name := range GuestBinaryNames {
			if err := filecopy.Copy(filepath.Join(guestBinaryDir, name), filepath.Join(tmpDir, "guest-binaries", name), 0o755); err != nil {
				_ = os.RemoveAll(tmpDir)
				return fmt.Errorf("copy guest binary %s: %w", name, err)
			}
		}
	}
	if err := os.Rename(tmpDir, templateDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("install builtin template \"docker\": %w", err)
	}
	return nil
}

func validateImageTemplateDir(path string) (string, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("image build config %q is not a directory", path)
	}
	if _, err := os.Stat(filepath.Join(path, "flake.nix")); err != nil {
		return "", fmt.Errorf("image build config %q missing flake.nix: %w", path, err)
	}
	return path, nil
}

func copyImageBuilderAssets(path string) error {
	assets, err := fs.Sub(imageBuilderAssets, "builder")
	if err != nil {
		return fmt.Errorf("open image builder assets: %w", err)
	}
	if err := os.CopyFS(path, assets); err != nil {
		return fmt.Errorf("copy image builder assets: %w", err)
	}
	return nil
}

func clearImageBuildWorkDir(ctx context.Context, runtime builderRuntime, workDir string) error {
	err := os.RemoveAll(workDir)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	if restoreErr := runtime.RestoreWorkDirAccess(ctx, workDir); restoreErr != nil {
		return errors.Join(err, restoreErr)
	}
	if retryErr := os.RemoveAll(workDir); retryErr != nil {
		return retryErr
	}
	return nil
}

func (s Store) runContainerImageBuild(ctx context.Context, runtime builderRuntime, imageName string, workDir string, options BuildOptions) error {
	containerName, buildID, err := newImageBuilderContainerName(imageName)
	if err != nil {
		return err
	}
	labels := map[string]string{
		"io.spind.kind":         "image-builder",
		"io.spind.image":        imageName,
		"io.spind.build-id":     buildID,
		"io.spind.cache-volume": imageBuilderNixStoreVolume,
		"io.spind.runtime":      runtime.Name(),
	}
	if err := runtime.EnsureVolume(ctx, imageBuilderNixStoreVolume); err != nil {
		return err
	}
	defer func() {
		_ = runtime.RemoveContainer(context.Background(), containerName)
	}()
	env := map[string]string{}
	if version := guestAgentInstallVersion(); version != "" {
		env["SPIND_GUEST_AGENT_VERSION"] = version
	}
	_, err = runtime.Run(ctx, builderRunOptions{
		ContainerName: containerName,
		Image:         defaultImageBuilderRuntimeImage,
		WorkDir:       workDir,
		DataSize:      options.DataSize,
		Env:           env,
		Labels:        labels,
		Command: []string{
			"sh",
			"-lc",
			"nix --extra-experimental-features 'nix-command flakes' develop /work/builder#image-builder -c bash /work/builder/build-image.sh",
		},
	})
	return err
}

func (s Store) imageBuilderRuntime() builderRuntime {
	return dockerBuilderRuntime{}
}

func FindDefaultTemplateSource() string {
	candidates := []string{}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		for _, templateDir := range []string{filepath.Join(candidate, "templates", "docker"), candidate} {
			if _, err := validateImageTemplateDir(templateDir); err == nil {
				return templateDir
			}
		}
	}
	return ""
}

func normalizeBuiltinDockerTemplateSource(sourceRoot string) (string, error) {
	for _, candidate := range []string{sourceRoot, filepath.Join(sourceRoot, "templates", "docker")} {
		if path, err := validateImageTemplateDir(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("builtin template \"docker\" missing flake.nix in %s or %s", sourceRoot, filepath.Join(sourceRoot, "templates", "docker"))
}

func copyImageTemplateSource(sourceRoot string, targetRoot string) error {
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read builtin template file %q: %w", path, err)
		}
		relativePath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("resolve builtin template file %q: %w", path, err)
		}
		if relativePath == "." {
			return nil
		}
		if relativePath == "guest-binaries" {
			return fs.SkipDir
		}
		if entry.Name() == "result" {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		targetPath := filepath.Join(targetRoot, relativePath)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat builtin template file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create builtin template directory: %w", err)
		}
		if err := filecopy.Copy(path, targetPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy builtin template file %q: %w", relativePath, err)
		}
		return nil
	})
}

func copyEmbeddedDockerTemplateSource(source fs.FS, targetRoot string) error {
	return fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read embedded builtin template file %q: %w", path, err)
		}
		if path == "." {
			return nil
		}
		targetPath := filepath.Join(targetRoot, path)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat embedded builtin template file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create builtin template directory: %w", err)
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return fmt.Errorf("read embedded builtin template file %q: %w", path, err)
		}
		if err := os.WriteFile(targetPath, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy embedded builtin template file %q: %w", path, err)
		}
		return nil
	})
}

func findGuestBinarySourceDir(sourceRoot string) string {
	candidates := []string{}
	if sourceRoot != "" {
		candidates = append(candidates,
			filepath.Join(sourceRoot, "guest-binaries"),
			filepath.Join(sourceRoot, "bin"),
			filepath.Clean(filepath.Join(sourceRoot, "..", "..", "bin")),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "bin"))
	}
	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		candidates = append(candidates, dir, filepath.Dir(dir))
	}
	seen := map[string]bool{}
	for _, dir := range candidates {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		missing := ""
		for _, name := range GuestBinaryNames {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				missing = name
				break
			}
		}
		if missing == "" {
			return dir
		}
	}
	return ""
}
