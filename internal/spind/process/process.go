package process

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func Signal(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal process %d: %w", pid, err)
	}
	return nil
}

func WaitForExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		reaped, err := Reap(pid)
		if err != nil {
			return err
		}
		if reaped {
			return nil
		}
		if !Alive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not stop within %s", pid, timeout)
}

func Reap(pid int) (bool, error) {
	var status syscall.WaitStatus
	reapedPID, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if err != nil {
		if errors.Is(err, syscall.ECHILD) {
			return false, nil
		}
		return false, fmt.Errorf("wait for process %d: %w", pid, err)
	}
	return reapedPID == pid, nil
}
