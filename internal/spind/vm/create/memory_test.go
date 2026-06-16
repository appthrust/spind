package create

import "testing"

func TestParseMemoryMiB(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int
	}{
		{value: "4096", want: 4096},
		{value: "4096MiB", want: 4096},
		{value: "4GiB", want: 4096},
		{value: "1TiB", want: 1024 * 1024},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseMemoryMiB(tt.value)
			if err != nil {
				t.Fatalf("parseMemoryMiB(%q) returned error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("parseMemoryMiB(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseMemoryMiBRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1GiB", "4KiB", "GiB"} {
		t.Run(value, func(t *testing.T) {
			if got, err := parseMemoryMiB(value); err == nil {
				t.Fatalf("parseMemoryMiB(%q) = %d, nil; want error", value, got)
			}
		})
	}
}
