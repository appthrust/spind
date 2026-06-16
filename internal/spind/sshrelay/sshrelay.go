package sshrelay

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

type Conn struct {
	net.Conn
	client    *ssh.Client
	transport net.Conn
}

func (c *Conn) Close() error {
	err := c.Conn.Close()
	_ = c.client.Close()
	_ = c.transport.Close()
	return err
}

func Dial(ctx context.Context, socketPath string, keyPath string, user string, host string, port int) (net.Conn, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("SSH relay socket path is missing")
	}
	if keyPath == "" {
		return nil, fmt.Errorf("SSH relay key path is missing")
	}
	if user == "" {
		return nil, fmt.Errorf("SSH relay user is missing")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read SSH relay key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse SSH relay key: %w", err)
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	transport, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, err
	}
	_ = transport.SetDeadline(time.Now().Add(10 * time.Second))
	sshConn, channels, requests, err := ssh.NewClientConn(transport, "spind-vsock", &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("start SSH relay client: %w", err)
	}
	_ = transport.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, channels, requests)
	conn, err := client.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_ = client.Close()
		_ = transport.Close()
		return nil, fmt.Errorf("open SSH relay target: %w", err)
	}
	return &Conn{Conn: conn, client: client, transport: transport}, nil
}
