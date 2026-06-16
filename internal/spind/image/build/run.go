package build

import (
	"context"
	"fmt"
	"io"

	"github.com/suin/spind/internal/spind/cli/output"
	cliruntime "github.com/suin/spind/internal/spind/cli/runtime"
	"github.com/suin/spind/internal/spind/config"
	spindimage "github.com/suin/spind/internal/spind/image"
	"github.com/suin/spind/internal/spind/requirements"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	if err := requirements.CheckOrError(requirements.CheckImageBuild(ctx, cfg, requirements.ImageBuildRequest{Name: options.Name})); err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	result, err := imageStore(cfg).Build(ctx, options.Name, spindimage.BuildOptions{
		Config:   options.Config,
		Force:    options.Force,
		DataSize: options.DataSize,
	})
	if err != nil {
		return cliruntime.ExitForError(stderr, err)
	}
	fmt.Fprintf(stdout, "built image %q (%s)\n", result.Name, output.FormatSize(result.SizeBytes))
	fmt.Fprintf(stdout, "path: %s\n", result.ImageDir)
	fmt.Fprintf(stdout, "template: %s\n", result.TemplatePath)
	fmt.Fprintf(stdout, "createdAt: %s\n", output.FormatTime(result.CreatedAt))
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
