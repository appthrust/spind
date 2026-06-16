package create

import (
	"fmt"
	"strconv"
	"strings"
)

func parseMemoryMiB(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("--memory must be non-empty")
	}
	baseText, unit := splitMemorySize(value)
	base, err := strconv.ParseInt(baseText, 10, 64)
	if err != nil || base <= 0 {
		return 0, fmt.Errorf("--memory must be a positive size like 4096MiB or 4GiB")
	}
	multiplier, ok := memoryMiBMultiplier(unit)
	if !ok {
		return 0, fmt.Errorf("--memory has unsupported suffix %q; use MiB or GiB", unit)
	}
	if base > int64(^uint(0)>>1)/multiplier {
		return 0, fmt.Errorf("--memory is too large")
	}
	return int(base * multiplier), nil
}

func splitMemorySize(value string) (string, string) {
	index := 0
	for index < len(value) && value[index] >= '0' && value[index] <= '9' {
		index++
	}
	return value[:index], value[index:]
}

func memoryMiBMultiplier(unit string) (int64, bool) {
	switch strings.ToLower(unit) {
	case "":
		return 1, true
	case "m", "mb", "mib":
		return 1, true
	case "g", "gb", "gib":
		return 1024, true
	case "t", "tb", "tib":
		return 1024 * 1024, true
	default:
		return 0, false
	}
}
