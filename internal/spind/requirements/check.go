package requirements

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/suin/spind/internal/spind/config"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func CheckVMStart(ctx context.Context, cfg config.Config, request VMStartRequest) Result {
	_ = ctx
	backend := request.Backend
	if backend == "" {
		backend = defaultBackend()
	}
	checks := []Check{{
		ID:       "host.os",
		Label:    "host operating system",
		Severity: SeverityInfo,
		Status:   StatusOK,
		Message:  runtime.GOOS,
	}}

	switch backend {
	case spindvm.BackendCloudHypervisor:
		checks = append(checks, checkCloudHypervisor(cfg, request)...)
	case spindvm.BackendVirtualizationFramework:
		checks = append(checks, checkVirtualizationFramework(cfg)...)
	default:
		checks = append(checks, Check{
			ID:       "backend.supported",
			Label:    "VM backend",
			Severity: SeverityRequired,
			Status:   StatusFailed,
			Message:  "unsupported backend " + backend,
		})
	}
	return Result{Checks: checks}
}

func CheckImageBuild(ctx context.Context, cfg config.Config, request ImageBuildRequest) Result {
	_ = ctx
	_ = cfg
	_ = request
	checks := []Check{{
		ID:       "image-build.docker",
		Label:    "Docker CLI",
		Severity: SeverityRequired,
		Status:   StatusOK,
		Message:  "docker",
	}}
	if _, err := exec.LookPath("docker"); err != nil {
		checks[0].Status = StatusMissing
		checks[0].Message = "docker binary not found"
		checks[0].Fix = "install Docker and make docker available in PATH"
	}
	return Result{Checks: checks}
}

func CheckDoctor(ctx context.Context, cfg config.Config) Result {
	checks := []Check{}
	checks = append(checks, Check{
		ID:       "host.os",
		Label:    "host operating system",
		Severity: SeverityInfo,
		Status:   StatusOK,
		Message:  runtime.GOOS,
	})
	checks = append(checks, Check{
		ID:       "backend.default",
		Label:    "default VM backend",
		Severity: SeverityInfo,
		Status:   StatusOK,
		Message:  defaultBackend(),
	})
	vmResult := CheckVMStart(ctx, cfg, VMStartRequest{Backend: defaultBackend(), Image: "docker"})
	for _, check := range vmResult.Checks {
		if check.ID == "host.os" {
			continue
		}
		checks = append(checks, check)
	}
	imageResult := CheckImageBuild(ctx, cfg, ImageBuildRequest{Name: "docker"})
	checks = append(checks, imageResult.Checks...)
	return Result{Checks: checks}
}

func CheckOrError(result Result) error {
	return result.FatalError()
}

func checkCloudHypervisor(cfg config.Config, request VMStartRequest) []Check {
	checks := []Check{}
	checks = append(checks, Check{
		ID:       "cloud-hypervisor.os",
		Label:    "Linux host",
		Severity: SeverityRequired,
		Status:   statusFor(runtime.GOOS == "linux"),
		Message:  hostOSMessage(runtime.GOOS == "linux", "Cloud Hypervisor backend requires Linux"),
	})
	checks = append(checks, checkPathExists("cloud-hypervisor.kvm", "/dev/kvm", SeverityRequired, "Cloud Hypervisor backend requires /dev/kvm", "run on a host with KVM available"))

	cloudHypervisorPath := cfg.CloudHypervisorPath
	if cloudHypervisorPath == "" {
		cloudHypervisorPath = config.FindExecutable("cloud-hypervisor")
	}
	checks = append(checks, checkExecutablePath("cloud-hypervisor.binary", "cloud-hypervisor", cloudHypervisorPath, SeverityRequired, "cloud-hypervisor binary not found", "install cloud-hypervisor or set SPIND_CLOUD_HYPERVISOR"))

	if dockerHostImage(request.Image) {
		passtPath := config.FirstEnv("SPIND_PASST", "KIDO_PASST")
		if passtPath == "" {
			passtPath = config.FindExecutable("passt")
		}
		severity := SeverityWarning
		if request.FromSnapshot {
			severity = SeverityRequired
		}
		checks = append(checks, checkExecutablePath("cloud-hypervisor.passt", "passt", passtPath, severity, "passt binary not found", "install passt or set SPIND_PASST"))

		virtiofsdPath := config.FindExecutable("virtiofsd")
		checks = append(checks, checkExecutablePath("cloud-hypervisor.virtiofsd", "virtiofsd", virtiofsdPath, SeverityWarning, "virtiofsd binary not found", "install virtiofsd to enable host path sharing"))
	}
	return checks
}

func checkVirtualizationFramework(cfg config.Config) []Check {
	checks := []Check{}
	checks = append(checks, Check{
		ID:       "virtualization-framework.os",
		Label:    "macOS host",
		Severity: SeverityRequired,
		Status:   statusFor(runtime.GOOS == "darwin"),
		Message:  hostOSMessage(runtime.GOOS == "darwin", "Virtualization.framework backend requires macOS"),
	})
	if cfg.RunnerPath != "" {
		checks = append(checks, checkExecutablePath("virtualization-framework.runner", "spind-vz runner", cfg.RunnerPath, SeverityRequired, "configured Swift runner is not executable", "set SPIND_VZ_RUNNER to an executable spind-vz runner"))
		return checks
	}
	checks = append(checks, checkExecutablePath("virtualization-framework.swiftc", "Apple Swift compiler", "/usr/bin/swiftc", SeverityRequired, "Apple Swift compiler not found", "install Xcode Command Line Tools with: xcode-select --install"))
	checks = append(checks, checkExecutablePath("virtualization-framework.codesign", "codesign", "/usr/bin/codesign", SeverityRequired, "codesign not found", "install Xcode Command Line Tools with: xcode-select --install"))
	return checks
}

func checkPathExists(id string, path string, severity Severity, missingMessage string, fix string) Check {
	check := Check{
		ID:       id,
		Label:    path,
		Severity: severity,
		Status:   StatusOK,
		Message:  path,
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			check.Status = StatusMissing
			check.Message = missingMessage
			check.Fix = fix
			return check
		}
		check.Status = StatusFailed
		check.Message = "check " + path + ": " + err.Error()
		check.Fix = fix
	}
	return check
}

func checkExecutablePath(id string, label string, path string, severity Severity, missingMessage string, fix string) Check {
	check := Check{
		ID:       id,
		Label:    label,
		Severity: severity,
		Status:   StatusOK,
		Message:  path,
	}
	if path == "" {
		check.Status = StatusMissing
		check.Message = missingMessage
		check.Fix = fix
		return check
	}
	info, err := os.Stat(path)
	if err != nil {
		check.Status = StatusMissing
		check.Message = missingMessage
		check.Fix = fix
		return check
	}
	if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		check.Status = StatusFailed
		check.Message = path + " is not executable"
		check.Fix = fix
		return check
	}
	return check
}

func statusFor(ok bool) Status {
	if ok {
		return StatusOK
	}
	return StatusFailed
}

func hostOSMessage(ok bool, failure string) string {
	if ok {
		return runtime.GOOS
	}
	return failure
}

func defaultBackend() string {
	switch runtime.GOOS {
	case "linux":
		return spindvm.BackendCloudHypervisor
	case "darwin":
		return spindvm.BackendVirtualizationFramework
	default:
		return "unsupported"
	}
}

func dockerHostImage(image string) bool {
	return strings.Contains(image, "docker")
}
