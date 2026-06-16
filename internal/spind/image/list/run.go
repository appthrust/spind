package list

import (
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindimage "github.com/suin/spind/internal/spind/image"
)

func Run(cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	images, err := imageStore(cfg).List()
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	if options.JSON {
		result := make([]output.ImageInfo, 0, len(images))
		for _, info := range images {
			result = append(result, output.NewImageInfo(info))
		}
		return output.WriteJSON(stdout, result, stderr)
	}
	if len(images) == 0 {
		fmt.Fprintln(stdout, "no images")
		return 0
	}
	output.PrintImageList(stdout, images)
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
