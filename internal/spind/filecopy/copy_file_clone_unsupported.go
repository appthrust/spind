//go:build !linux && !darwin

package filecopy

import "io/fs"

func cloneFile(src string, dst string, mode fs.FileMode) (bool, error) {
	return false, nil
}
