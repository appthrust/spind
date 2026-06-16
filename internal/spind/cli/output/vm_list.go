package output

import (
	"fmt"
	"io"

	spindvm "github.com/suin/spind/internal/spind/vmstore"
)

func PrintVMList(stdout io.Writer, vms []spindvm.Info) {
	rows := make([][]string, 0, len(vms))
	for _, info := range vms {
		rows = append(rows, []string{
			info.Name,
			info.Backend,
			info.Status,
			fmt.Sprintf("%t", info.ExecReady),
			DockerDisplay(info),
			fmt.Sprintf("%t", info.FromSnapshot),
			DisplayValue(info.SourceSnapshot),
			FormatTime(info.StartedAt),
		})
	}
	printTable(stdout,
		[]string{"NAME", "BACKEND", "STATUS", "EXEC_READY", "DOCKER", "FROM_SNAPSHOT", "SOURCE_SNAPSHOT", "STARTED_AT"},
		rows,
	)
}
