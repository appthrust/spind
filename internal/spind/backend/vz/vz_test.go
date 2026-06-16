package vz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySnapshotFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.vzvmsave"), []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySnapshotFiles(dir, []string{"state.vzvmsave"}); err != nil {
		t.Fatalf("VerifySnapshotFiles() error = %v", err)
	}
}
