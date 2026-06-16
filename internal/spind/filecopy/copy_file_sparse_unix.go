//go:build linux || darwin

package filecopy

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

var ErrSparseCopyUnsupported = errors.New("sparse copy is not supported")

func copySparseContents(in *os.File, out *os.File, size int64) (bool, error) {
	return CopySparseContentsRange(in, out, 0, size)
}

func CopySparseContentsRange(in *os.File, out *os.File, offset int64, size int64) (bool, error) {
	if size == 0 {
		return true, nil
	}

	end := offset + size
	data, err := unix.Seek(int(in.Fd()), offset, unix.SEEK_DATA)
	if err != nil {
		if errors.Is(err, unix.ENXIO) {
			return true, nil
		}
		if errors.Is(err, unix.EINVAL) {
			return false, ErrSparseCopyUnsupported
		}
		return false, err
	}

	for data < end {
		hole, err := unix.Seek(int(in.Fd()), data, unix.SEEK_HOLE)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				hole = end
			} else if errors.Is(err, unix.EINVAL) {
				return false, ErrSparseCopyUnsupported
			} else {
				return false, err
			}
		}
		if hole > end {
			hole = end
		}
		if hole > data {
			if _, err := in.Seek(data, io.SeekStart); err != nil {
				return false, err
			}
			if _, err := out.Seek(data-offset, io.SeekStart); err != nil {
				return false, err
			}
			if err := copySparseData(out, in, hole-data); err != nil {
				return false, err
			}
		}

		data, err = unix.Seek(int(in.Fd()), hole, unix.SEEK_DATA)
		if err != nil {
			if errors.Is(err, unix.ENXIO) {
				break
			}
			if errors.Is(err, unix.EINVAL) {
				return false, ErrSparseCopyUnsupported
			}
			return false, err
		}
	}
	return true, nil
}

func copySparseData(out *os.File, in *os.File, length int64) error {
	buffer := make([]byte, 256*1024)
	remaining := length
	for remaining > 0 {
		chunk := len(buffer)
		if remaining < int64(chunk) {
			chunk = int(remaining)
		}
		n, err := io.ReadFull(in, buffer[:chunk])
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if n == 0 {
			break
		}
		data := buffer[:n]
		if allZero(data) {
			if _, err := out.Seek(int64(n), io.SeekCurrent); err != nil {
				return err
			}
		} else if _, err := out.Write(data); err != nil {
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
