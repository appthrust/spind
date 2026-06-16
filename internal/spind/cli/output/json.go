package output

import (
	"encoding/json"
	"fmt"
	"io"
)

func WriteJSON(stdout io.Writer, value any, stderr io.Writer) int {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "spind: write JSON: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}
