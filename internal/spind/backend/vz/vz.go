package vz

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

type saveRequest struct {
	Op        string `json:"op"`
	StatePath string `json:"statePath"`
}

type saveResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func Save(ctx context.Context, socketPath string, statePath string) error {
	if socketPath == "" {
		return errors.New("Virtualization.framework control socket path is missing")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("connect Virtualization.framework control socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))

	request := saveRequest{Op: "save", StatePath: statePath}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return fmt.Errorf("send Virtualization.framework save request: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("read Virtualization.framework save response: %w", err)
	}
	var response saveResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return fmt.Errorf("decode Virtualization.framework save response: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "save failed"
		}
		return errors.New(response.Error)
	}
	return nil
}

func ReadLifecycle(stdout io.Reader, log io.Writer, events chan<- string) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(log, line)
		var event struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &event); err == nil && event.Event != "" {
			events <- event.Event
		}
	}
	close(events)
}

func WaitLifecycleEvent(events <-chan string, processAlive func() bool, want string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return fmt.Errorf("Swift runner exited before lifecycle event %q", want)
			}
			if event == want {
				return nil
			}
		case <-ticker.C:
			if !processAlive() {
				return fmt.Errorf("Swift runner exited before lifecycle event %q", want)
			}
		case <-deadline.C:
			return fmt.Errorf("Swift runner lifecycle event %q was not received within %s", want, timeout)
		}
	}
}

func VerifySnapshotFiles(snapshotDir string, names []string) error {
	for _, name := range names {
		info, err := os.Stat(filepath.Join(snapshotDir, name))
		if err != nil {
			return fmt.Errorf("verify Virtualization.framework snapshot file %q: %w", name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("Virtualization.framework snapshot file %q is a directory", name)
		}
	}
	return nil
}

func VerifyRestoreFiles(statePath string, paths []string) error {
	if statePath == "" {
		return errors.New("Virtualization.framework restore state path is missing")
	}
	for _, path := range append([]string{statePath}, paths...) {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("verify Virtualization.framework restore file %q: %w", path, err)
		}
		if info.IsDir() {
			return fmt.Errorf("Virtualization.framework restore file %q is a directory", path)
		}
	}
	return nil
}
