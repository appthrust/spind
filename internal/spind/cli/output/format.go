package output

import (
	"fmt"
	"time"

	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func FormatDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return duration.String()
	}
	return duration.Round(time.Millisecond).String()
}

func FormatSize(bytes int64) string {
	if bytes <= 0 {
		return "0.0 MiB"
	}
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if bytes >= gib {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
	}
	return fmt.Sprintf("%.1f MiB", float64(bytes)/mib)
}

func FormatMiB(mib int) string {
	if mib <= 0 {
		return "-"
	}
	if mib >= 1024 {
		return fmt.Sprintf("%.1f GiB", float64(mib)/1024)
	}
	return fmt.Sprintf("%d MiB", mib)
}

func FormatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func FormatOptionalDuration(duration time.Duration) string {
	if duration == 0 {
		return ""
	}
	return FormatDuration(duration)
}

func DisplayValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func DockerDisplay(info spindvm.Info) string {
	if info.DockerAvailable {
		return info.DockerEndpointURI
	}
	return "unavailable"
}
