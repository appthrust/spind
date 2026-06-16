package start

import (
	"github.com/suin/spind/internal/spind/store"
	"github.com/suin/spind/internal/spind/vmstore"
)

var (
	ErrAlreadyExists = store.ErrAlreadyExists
	ErrInvalidName   = store.ErrInvalidName
	ErrNotFound      = store.ErrNotFound
	ErrNotRunning    = vmstore.ErrNotRunning
)
