package doctor

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/suin/spind/internal/spind/cli/output"
	"github.com/suin/spind/internal/spind/config"
	"github.com/suin/spind/internal/spind/requirements"
)

func Run(ctx context.Context, cfg config.Config, options Options, stdout io.Writer, stderr io.Writer) int {
	result := requirements.CheckDoctor(ctx, cfg)
	if options.JSON {
		code := output.WriteJSON(stdout, result, stderr)
		if code != 0 {
			return code
		}
		if result.FatalError() != nil {
			return 1
		}
		return 0
	}
	printResult(stdout, result)
	if err := result.FatalError(); err != nil {
		return 1
	}
	return 0
}

func printResult(stdout io.Writer, result requirements.Result) {
	fmt.Fprintln(stdout, "spind doctor")
	for _, severity := range []requirements.Severity{requirements.SeverityRequired, requirements.SeverityWarning, requirements.SeverityInfo} {
		checks := checksBySeverity(result.Checks, severity)
		if len(checks) == 0 {
			continue
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, severityTitle(severity))
		for _, check := range checks {
			fmt.Fprintf(stdout, "  %-7s %s", check.Status, check.Label)
			if check.Message != "" {
				fmt.Fprintf(stdout, " - %s", check.Message)
			}
			fmt.Fprintln(stdout)
			if check.Fix != "" && check.Status != requirements.StatusOK && check.Status != requirements.StatusSkipped {
				fmt.Fprintf(stdout, "          fix: %s\n", check.Fix)
			}
		}
	}
}

func checksBySeverity(checks []requirements.Check, severity requirements.Severity) []requirements.Check {
	filtered := []requirements.Check{}
	for _, check := range checks {
		if check.Severity == severity {
			filtered = append(filtered, check)
		}
	}
	return filtered
}

func severityTitle(severity requirements.Severity) string {
	value := string(severity)
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
