//go:build linux

package filecopy

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(src string, dst string, mode fs.FileMode) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return false, err
	}
	cleanup := true
	defer func() {
		_ = out.Close()
		if cleanup {
			_ = os.Remove(dst)
		}
	}()

	if err := unix.IoctlFileClone(int(out.Fd()), int(in.Fd())); err != nil {
		if errors.Is(err, unix.EOPNOTSUPP) ||
			errors.Is(err, unix.ENOTTY) ||
			errors.Is(err, unix.EINVAL) ||
			errors.Is(err, unix.EXDEV) {
			return false, nil
		}
		return false, err
	}
	if err := out.Chmod(mode); err != nil {
		return false, err
	}
	if err := out.Sync(); err != nil {
		return false, err
	}
	cleanup = false
	return true, nil
}
