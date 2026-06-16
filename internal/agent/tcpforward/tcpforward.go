package tcpforward

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/suin/spind/internal/agent/relay"
	"github.com/suin/spind/internal/agent/vsock"
)

const vsockPort = 10241

func Run(ctx context.Context) error {
	return vsock.Listen(ctx, vsockPort, handleConnection)
}

func handleConnection(vsockConn net.Conn) {
	defer vsockConn.Close()

	reader := bufio.NewReader(vsockConn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	host, port, err := parseConnectLine(line)
	if err != nil {
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return
	}
	if _, err := fmt.Fprintln(vsockConn, "OK"); err != nil {
		_ = target.Close()
		return
	}
	relay.Buffered(reader, vsockConn, target)
}

func parseConnectLine(line string) (string, int, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "CONNECT" || fields[1] != "127.0.0.1" {
		return "", 0, errors.New("invalid CONNECT line")
	}
	port, err := strconv.Atoi(fields[2])
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("invalid CONNECT port")
	}
	return fields[1], port, nil
}
