package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func WaitAPI(ctx context.Context, socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := Ping(ctx, socketPath); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("Docker API was not ready within %s: %w", timeout, lastErr)
	}
	return fmt.Errorf("Docker API was not ready within %s", timeout)
}

func Ping(ctx context.Context, socketPath string) error {
	if socketPath == "" {
		return errors.New("Docker endpoint socket path is missing")
	}
	client, closeIdle := unixSocketClient(socketPath, 2*time.Second)
	defer closeIdle()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Docker API _ping returned %s", response.Status)
	}
	if strings.TrimSpace(string(body)) != "OK" {
		return fmt.Errorf("Docker API _ping returned %q", strings.TrimSpace(string(body)))
	}
	return nil
}

func ListContainers(ctx context.Context, dockerEndpointPath string) ([]ContainerSummary, error) {
	client, closeIdle := unixSocketClient(dockerEndpointPath, 2*time.Second)
	defer closeIdle()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Docker API containers/json returned %s", response.Status)
	}
	var containers []ContainerSummary
	if err := json.NewDecoder(response.Body).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}

func unixSocketClient(socketPath string, timeout time.Duration) (*http.Client, func()) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: time.Second}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &http.Client{Transport: transport, Timeout: timeout}, transport.CloseIdleConnections
}
