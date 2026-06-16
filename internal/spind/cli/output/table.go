package output

import (
	"fmt"
	"io"
	"strings"
)

func printTable(stdout io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for index, value := range header {
		widths[index] = len(value)
	}
	for _, row := range rows {
		for index, value := range row {
			if index < len(widths) && len(value) > widths[index] {
				widths[index] = len(value)
			}
		}
	}
	printTableRow(stdout, widths, header)
	for _, row := range rows {
		printTableRow(stdout, widths, row)
	}
}

func printTableRow(stdout io.Writer, widths []int, values []string) {
	var line strings.Builder
	for index, value := range values {
		if index > 0 {
			line.WriteString("  ")
		}
		if index == len(values)-1 {
			line.WriteString(value)
			continue
		}
		width := 0
		if index < len(widths) {
			width = widths[index]
		}
		fmt.Fprintf(&line, "%-*s", width, value)
	}
	fmt.Fprintln(stdout, strings.TrimRight(line.String(), " "))
}
