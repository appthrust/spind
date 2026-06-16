package docker

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

func PublishedPorts(ctx context.Context, dockerEndpointPath string) []string {
	raw := PublishedPortsRaw(ctx, dockerEndpointPath)
	ports := make([]string, 0, len(raw))
	for _, port := range raw {
		ports = append(ports, strconv.Itoa(int(port))+"/tcp")
	}
	return ports
}

func PublishedPortsRaw(ctx context.Context, dockerEndpointPath string) []uint16 {
	containers, err := ListContainers(ctx, dockerEndpointPath)
	if err != nil {
		return nil
	}
	seen := map[uint16]struct{}{}
	for _, container := range containers {
		for _, port := range container.Ports {
			if port.PublicPort == 0 || strings.ToLower(port.Type) != "tcp" {
				continue
			}
			seen[port.PublicPort] = struct{}{}
		}
	}
	ports := make([]uint16, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i int, j int) bool {
		return ports[i] < ports[j]
	})
	return ports
}
