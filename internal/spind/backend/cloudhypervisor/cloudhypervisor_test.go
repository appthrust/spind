package cloudhypervisor

import "testing"

func TestNetworkBackendNames(t *testing.T) {
	if NetworkBackendPasst() != "passt" {
		t.Fatalf("NetworkBackendPasst() = %q", NetworkBackendPasst())
	}
	if NetworkBackendTap() != "tap" {
		t.Fatalf("NetworkBackendTap() = %q", NetworkBackendTap())
	}
}
