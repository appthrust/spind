//go:build !linux

package mounthelper

import (
	"context"
	"errors"
)

func Run(context.Context) error {
	return errors.New("mount-helper is supported on Linux guests only")
}
