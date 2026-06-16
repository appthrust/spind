package filecopy

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

func LinkOrCopy(src string, dst string, mode fs.FileMode) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrPermission) && !errors.Is(err, os.ErrInvalid) {
		// Cross-device links and filesystems without hardlink support fall back to
		// a real copy. Other errors, such as a missing source or existing
		// destination, should be returned as-is by Copy.
	}
	return Copy(src, dst, mode)
}

func Copy(src string, dst string, mode fs.FileMode) error {
	if cloned, err := cloneFile(src, dst, mode); err != nil {
		return err
	} else if cloned {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		_ = out.Close()
		if cleanup {
			_ = os.Remove(dst)
		}
	}()

	if err := out.Chmod(mode); err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		if err := out.Truncate(info.Size()); err != nil {
			return err
		}
		copied, err := copySparseContents(in, out, info.Size())
		if err != nil && !errors.Is(err, ErrSparseCopyUnsupported) {
			return err
		}
		if copied {
			if err := out.Sync(); err != nil {
				return err
			}
			cleanup = false
			return nil
		}
	}

	if _, err := in.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := out.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	cleanup = false
	return nil
}
