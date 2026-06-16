package delete

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindimage "github.com/suin/spind/internal/spind/image"
	"github.com/suin/spind/internal/spind/vmstore"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	for _, name := range options.Names {
		references, err := imageReferences(cfg.VMStore, name)
		if err != nil {
			return cliruntime.ExitForError(stderr, err)
		}
		result, err := imageStore(cfg).Delete(name, references, spindimage.DeleteOptions{Force: options.Force})
		if err != nil {
			return cliruntime.ExitForError(stderr, err)
		}
		if len(result.ReferencedVMs) > 0 {
			fmt.Fprintf(stdout, "deleted image %q (%s, referenced by %s)\n", name, output.FormatSize(result.SizeBytes), strings.Join(result.ReferencedVMs, ", "))
			continue
		}
		fmt.Fprintf(stdout, "deleted image %q (%s)\n", name, output.FormatSize(result.SizeBytes))
	}
	return 0
}

func imageStore(cfg config.Config) spindimage.Store {
	return spindimage.Store{
		Home:                  cfg.Home,
		ImageStore:            cfg.ImageStore,
		TemplateStore:         cfg.TemplateStore,
		BuiltinTemplateSource: cfg.BuiltinTemplateSource,
	}
}

func imageReferences(vmStore string, imageName string) ([]string, error) {
	entries, err := os.ReadDir(vmStore)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read VM store: %w", err)
	}
	references := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metadata, err := vmstore.ReadMetadata(filepath.Join(vmStore, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read VM metadata %q while checking image references: %w", entry.Name(), err)
		}
		if metadata.Image == imageName {
			references = append(references, entry.Name())
		}
	}
	sort.Strings(references)
	return references, nil
}
