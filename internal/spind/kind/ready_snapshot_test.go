package kind

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	spinddocker "github.com/suin/spind/internal/spind/docker"
)

func TestResolveKubeconfigPathDefaultsToGlobalKubeconfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "")

	got, err := ResolveKubeconfigPath("")
	if err != nil {
		t.Fatalf("ResolveKubeconfigPath returned error: %v", err)
	}
	want := filepath.Join(home, ".kube", "config")
	if got != want {
		t.Fatalf("ResolveKubeconfigPath = %q, want %q", got, want)
	}
}

func TestResolveKubeconfigPathUsesSingleKUBECONFIG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", "~/kind-config")

	got, err := ResolveKubeconfigPath("")
	if err != nil {
		t.Fatalf("ResolveKubeconfigPath returned error: %v", err)
	}
	want := filepath.Join(home, "kind-config")
	if got != want {
		t.Fatalf("ResolveKubeconfigPath = %q, want %q", got, want)
	}
}

func TestResolveKubeconfigPathRejectsMultipleKUBECONFIGPaths(t *testing.T) {
	t.Setenv("KUBECONFIG", strings.Join([]string{"one", "two"}, string(os.PathListSeparator)))

	_, err := ResolveKubeconfigPath("")
	if err == nil || !strings.Contains(err.Error(), "multiple paths") {
		t.Fatalf("ResolveKubeconfigPath error = %v, want multiple paths error", err)
	}
}

func TestValidateServerPublishedPort(t *testing.T) {
	if err := ValidateServerPublishedPort("https://127.0.0.1:35123", 35123, []uint16{35123}); err != nil {
		t.Fatalf("ValidateServerPublishedPort returned error: %v", err)
	}
	if err := ValidateServerPublishedPort("https://localhost:35123", 35123, []uint16{35123}); err != nil {
		t.Fatalf("ValidateServerPublishedPort returned error for localhost: %v", err)
	}
	if err := ValidateServerPublishedPort("https://[::1]:35123", 35123, []uint16{35123}); err != nil {
		t.Fatalf("ValidateServerPublishedPort returned error for IPv6 loopback: %v", err)
	}
}

func TestValidateServerPublishedPortRejectsMismatch(t *testing.T) {
	tests := []struct {
		name          string
		server        string
		serverPort    int
		publishedPort []uint16
		want          string
	}{
		{
			name:          "non loopback",
			server:        "https://192.0.2.10:35123",
			serverPort:    35123,
			publishedPort: []uint16{35123},
			want:          "not a loopback host",
		},
		{
			name:          "no published ports",
			server:        "https://127.0.0.1:35123",
			serverPort:    35123,
			publishedPort: nil,
			want:          "no published kind control-plane API port",
		},
		{
			name:          "port mismatch",
			server:        "https://127.0.0.1:35123",
			serverPort:    35123,
			publishedPort: []uint16{35124},
			want:          "uses port 35123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServerPublishedPort(tt.server, tt.serverPort, tt.publishedPort)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateServerPublishedPort error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestControlPlanePublishedPortsFromContainers(t *testing.T) {
	ports := ControlPlanePublishedPortsFromContainers([]spinddocker.ContainerSummary{
		{
			Names: []string{"/dev-control-plane"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 35123, Type: "tcp"},
				{PrivatePort: 80, PublicPort: 30080, Type: "tcp"},
			},
		},
		{
			Names:  []string{"/named-differently"},
			Labels: map[string]string{"io.x-k8s.kind.role": "control-plane"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 35124, Type: "TCP"},
				{PrivatePort: 6443, PublicPort: 35123, Type: "tcp"},
				{PrivatePort: 6443, PublicPort: 35125, Type: "udp"},
			},
		},
		{
			Names: []string{"/worker"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 45123, Type: "tcp"},
			},
		},
	})
	want := []uint16{35123, 35124}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("ControlPlanePublishedPortsFromContainers = %#v, want %#v", ports, want)
	}
}

func TestK3dAPIServerPublishedPortsFromContainersUsesLoadBalancer(t *testing.T) {
	ports := APIServerPublishedPortsFromContainers([]spinddocker.ContainerSummary{
		{
			Names:  []string{"/k3d-dev-server-0"},
			Labels: map[string]string{"k3d.role": "server"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 35123, Type: "tcp"},
			},
		},
		{
			Names:  []string{"/k3d-dev-serverlb"},
			Labels: map[string]string{"k3d.role": "loadbalancer"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 35124, Type: "tcp"},
			},
		},
	}, DistributionK3d)
	want := []uint16{35124}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("APIServerPublishedPortsFromContainers = %#v, want %#v", ports, want)
	}
}

func TestValidateK3dServerPublishedPortAcceptsWildcardHost(t *testing.T) {
	if err := ValidateDistributionServerPublishedPort(DistributionK3d, "https://0.0.0.0:35123", 35123, []uint16{35123}); err != nil {
		t.Fatalf("ValidateDistributionServerPublishedPort() error = %v", err)
	}
}
