//go:build !linux && !darwin

package filecopy

import "os"

var ErrSparseCopyUnsupported = errUnsupportedSparseCopy{}

type errUnsupportedSparseCopy struct{}

func (errUnsupportedSparseCopy) Error() string { return "sparse copy is not supported" }

func copySparseContents(in *os.File, out *os.File, size int64) (bool, error) {
	return false, ErrSparseCopyUnsupported
}

func CopySparseContentsRange(in *os.File, out *os.File, offset int64, size int64) (bool, error) {
	return false, ErrSparseCopyUnsupported
}
