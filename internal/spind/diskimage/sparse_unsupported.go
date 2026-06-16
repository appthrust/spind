//go:build !linux && !darwin

package diskimage

import "os"

func copySparseRangePlatform(src *os.File, dst *os.File, srcSize int64, dstStart int64) (bool, error) {
	return false, nil
}
