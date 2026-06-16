//go:build darwin

package filecopy

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(src string, dst string, mode fs.FileMode) (bool, error) {
	if err := unix.Clonefile(src, dst, 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) ||
			errors.Is(err, unix.EINVAL) ||
			errors.Is(err, unix.EXDEV) {
			return false, nil
		}
		return false, err
	}
	if err := os.Chmod(dst, mode); err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	file, err := os.OpenFile(dst, os.O_WRONLY, mode)
	if err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(dst)
		return false, err
	}
	return true, nil
}
