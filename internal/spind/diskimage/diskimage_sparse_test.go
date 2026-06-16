//go:build linux || darwin

package diskimage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCreateMBRDiskPreservesSparseRootFSHoles(t *testing.T) {
	dir := t.TempDir()
	rootfsPath := filepath.Join(dir, "rootfs.img")
	diskPath := filepath.Join(dir, "disk.img")

	rootfs, err := os.OpenFile(rootfsPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rootfs.WriteAt([]byte("rootfs-start"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := rootfs.WriteAt([]byte("rootfs-end"), 64*1024*1024-4096); err != nil {
		t.Fatal(err)
	}
	if err := rootfs.Truncate(64 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	if err := rootfs.Close(); err != nil {
		t.Fatal(err)
	}

	if err := CreateMBRDisk(diskPath, rootfsPath, 128*1024*1024, 0); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 128*1024*1024 {
		t.Fatalf("disk size = %d, want %d", info.Size(), 128*1024*1024)
	}
	allocated := allocatedBytes(t, diskPath)
	if allocated > 16*1024*1024 {
		t.Fatalf("allocated bytes = %d, want <= %d", allocated, 16*1024*1024)
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
