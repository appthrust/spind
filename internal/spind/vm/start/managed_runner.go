package start

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	spindvz "github.com/suin/spind/swift/spind-vz"
)

var (
	swiftcPath   = "/usr/bin/swiftc"
	codesignPath = "/usr/bin/codesign"
	hostGOOS     = runtime.GOOS
)

func (m *Manager) PrepareManagedVirtualizationRunner(ctx context.Context) (string, error) {
	if hostGOOS != "darwin" {
		return "", fmt.Errorf("Virtualization.framework backend requires macOS, got %s", hostGOOS)
	}
	return m.ensureManagedVirtualizationRunner(ctx)
}

func (m *Manager) resolveVirtualizationRunner(ctx context.Context) (string, error) {
	if m.RunnerPath != "" {
		if !isExecutable(m.RunnerPath) {
			return "", fmt.Errorf("configured Swift runner %q is not executable", m.RunnerPath)
		}
		return m.RunnerPath, nil
	}
	if hostGOOS != "darwin" {
		return "", fmt.Errorf("Virtualization.framework backend requires macOS, got %s", hostGOOS)
	}
	return m.ensureManagedVirtualizationRunner(ctx)
}

func (m *Manager) ensureManagedVirtualizationRunner(ctx context.Context) (string, error) {
	root := filepath.Join(m.Home, "runners", "spind-vz")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create Swift runner cache: %w", err)
	}

	lockPath := filepath.Join(root, ".build.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return "", fmt.Errorf("open Swift runner build lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock Swift runner build: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	hash := managedVirtualizationRunnerHash()
	dir := filepath.Join(root, hash)
	runnerPath := filepath.Join(dir, "spind-vz")
	if isExecutable(runnerPath) {
		return runnerPath, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create Swift runner directory: %w", err)
	}

	sourcePath := filepath.Join(dir, "main.swift")
	entitlementsPath := filepath.Join(dir, "spind-vz.entitlements")
	if err := os.WriteFile(sourcePath, []byte(spindvz.MainSwift), 0o644); err != nil {
		return "", fmt.Errorf("write embedded Swift runner source: %w", err)
	}
	if err := os.WriteFile(entitlementsPath, []byte(spindvz.Entitlements), 0o644); err != nil {
		return "", fmt.Errorf("write embedded Swift runner entitlements: %w", err)
	}

	tmpPath := filepath.Join(dir, "spind-vz.tmp")
	_ = os.Remove(tmpPath)
	if err := runSwiftCompiler(ctx, sourcePath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := runAdHocCodesign(ctx, entitlementsPath, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("make managed Swift runner executable: %w", err)
	}
	if err := os.Rename(tmpPath, runnerPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("install managed Swift runner: %w", err)
	}
	return runnerPath, nil
}

func managedVirtualizationRunnerHash() string {
	const buildRecipe = "swiftc -O -framework Virtualization + codesign --force --sign -"
	sum := sha256.Sum256([]byte(spindvz.MainSwift + "\x00" + spindvz.Entitlements + "\x00" + buildRecipe))
	return hex.EncodeToString(sum[:])[:16]
}

func runSwiftCompiler(ctx context.Context, sourcePath string, outputPath string) error {
	if !isExecutable(swiftcPath) {
		return errors.New("Virtualization.framework backend requires Apple Swift compiler; install Xcode Command Line Tools with: xcode-select --install")
	}
	cmd := exec.CommandContext(ctx, swiftcPath, "-O", "-framework", "Virtualization", "-o", outputPath, sourcePath)
	cmd.Env = withoutEnv(os.Environ(), "SDKROOT", "DEVELOPER_DIR")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build managed Swift runner with swiftc: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func runAdHocCodesign(ctx context.Context, entitlementsPath string, runnerPath string) error {
	if !isExecutable(codesignPath) {
		return fmt.Errorf("codesign not found at %s", codesignPath)
	}
	cmd := exec.CommandContext(ctx, codesignPath, "--force", "--sign", "-", "--entitlements", entitlementsPath, runnerPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ad-hoc codesign managed Swift runner: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func withoutEnv(values []string, names ...string) []string {
	blocked := make(map[string]struct{}, len(names))
	for _, name := range names {
		blocked[name] = struct{}{}
	}
	filtered := values[:0]
	for _, value := range values {
		name, _, ok := strings.Cut(value, "=")
		if ok {
			if _, skip := blocked[name]; skip {
				continue
			}
		}
		filtered = append(filtered, value)
	}
	return filtered
}
