package start

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/suin/spind/internal/spind/backend/cloudhypervisor"
	spindvm "github.com/suin/spind/internal/spind/vmstore"
	"golang.org/x/crypto/ssh"
)

var shellSafeArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func (m *Manager) Exec(ctx context.Context, name string, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (int, error) {
	if err := validateStoreName(name); err != nil {
		return 1, err
	}
	if len(args) == 0 {
		return 2, errors.New("exec command is empty")
	}

	vmDir := filepath.Join(m.VMStore, name)
	if _, err := os.Stat(filepath.Join(vmDir, vmMetadataName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, fmt.Errorf("VM %q: %w", name, ErrNotFound)
		}
		return 1, fmt.Errorf("read VM metadata: %w", err)
	}
	var metadata spindvm.Metadata
	if err := readJSON(filepath.Join(vmDir, vmMetadataName), &metadata); err != nil {
		return 1, fmt.Errorf("read VM metadata: %w", err)
	}
	if metadata.ExecUser == "" {
		metadata.ExecUser = defaultExecUser
	}
	if metadata.Backend == "" {
		metadata.Backend = BackendVirtualizationFramework
	}

	state, err := readState(vmDir)
	if err != nil {
		return 1, err
	}
	if state.Status != "running" || state.PID == 0 || !processAlive(state.PID) {
		return 1, fmt.Errorf("VM %q: %w", name, ErrNotRunning)
	}
	if state.ExecSocketPath == "" {
		return 1, fmt.Errorf("VM %q has no exec connection info: %w", name, ErrNotRunning)
	}

	conn, err := m.dialExecTransport(ctx, metadata, state)
	if err != nil {
		return 1, fmt.Errorf("connect exec relay: %w", err)
	}
	defer conn.Close()

	signer, err := readSSHSigner(filepath.Join(vmDir, vmSSHPrivateKeyName))
	if err != nil {
		return 1, err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	sshConn, channels, requests, err := ssh.NewClientConn(conn, "spind-vsock", &ssh.ClientConfig{
		User: metadata.ExecUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		return 1, fmt.Errorf("start SSH session: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, channels, requests)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return 1, fmt.Errorf("open SSH session: %w", err)
	}
	defer session.Close()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return 1, fmt.Errorf("open SSH stdin: %w", err)
	}
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return 1, fmt.Errorf("open SSH stdout: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return 1, fmt.Errorf("open SSH stderr: %w", err)
	}

	var outputWG sync.WaitGroup
	var copyErr error
	var copyErrOnce sync.Once
	setCopyErr := func(err error) {
		if err != nil {
			copyErrOnce.Do(func() {
				copyErr = err
			})
		}
	}

	go func() {
		defer stdinPipe.Close()
		_, _ = io.Copy(stdinPipe, stdin)
	}()
	outputWG.Add(2)
	go func() {
		defer outputWG.Done()
		if _, err := io.Copy(stdout, stdoutPipe); err != nil {
			setCopyErr(fmt.Errorf("write stdout: %w", err))
		}
	}()
	go func() {
		defer outputWG.Done()
		if _, err := io.Copy(stderr, stderrPipe); err != nil {
			setCopyErr(fmt.Errorf("write stderr: %w", err))
		}
	}()

	exitCode := 0
	if err := session.Run(shellCommand(args)); err != nil {
		var exitError *ssh.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitStatus()
		} else {
			return 1, fmt.Errorf("run SSH command: %w", err)
		}
	}
	outputWG.Wait()
	if copyErr != nil {
		return 1, copyErr
	}
	return exitCode, nil
}

func (m *Manager) dialExecTransport(ctx context.Context, metadata spindvm.Metadata, state spindvm.State) (net.Conn, error) {
	switch metadata.Backend {
	case BackendVirtualizationFramework:
		if state.ExecSocketPath == "" {
			return nil, fmt.Errorf("missing exec socket path")
		}
		dialer := net.Dialer{Timeout: 5 * time.Second}
		return dialer.DialContext(ctx, "unix", state.ExecSocketPath)
	case BackendCloudHypervisor:
		return cloudhypervisor.DialVsock(ctx, state.CloudHypervisorVsockSocketPath, state.ExecPort)
	default:
		return nil, fmt.Errorf("unsupported backend %q", metadata.Backend)
	}
}

func readSSHSigner(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("SSH exec key %q: %w", vmSSHPrivateKeyName, ErrNotFound)
		}
		return nil, fmt.Errorf("read SSH exec key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse SSH exec key: %w", err)
	}
	return signer, nil
}

func shellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg != "" && shellSafeArg.MatchString(arg) {
		return arg
	}
	quoted := "'"
	for _, r := range arg {
		if r == '\'' {
			quoted += "'\\''"
		} else {
			quoted += string(r)
		}
	}
	return quoted + "'"
}
