package cloudhypervisor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	apiRequestTimeout         = 5 * time.Second
	snapshotAPIRequestTimeout = 120 * time.Second
)

func WaitForAPI(ctx context.Context, pid int, socketPath string, timeout time.Duration, processAlive func(int) bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return errors.New("Cloud Hypervisor exited before REST API was ready")
		}
		if err := Request(ctx, socketPath, http.MethodGet, "/api/v1/vmm.ping"); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("Cloud Hypervisor REST API was not ready within %s", timeout)
}

func WaitForSocketFile(path string, pid int, timeout time.Duration, processAlive func(int) bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("process %d exited before socket %s was ready", pid, path)
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check socket %s: %w", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("socket %s was not ready within %s", path, timeout)
}

func Request(ctx context.Context, socketPath string, method string, path string) error {
	return requestBody(ctx, socketPath, method, path, nil, "")
}

func Snapshot(ctx context.Context, socketPath string, destinationDir string) error {
	body, err := json.Marshal(map[string]string{
		"destination_url": "file://" + destinationDir,
	})
	if err != nil {
		return err
	}
	return requestBodyWithTimeout(ctx, socketPath, http.MethodPut, "/api/v1/vm.snapshot", bytes.NewReader(body), "application/json", snapshotAPIRequestTimeout)
}

func requestBody(ctx context.Context, socketPath string, method string, path string, body io.Reader, contentType string) error {
	return requestBodyWithTimeout(ctx, socketPath, method, path, body, contentType, apiRequestTimeout)
}

func requestBodyWithTimeout(ctx context.Context, socketPath string, method string, path string, body io.Reader, contentType string, timeout time.Duration) error {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout}
	request, err := http.NewRequestWithContext(ctx, method, "http://cloud-hypervisor"+path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Cloud Hypervisor API %s %s returned %s", method, path, response.Status)
	}
	return nil
}

func DialVsock(ctx context.Context, socketPath string, port uint32) (net.Conn, error) {
	if socketPath == "" {
		return nil, errors.New("missing Cloud Hypervisor vsock socket path")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	status := strings.TrimSpace(line)
	if status != "OK" && !strings.HasPrefix(status, "OK ") {
		_ = conn.Close()
		return nil, fmt.Errorf("Cloud Hypervisor vsock CONNECT returned %q", status)
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *bufferedConn) CloseWrite() error {
	if closer, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return c.Conn.Close()
}
