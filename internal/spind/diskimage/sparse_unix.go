//go:build linux || darwin

package diskimage

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func copySparseRangePlatform(src *os.File, dst *os.File, srcSize int64, dstStart int64) (bool, error) {
	data, err := unix.Seek(int(src.Fd()), 0, unix.SEEK_DATA)
	if err != nil {
		if errors.Is(err, unix.ENXIO) {
			return true, nil
		}
		if errors.Is(err, unix.EINVAL) {
			return false, nil
		}
		return false, err
	}

	for data < srcSize {
		hole, err := unix.Seek(int(src.Fd()), data, unix.SEEK_HOLE)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				hole = srcSize
			} else if errors.Is(err, unix.EINVAL) {
				return false, nil
			} else {
				return false, err
			}
		}
		if hole > srcSize {
			hole = srcSize
		}
		if hole > data {
			if _, err := src.Seek(data, io.SeekStart); err != nil {
				return false, err
			}
			if _, err := dst.Seek(dstStart+data, io.SeekStart); err != nil {
				return false, err
			}
			if err := copySparseData(dst, src, hole-data); err != nil {
				return false, err
			}
		}

		data, err = unix.Seek(int(src.Fd()), hole, unix.SEEK_DATA)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				break
			}
			if errors.Is(err, unix.EINVAL) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

func copySparseData(dst *os.File, src *os.File, length int64) error {
	buffer := make([]byte, 256*1024)
	remaining := length
	for remaining > 0 {
		chunk := len(buffer)
		if remaining < int64(chunk) {
			chunk = int(remaining)
		}
		n, err := io.ReadFull(src, buffer[:chunk])
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if n == 0 {
			break
		}
		data := buffer[:n]
		if allZero(data) {
			if _, err := dst.Seek(int64(n), io.SeekCurrent); err != nil {
				return err
			}
		} else if _, err := dst.Write(data); err != nil {
			return err
		}
		remaining -= int64(n)
		if err != nil {
			break
		}
	}
	return nil
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}
