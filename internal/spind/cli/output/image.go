package output

import (
	"io"

	spindimage "github.com/suin/spind/internal/spind/image"
)

type ImageInfo struct {
	Name              string `json:"name"`
	Architecture      string `json:"architecture,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	KernelCommandLine string `json:"kernelCommandLine,omitempty"`
	ExecUser          string `json:"execUser,omitempty"`
	SizeBytes         int64  `json:"sizeBytes"`
	ImageDir          string `json:"imageDir"`
	Health            string `json:"health"`
	HealthMessage     string `json:"healthMessage,omitempty"`
}

func NewImageInfo(info spindimage.Info) ImageInfo {
	return ImageInfo{
		Name:              info.Name,
		Architecture:      info.Architecture,
		CreatedAt:         FormatTime(info.CreatedAt),
		KernelCommandLine: info.KernelCommandLine,
		ExecUser:          info.ExecUser,
		SizeBytes:         info.SizeBytes,
		ImageDir:          info.ImageDir,
		Health:            info.Health,
		HealthMessage:     info.HealthMessage,
	}
}

func PrintImageList(stdout io.Writer, images []spindimage.Info) {
	rows := make([][]string, 0, len(images))
	for _, info := range images {
		rows = append(rows, []string{
			info.Name,
			DisplayValue(info.Architecture),
			FormatSize(info.SizeBytes),
			FormatTime(info.CreatedAt),
			info.Health,
		})
	}
	printTable(stdout,
		[]string{"NAME", "ARCH", "SIZE", "CREATED_AT", "HEALTH"},
		rows,
	)
}
