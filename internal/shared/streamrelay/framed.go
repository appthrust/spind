package streamrelay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	frameData       byte = 0
	frameCloseWrite byte = 1
	maxFramePayload      = 64 * 1024
)

// RelayFramed relays a raw local stream over a framed transport.
//
// The framed transport carries write-side shutdown as an explicit control
// frame, so callers do not need to depend on half-close support from the
// transport itself.
func RelayFramed(local net.Conn, framed net.Conn) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = local.Close()
			_ = framed.Close()
		})
	}
	defer closeBoth()

	results := make(chan error, 2)
	go func() {
		results <- copyLocalToFramed(local, framed)
	}()
	go func() {
		results <- copyFramedToLocal(local, framed)
	}()
	for range 2 {
		if err := <-results; err != nil {
			closeBoth()
		}
	}
}

func RelayRaw(left net.Conn, right net.Conn) {
	done := make(chan struct{}, 2)
	go copyAndClose(left, right, done)
	go copyAndClose(right, left, done)
	<-done
	_ = left.Close()
	_ = right.Close()
	<-done
}

func copyAndClose(dst net.Conn, src net.Conn, done chan<- struct{}) {
	_, _ = io.Copy(dst, src)
	_ = closeWrite(dst)
	done <- struct{}{}
}

func copyLocalToFramed(local net.Conn, framed net.Conn) error {
	buffer := make([]byte, maxFramePayload)
	for {
		n, readErr := local.Read(buffer)
		if n > 0 {
			if err := writeFrame(framed, frameData, buffer[:n]); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return writeFrame(framed, frameCloseWrite, nil)
			}
			return readErr
		}
	}
}

func copyFramedToLocal(local net.Conn, framed net.Conn) error {
	for {
		frameType, payload, err := readFrame(framed)
		if err != nil {
			return err
		}
		switch frameType {
		case frameData:
			if len(payload) == 0 {
				continue
			}
			if err := writeFull(local, payload); err != nil {
				return err
			}
		case frameCloseWrite:
			return closeWrite(local)
		default:
			return fmt.Errorf("unknown stream relay frame type %d", frameType)
		}
	}
}

func writeFrame(w io.Writer, frameType byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return fmt.Errorf("stream relay frame payload too large: %d", len(payload))
	}
	header := [5]byte{frameType}
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if err := writeFull(w, header[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	return writeFull(w, payload)
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size > maxFramePayload {
		return 0, nil, fmt.Errorf("stream relay frame payload too large: %d", size)
	}
	payload := make([]byte, size)
	if size > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return header[0], payload, nil
}

func closeWrite(conn net.Conn) error {
	if closer, ok := conn.(interface{ CloseWrite() error }); ok {
		return closer.CloseWrite()
	}
	return conn.Close()
}

func writeFull(w io.Writer, buffer []byte) error {
	for len(buffer) > 0 {
		n, err := w.Write(buffer)
		if n > 0 {
			buffer = buffer[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
