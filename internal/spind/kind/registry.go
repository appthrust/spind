package kind

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	spinddocker "github.com/suin/spind/internal/spind/docker"
)

type RegistryInfo struct {
	TargetPort      int
	Host            string
	HostFromCluster string
}

func K3dRegistry(ctx context.Context, dockerEndpointPath string) (RegistryInfo, error) {
	containers, err := spinddocker.ListContainers(ctx, dockerEndpointPath)
	if err != nil {
		return RegistryInfo{}, fmt.Errorf("list k3d registry containers: %w", err)
	}
	registries := k3dRegistriesFromContainers(containers)
	if len(registries) == 0 {
		return RegistryInfo{}, nil
	}
	if len(registries) > 1 {
		return RegistryInfo{}, errors.New("multiple k3d registries found; automatic registry selection is ambiguous")
	}
	return registries[0], nil
}

func k3dRegistriesFromContainers(containers []spinddocker.ContainerSummary) []RegistryInfo {
	registries := []RegistryInfo{}
	for _, container := range containers {
		if container.Labels["k3d.role"] != "registry" {
			continue
		}
		targetPort := 0
		for _, port := range container.Ports {
			if port.PrivatePort == 5000 && port.PublicPort != 0 && strings.ToLower(port.Type) == "tcp" {
				targetPort = int(port.PublicPort)
				break
			}
		}
		if targetPort == 0 {
			continue
		}
		host := firstContainerName(container)
		registries = append(registries, RegistryInfo{
			TargetPort:      targetPort,
			Host:            host,
			HostFromCluster: host + ":5000",
		})
	}
	sort.Slice(registries, func(i int, j int) bool {
		return registries[i].Host < registries[j].Host
	})
	return registries
}

func firstContainerName(container spinddocker.ContainerSummary) string {
	for _, name := range container.Names {
		name = strings.TrimPrefix(name, "/")
		if name != "" {
			return name
		}
	}
	return ""
}
