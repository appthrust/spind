package kind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	spinddocker "github.com/suin/spind/internal/spind/docker"
	"github.com/suin/spind/internal/spind/filecopy"
	"github.com/suin/spind/internal/spind/kubeconfig"
	"github.com/suin/spind/internal/spind/store"
)

type kubectlNodeList struct {
	Items []kubectlNode `json:"items"`
}

type kubectlNode struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

func PrepareSnapshot(ctx context.Context, tmpDir string, dockerEndpointPath string, dockerReady bool, options SnapshotOptions) (*Metadata, error) {
	if !dockerReady {
		return nil, errors.New("kind-ready snapshot requires Docker API ready")
	}
	kubeconfigPath, err := ResolveKubeconfigPath(options.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}
	contextName, err := ResolveContext(ctx, kubeconfigPath, options.Context)
	if err != nil {
		return nil, err
	}
	template, err := KubectlKubeconfigTemplate(ctx, kubeconfigPath, contextName)
	if err != nil {
		return nil, err
	}
	doc, err := kubeconfig.ParseJSON(template)
	if err != nil {
		return nil, err
	}
	sourceCluster, sourceUser, sourceServer, targetPort, err := kubeconfig.SourceInfo(doc)
	if err != nil {
		return nil, err
	}
	if err := ValidateKubeconfigMatchesDocker(ctx, dockerEndpointPath, sourceServer, targetPort); err != nil {
		return nil, err
	}
	nodes, err := KubectlReadyNodes(ctx, kubeconfigPath, contextName)
	if err != nil {
		return nil, err
	}

	kindDir := filepath.Join(tmpDir, DirName)
	if err := os.MkdirAll(filepath.Join(kindDir, "certs"), 0o755); err != nil {
		return nil, fmt.Errorf("create kind snapshot directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(kindDir, KubeconfigTemplateName), template, 0o600); err != nil {
		return nil, fmt.Errorf("write kind kubeconfig template: %w", err)
	}
	metadata := Metadata{
		KindReady:             true,
		SourceKubeconfigPath:  kubeconfigPath,
		SourceContext:         contextName,
		SourceCluster:         sourceCluster,
		SourceUser:            sourceUser,
		SourceServer:          sourceServer,
		APIServerTargetPort:   targetPort,
		Nodes:                 nodes,
		ReadyCheck:            "ok",
		ReadyCheckCompletedAt: time.Now().UTC(),
	}
	if err := store.WriteJSON(filepath.Join(kindDir, MetadataName), metadata, 0o644); err != nil {
		return nil, fmt.Errorf("write kind metadata: %w", err)
	}
	return &metadata, nil
}

func ResolveKubeconfigPath(explicitPath string) (string, error) {
	if explicitPath != "" {
		return kubeconfig.ResolvePath(explicitPath)
	}
	paths, err := kubeconfig.CandidatePaths("")
	if err != nil {
		return "", err
	}
	if len(paths) != 1 {
		return "", errors.New("KUBECONFIG contains multiple paths; pass --kubeconfig explicitly")
	}
	return kubeconfig.ResolvePath(paths[0])
}

func ResolveContext(ctx context.Context, kubeconfigPath string, contextName string) (string, error) {
	if contextName != "" {
		return contextName, nil
	}
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "config", "current-context")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read kubeconfig current-context: %w: %s", err, string(output))
	}
	currentContext := strings.TrimSpace(string(output))
	if currentContext == "" {
		return "", errors.New("kubeconfig has no current-context; pass --context explicitly")
	}
	return currentContext, nil
}

func KubectlKubeconfigTemplate(ctx context.Context, kubeconfigPath string, contextName string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "config", "view", "--raw", "--flatten", "--minify", "--context", contextName, "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig context %q: %w: %s", contextName, err, string(output))
	}
	var doc kubeconfig.Document
	if err := json.Unmarshal(output, &doc); err != nil {
		return nil, fmt.Errorf("parse kubectl config view output: %w", err)
	}
	if len(doc.Contexts) == 0 || len(doc.Clusters) == 0 || len(doc.Users) == 0 {
		return nil, fmt.Errorf("kubeconfig context %q is incomplete", contextName)
	}
	formatted, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(formatted, '\n'), nil
}

func KubectlReadyNodes(ctx context.Context, kubeconfigPath string, contextName string) ([]NodeSummary, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "--context", contextName, "get", "nodes", "-o", "json")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("check kind nodes: %w: %s", err, string(output))
	}
	var list kubectlNodeList
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, fmt.Errorf("parse kubectl nodes: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, errors.New("kind-ready snapshot requires at least one node")
	}
	nodes := make([]NodeSummary, 0, len(list.Items))
	for _, node := range list.Items {
		ready := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				ready = true
				break
			}
		}
		nodes = append(nodes, NodeSummary{Name: node.Metadata.Name, Ready: ready})
		if !ready {
			return nodes, fmt.Errorf("node %q is not Ready", node.Metadata.Name)
		}
	}
	return nodes, nil
}

func ValidateKubeconfigMatchesDocker(ctx context.Context, dockerEndpointPath string, server string, serverPort int) error {
	if dockerEndpointPath == "" {
		return errors.New("kind-ready snapshot requires Docker endpoint path")
	}
	publishedPorts, err := ControlPlanePublishedPorts(ctx, dockerEndpointPath)
	if err != nil {
		return fmt.Errorf("list kind control-plane Docker ports: %w", err)
	}
	return ValidateServerPublishedPort(server, serverPort, publishedPorts)
}

func ControlPlanePublishedPorts(ctx context.Context, dockerEndpointPath string) ([]uint16, error) {
	containers, err := spinddocker.ListContainers(ctx, dockerEndpointPath)
	if err != nil {
		return nil, err
	}
	return ControlPlanePublishedPortsFromContainers(containers), nil
}

func ControlPlanePublishedPortsFromContainers(containers []spinddocker.ContainerSummary) []uint16 {
	seen := map[uint16]struct{}{}
	for _, container := range containers {
		if !isControlPlaneContainer(container) {
			continue
		}
		for _, port := range container.Ports {
			if port.PrivatePort != 6443 || port.PublicPort == 0 || strings.ToLower(port.Type) != "tcp" {
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

func isControlPlaneContainer(container spinddocker.ContainerSummary) bool {
	if container.Labels["io.x-k8s.kind.role"] == "control-plane" {
		return true
	}
	for _, name := range container.Names {
		name = strings.TrimPrefix(name, "/")
		if strings.HasSuffix(name, "-control-plane") {
			return true
		}
	}
	return false
}

func ValidateServerPublishedPort(server string, serverPort int, publishedPorts []uint16) error {
	parsed, err := url.Parse(server)
	if err != nil {
		return fmt.Errorf("parse kubeconfig server: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("kubeconfig server %q has no host", server)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("kubeconfig server %q is not a loopback host", server)
	}
	if len(publishedPorts) == 0 {
		return errors.New("target VM has no published kind control-plane API port")
	}
	for _, publishedPort := range publishedPorts {
		if int(publishedPort) == serverPort {
			return nil
		}
	}
	return fmt.Errorf("kubeconfig server %q uses port %d, but target VM kind control-plane publishes %s", server, serverPort, formatPortList(publishedPorts))
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func formatPortList(ports []uint16) string {
	if len(ports) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, strconv.Itoa(int(port)))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func CopyArtifacts(snapshotDir string, vmDir string) error {
	srcDir := filepath.Join(snapshotDir, DirName)
	if _, err := os.Stat(srcDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check kind snapshot artifacts: %w", err)
	}
	dstDir := filepath.Join(vmDir, DirName)
	return copyDirectory(srcDir, dstDir)
}

func copyDirectory(srcDir string, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return filecopy.Copy(path, dst, info.Mode().Perm())
	})
}

func ReadMetadata(root string) (Metadata, error) {
	var metadata Metadata
	if err := store.ReadJSON(filepath.Join(root, DirName, MetadataName), &metadata); err != nil {
		return metadata, fmt.Errorf("read kind metadata: %w", err)
	}
	return metadata, nil
}

func GenerateKubeconfig(vmDir string, vmName string, serverURL string, outputPath string) error {
	data, err := os.ReadFile(filepath.Join(vmDir, DirName, KubeconfigTemplateName))
	if err != nil {
		return fmt.Errorf("read kubeconfig template: %w", err)
	}
	doc, err := kubeconfig.ParseJSON(data)
	if err != nil {
		return err
	}
	name := "spind-" + vmName
	if len(doc.Clusters) == 0 || len(doc.Users) == 0 || len(doc.Contexts) == 0 {
		return errors.New("kubeconfig template is incomplete")
	}
	doc.Clusters = doc.Clusters[:1]
	doc.Users = doc.Users[:1]
	doc.Contexts = doc.Contexts[:1]
	doc.Clusters[0].Name = name
	doc.Clusters[0].Cluster.Server = serverURL
	doc.Users[0].Name = name
	doc.Contexts[0].Name = name
	doc.Contexts[0].Context.Cluster = name
	doc.Contexts[0].Context.User = name
	doc.CurrentContext = name
	output, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(output, '\n'), 0o600)
}
