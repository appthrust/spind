package requirements

import (
	"errors"
	"strings"

	"github.com/suin/spind/internal/spind/config"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

type Severity string

const (
	SeverityRequired Severity = "required"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

type Status string

const (
	StatusOK      Status = "ok"
	StatusMissing Status = "missing"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

type Check struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Severity Severity `json:"severity"`
	Status   Status   `json:"status"`
	Message  string   `json:"message,omitempty"`
	Fix      string   `json:"fix,omitempty"`
}

type Result struct {
	Checks []Check `json:"checks"`
}

func (r Result) OK() bool {
	return r.FatalError() == nil
}

func Merge(results ...Result) Result {
	merged := Result{}
	for _, result := range results {
		merged.Checks = append(merged.Checks, result.Checks...)
	}
	return merged
}

func (r Result) FatalError() error {
	messages := []string{}
	for _, check := range r.Checks {
		if check.Severity != SeverityRequired {
			continue
		}
		if check.Status == StatusOK || check.Status == StatusSkipped {
			continue
		}
		message := check.Message
		if message == "" {
			message = check.Label + " is " + string(check.Status)
		}
		if check.Fix != "" {
			message += "; " + check.Fix
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return nil
	}
	return errors.New("requirements not satisfied: " + strings.Join(messages, "; "))
}

type VMStartRequest struct {
	Backend      string
	Image        string
	FromSnapshot bool
}

type ImageBuildRequest struct {
	Name string
}

func ForVMStart(metadata spindvm.Metadata) VMStartRequest {
	return VMStartRequest{
		Backend:      metadata.Backend,
		Image:        metadata.Image,
		FromSnapshot: metadata.FromSnapshot,
	}
}

func ConfigFromVMManager(home string, imageStore string, templateStore string, builtinTemplateSource string, vmStore string, snapshotStore string, runnerPath string, cloudHypervisorPath string) config.Config {
	return config.Config{
		Home:                  home,
		ImageStore:            imageStore,
		TemplateStore:         templateStore,
		BuiltinTemplateSource: builtinTemplateSource,
		VMStore:               vmStore,
		SnapshotStore:         snapshotStore,
		RunnerPath:            runnerPath,
		CloudHypervisorPath:   cloudHypervisorPath,
	}
}
