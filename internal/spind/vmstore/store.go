package vmstore

import (
	"fmt"
	"path/filepath"

	"github.com/suin/spind/internal/spind/store"
)

const (
	MetadataName = "metadata.json"
	StateName    = "state.json"
)

func ValidateName(name string) error {
	if !store.ValidName(name) {
		return fmt.Errorf("%q: %w", name, store.ErrInvalidName)
	}
	return nil
}

func ReadMetadata(vmDir string) (Metadata, error) {
	var metadata Metadata
	if err := store.ReadJSON(filepath.Join(vmDir, MetadataName), &metadata); err != nil {
		return metadata, err
	}
	return metadata, nil
}

func WriteMetadata(vmDir string, metadata Metadata) error {
	return store.WriteJSON(filepath.Join(vmDir, MetadataName), metadata, 0o644)
}

func ReadState(vmDir string) (State, error) {
	var state State
	if err := store.ReadJSON(filepath.Join(vmDir, StateName), &state); err != nil {
		return state, fmt.Errorf("read VM state: %w", err)
	}
	return state, nil
}

func WriteState(vmDir string, state State) error {
	return store.WriteJSON(filepath.Join(vmDir, StateName), state, 0o644)
}
