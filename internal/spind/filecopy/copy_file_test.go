//go:build linux || darwin

package filecopy

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyFilePreservesSparseHoles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.img")
	dst := filepath.Join(dir, "dst.img")

	file, err := os.OpenFile(src, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("start"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("end"), 128*1024*1024-4096); err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(128 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	if info.Size() != 128*1024*1024 {
		t.Fatalf("size = %d, want %d", info.Size(), 128*1024*1024)
	}
	allocated := allocatedBytes(t, dst)
	if allocated > 16*1024*1024 {
		t.Fatalf("allocated bytes = %d, want <= %d", allocated, 16*1024*1024)
	}

	data := make([]byte, 3)
	read, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	if _, err := read.ReadAt(data, 128*1024*1024-4096); err != nil {
		t.Fatal(err)
	}
	if string(data) != "end" {
		t.Fatalf("tail data = %q, want end", string(data))
	}
}

func TestCopyReadOnlySourceUsesRequestedMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("readonly"), 0o444); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v, want 0644", info.Mode().Perm())
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "readonly" {
		t.Fatalf("content = %q, want readonly", string(data))
	}
}

func allocatedBytes(t *testing.T, path string) int64 {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Blocks * 512
}
