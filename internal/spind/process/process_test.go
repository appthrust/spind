package process

import (
	"syscall"
	"testing"
)

func TestAliveFalseForNonPositivePID(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if Alive(pid) {
			t.Fatalf("Alive(%d) = true, want false", pid)
		}
	}
}

func TestSignalIgnoresNonPositivePID(t *testing.T) {
	if err := Signal(0, syscall.SIGTERM); err != nil {
		t.Fatalf("Signal(0) error = %v, want nil", err)
	}
}
