package kind

import (
	"reflect"
	"testing"

	spinddocker "github.com/suin/spind/internal/spind/docker"
)

func TestK3dRegistriesFromContainers(t *testing.T) {
	registries := k3dRegistriesFromContainers([]spinddocker.ContainerSummary{
		{
			Names:  []string{"/k3d-dev-registry"},
			Labels: map[string]string{"k3d.role": "registry"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 5000, PublicPort: 52793, Type: "tcp"},
			},
		},
		{
			Names:  []string{"/k3d-dev-serverlb"},
			Labels: map[string]string{"k3d.role": "loadbalancer"},
			Ports: []spinddocker.PortMapping{
				{PrivatePort: 6443, PublicPort: 35124, Type: "tcp"},
			},
		},
	})
	want := []RegistryInfo{{
		TargetPort:      52793,
		Host:            "k3d-dev-registry",
		HostFromCluster: "k3d-dev-registry:5000",
	}}
	if !reflect.DeepEqual(registries, want) {
		t.Fatalf("k3dRegistriesFromContainers = %#v, want %#v", registries, want)
	}
}
