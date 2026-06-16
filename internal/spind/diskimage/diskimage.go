package diskimage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	SectorSize         = 512
	DefaultStartSector = 2048
	linuxPartitionType = 0x83
)

type Partition struct {
	Start int64
	Size  int64
}

func CreateMBRDisk(diskPath string, rootfsPath string, diskSize int64, startSector uint32) error {
	if startSector == 0 {
		startSector = DefaultStartSector
	}
	rootfs, err := os.Open(rootfsPath)
	if err != nil {
		return fmt.Errorf("open root filesystem image: %w", err)
	}
	defer rootfs.Close()
	rootfsInfo, err := rootfs.Stat()
	if err != nil {
		return fmt.Errorf("stat root filesystem image: %w", err)
	}
	start := int64(startSector) * SectorSize
	if diskSize <= start {
		return fmt.Errorf("disk size %d is too small for partition start %d", diskSize, start)
	}
	if rootfsInfo.Size() > diskSize-start {
		return fmt.Errorf("root filesystem image size %d exceeds partition capacity %d", rootfsInfo.Size(), diskSize-start)
	}
	partitionSectors := uint32((diskSize - start) / SectorSize)

	disk, err := os.OpenFile(diskPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("create disk image: %w", err)
	}
	defer disk.Close()
	if err := disk.Truncate(diskSize); err != nil {
		return fmt.Errorf("size disk image: %w", err)
	}
	mbr := make([]byte, SectorSize)
	entry := mbr[446 : 446+16]
	entry[4] = linuxPartitionType
	entry[1], entry[2], entry[3] = 0x00, 0x02, 0x00
	entry[5], entry[6], entry[7] = 0xfe, 0xff, 0xff
	binary.LittleEndian.PutUint32(entry[8:12], startSector)
	binary.LittleEndian.PutUint32(entry[12:16], partitionSectors)
	mbr[510], mbr[511] = 0x55, 0xaa
	if _, err := disk.WriteAt(mbr, 0); err != nil {
		return fmt.Errorf("write MBR: %w", err)
	}
	if _, err := rootfs.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek root filesystem image: %w", err)
	}
	if _, err := disk.Seek(start, io.SeekStart); err != nil {
		return fmt.Errorf("seek disk partition: %w", err)
	}
	if copied, err := copySparseRange(rootfs, disk, rootfsInfo.Size(), start); err != nil {
		return fmt.Errorf("write root filesystem partition: %w", err)
	} else if !copied {
		if _, err := rootfs.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek root filesystem image: %w", err)
		}
		if _, err := disk.Seek(start, io.SeekStart); err != nil {
			return fmt.Errorf("seek disk partition: %w", err)
		}
		if _, err := io.Copy(disk, rootfs); err != nil {
			return fmt.Errorf("write root filesystem partition: %w", err)
		}
	}
	return disk.Sync()
}

func copySparseRange(src *os.File, dst *os.File, srcSize int64, dstStart int64) (bool, error) {
	if srcSize == 0 {
		return true, nil
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	if _, err := dst.Seek(dstStart, io.SeekStart); err != nil {
		return false, err
	}
	return copySparseRangePlatform(src, dst, srcSize, dstStart)
}

func FirstPartition(path string) (Partition, error) {
	file, err := os.Open(path)
	if err != nil {
		return Partition{}, err
	}
	defer file.Close()
	mbr := make([]byte, SectorSize)
	if _, err := io.ReadFull(file, mbr); err != nil {
		return Partition{}, fmt.Errorf("read MBR: %w", err)
	}
	if mbr[510] != 0x55 || mbr[511] != 0xaa {
		return Partition{}, errors.New("disk image has no MBR signature")
	}
	entry := mbr[446 : 446+16]
	if entry[4] == 0 {
		return Partition{}, errors.New("disk image first partition is empty")
	}
	startSector := binary.LittleEndian.Uint32(entry[8:12])
	sectorCount := binary.LittleEndian.Uint32(entry[12:16])
	if startSector == 0 || sectorCount == 0 {
		return Partition{}, errors.New("disk image first partition has invalid bounds")
	}
	return Partition{
		Start: int64(startSector) * SectorSize,
		Size:  int64(sectorCount) * SectorSize,
	}, nil
}
