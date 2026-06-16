package store

import "errors"

var (
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidName   = errors.New("invalid name")
	ErrNotFound      = errors.New("not found")
)
