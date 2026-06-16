//go:build linux || darwin

package start

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/suin/spind/internal/spind/diskimage"
)

func TestCopyDiskPartitionPreservesSparseHoles(t *testing.T) {
	dir := t.TempDir()
	diskPath := filepath.Join(dir, "disk.img")
	rootfsPath := filepath.Join(dir, "rootfs.img")

	disk, err := os.OpenFile(diskPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	partition := diskimage.Partition{Start: 1024 * 1024, Size: 128 * 1024 * 1024}
	if _, err := disk.WriteAt([]byte("rootfs-start"), partition.Start); err != nil {
		t.Fatal(err)
	}
	if _, err := disk.WriteAt([]byte("rootfs-end"), partition.Start+partition.Size-4096); err != nil {
		t.Fatal(err)
	}
	if err := disk.Truncate(partition.Start + partition.Size); err != nil {
		t.Fatal(err)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}

	if err := copyDiskPartition(diskPath, rootfsPath, partition); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(rootfsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != partition.Size {
		t.Fatalf("rootfs size = %d, want %d", info.Size(), partition.Size)
	}
	allocated := allocatedBytesForPartitionTest(t, rootfsPath)
	if allocated > 16*1024*1024 {
		t.Fatalf("allocated bytes = %d, want <= %d", allocated, 16*1024*1024)
	}
	tail := make([]byte, len("rootfs-end"))
	rootfs, err := os.Open(rootfsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rootfs.Close()
	if _, err := rootfs.ReadAt(tail, partition.Size-4096); err != nil {
		t.Fatal(err)
	}
	if string(tail) != "rootfs-end" {
		t.Fatalf("tail = %q, want rootfs-end", string(tail))
	}
}

func allocatedBytesForPartitionTest(t *testing.T, path string) int64 {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	return stat.Blocks * 512
}
