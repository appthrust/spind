package kubeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Document struct {
	APIVersion     string         `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind           string         `json:"kind,omitempty" yaml:"kind,omitempty"`
	Clusters       []Cluster      `json:"clusters,omitempty" yaml:"clusters,omitempty"`
	Users          []User         `json:"users,omitempty" yaml:"users,omitempty"`
	Contexts       []Context      `json:"contexts,omitempty" yaml:"contexts,omitempty"`
	CurrentContext string         `json:"current-context,omitempty" yaml:"current-context,omitempty"`
	Preferences    map[string]any `json:"preferences,omitempty" yaml:"preferences,omitempty"`
	Extensions     []Extension    `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	OtherFields    map[string]any `json:"-" yaml:",inline"`
}

type Cluster struct {
	Name        string         `json:"name" yaml:"name"`
	Cluster     ClusterRef     `json:"cluster" yaml:"cluster"`
	OtherFields map[string]any `json:"-" yaml:",inline"`
}

type ClusterRef struct {
	Server                   string         `json:"server,omitempty" yaml:"server,omitempty"`
	CertificateAuthorityData string         `json:"certificate-authority-data,omitempty" yaml:"certificate-authority-data,omitempty"`
	InsecureSkipTLSVerify    bool           `json:"insecure-skip-tls-verify,omitempty" yaml:"insecure-skip-tls-verify,omitempty"`
	OtherFields              map[string]any `json:"-" yaml:",inline"`
}

type User struct {
	Name        string         `json:"name" yaml:"name"`
	User        map[string]any `json:"user" yaml:"user"`
	OtherFields map[string]any `json:"-" yaml:",inline"`
}

type Context struct {
	Name        string         `json:"name" yaml:"name"`
	Context     ContextRef     `json:"context" yaml:"context"`
	OtherFields map[string]any `json:"-" yaml:",inline"`
}

type ContextRef struct {
	Cluster     string         `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	User        string         `json:"user,omitempty" yaml:"user,omitempty"`
	OtherFields map[string]any `json:"-" yaml:",inline"`
}

type Extension struct {
	Name      string `json:"name" yaml:"name"`
	Extension any    `json:"extension" yaml:"extension"`
}

type MergeOptions struct {
	Replace           bool
	SetCurrentContext bool
}

func CandidatePaths(explicitPath string) ([]string, error) {
	if explicitPath != "" {
		path, err := expandUserPath(explicitPath)
		if err != nil {
			return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
		}
		return []string{path}, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		seen := map[string]bool{}
		paths := []string{}
		for _, path := range strings.Split(env, string(os.PathListSeparator)) {
			if path == "" {
				continue
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
		if len(paths) > 0 {
			return paths, nil
		}
	}
	path, err := defaultGlobalKubeconfigPath()
	if err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func ParseJSON(data []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, fmt.Errorf("parse kubeconfig template: %w", err)
	}
	return doc, nil
}

func SourceInfo(doc Document) (clusterName string, userName string, server string, port int, err error) {
	if len(doc.Contexts) == 0 {
		err = errors.New("kubeconfig has no context")
		return
	}
	context := doc.Contexts[0]
	clusterName = context.Context.Cluster
	userName = context.Context.User
	for _, cluster := range doc.Clusters {
		if cluster.Name == clusterName {
			server = cluster.Cluster.Server
			break
		}
	}
	if server == "" {
		err = fmt.Errorf("kubeconfig cluster %q has no server", clusterName)
		return
	}
	parsed, parseErr := url.Parse(server)
	if parseErr != nil {
		err = fmt.Errorf("parse kubeconfig server: %w", parseErr)
		return
	}
	portText := parsed.Port()
	if portText == "" {
		err = fmt.Errorf("kubeconfig server %q has no explicit port", server)
		return
	}
	port, err = strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		err = fmt.Errorf("kubeconfig server %q has invalid port", server)
		return
	}
	return
}

func PathForMerge(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no kubeconfig paths")
	}
	if len(paths) == 1 {
		return paths[0], nil
	}
	for _, path := range paths {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat kubeconfig %q: %w", path, err)
		}
	}
	return paths[len(paths)-1], nil
}

func defaultGlobalKubeconfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}

func ResolvePath(path string) (string, error) {
	return expandUserPath(path)
}

func expandUserPath(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if len(path) > 1 && os.IsPathSeparator(path[1]) {
		return filepath.Join(home, path[2:]), nil
	}
	return "", fmt.Errorf("unsupported home path %q", path)
}

func MergeFile(targetPath string, sourcePath string, name string, options MergeOptions) error {
	return withKubeconfigLock(targetPath, func() error {
		source, err := ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read VM kubeconfig: %w", err)
		}
		if err := requireEntry(source, name); err != nil {
			return err
		}
		target, err := ReadFile(targetPath)
		if err != nil {
			return fmt.Errorf("read kubeconfig %q: %w", targetPath, err)
		}
		if !options.Replace && hasEntry(target, name) {
			return fmt.Errorf("kubeconfig %q already has entry %q; use --replace to overwrite it", targetPath, name)
		}
		target = removeEntry(target, name)
		target = appendEntry(target, source, name)
		if options.SetCurrentContext {
			target.CurrentContext = name
		}
		normalizeHeader(&target)
		return WriteFileAtomic(targetPath, target)
	})
}

func UnmergeFile(path string, name string) (bool, error) {
	if stat, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat kubeconfig %q: %w", path, err)
	} else if stat.IsDir() {
		return false, fmt.Errorf("kubeconfig %q is a directory", path)
	}
	changed := false
	err := withKubeconfigLock(path, func() error {
		doc, err := ReadFile(path)
		if err != nil {
			return fmt.Errorf("read kubeconfig %q: %w", path, err)
		}
		if entryReferencedByOtherContext(doc, name) {
			return fmt.Errorf("kubeconfig %q has entry %q referenced by another context", path, name)
		}
		next := removeEntry(doc, name)
		if doc.CurrentContext == name {
			next.CurrentContext = ""
		}
		changed = !documentsEqual(doc, next)
		if !changed {
			return nil
		}
		normalizeHeader(&next)
		return WriteFileAtomic(path, next)
	})
	return changed, err
}

func withKubeconfigLock(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create kubeconfig directory: %w", err)
	}
	lockPath := path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("lock kubeconfig %q: %w", path, err)
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	}()
	return fn()
}

func ReadFile(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Document{}, nil
		}
		return Document{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return Document{}, nil
	}
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func WriteFileAtomic(path string, doc Document) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create kubeconfig directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create kubeconfig temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod kubeconfig temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write kubeconfig temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close kubeconfig temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace kubeconfig: %w", err)
	}
	cleanup = false
	return nil
}

func requireEntry(doc Document, name string) error {
	if len(findKubeconfigClusters(doc, name)) != 1 || len(findKubeconfigUsers(doc, name)) != 1 || len(findKubeconfigContexts(doc, name)) != 1 {
		return fmt.Errorf("VM kubeconfig does not contain exactly one %q cluster, user, and context", name)
	}
	return nil
}

func hasEntry(doc Document, name string) bool {
	return len(findKubeconfigClusters(doc, name)) > 0 || len(findKubeconfigUsers(doc, name)) > 0 || len(findKubeconfigContexts(doc, name)) > 0
}

func HasEntry(doc Document, name string) bool {
	return hasEntry(doc, name)
}

func appendEntry(target Document, source Document, name string) Document {
	target.Clusters = append(target.Clusters, findKubeconfigClusters(source, name)...)
	target.Users = append(target.Users, findKubeconfigUsers(source, name)...)
	target.Contexts = append(target.Contexts, findKubeconfigContexts(source, name)...)
	return target
}

func removeEntry(doc Document, name string) Document {
	next := doc
	next.Clusters = filterKubeconfigClusters(doc.Clusters, name)
	next.Users = filterKubeconfigUsers(doc.Users, name)
	next.Contexts = filterKubeconfigContexts(doc.Contexts, name)
	return next
}

func entryReferencedByOtherContext(doc Document, name string) bool {
	for _, context := range doc.Contexts {
		if context.Name == name {
			continue
		}
		if context.Context.Cluster == name || context.Context.User == name {
			return true
		}
	}
	return false
}

func normalizeHeader(doc *Document) {
	if doc.APIVersion == "" {
		doc.APIVersion = "v1"
	}
	if doc.Kind == "" {
		doc.Kind = "Config"
	}
}

func findKubeconfigClusters(doc Document, name string) []Cluster {
	out := []Cluster{}
	for _, cluster := range doc.Clusters {
		if cluster.Name == name {
			out = append(out, cluster)
		}
	}
	return out
}

func FindClusters(doc Document, name string) []Cluster {
	return findKubeconfigClusters(doc, name)
}

func findKubeconfigUsers(doc Document, name string) []User {
	out := []User{}
	for _, user := range doc.Users {
		if user.Name == name {
			out = append(out, user)
		}
	}
	return out
}

func FindUsers(doc Document, name string) []User {
	return findKubeconfigUsers(doc, name)
}

func findKubeconfigContexts(doc Document, name string) []Context {
	out := []Context{}
	for _, context := range doc.Contexts {
		if context.Name == name {
			out = append(out, context)
		}
	}
	return out
}

func FindContexts(doc Document, name string) []Context {
	return findKubeconfigContexts(doc, name)
}

func filterKubeconfigClusters(clusters []Cluster, name string) []Cluster {
	out := make([]Cluster, 0, len(clusters))
	for _, cluster := range clusters {
		if cluster.Name != name {
			out = append(out, cluster)
		}
	}
	return out
}

func filterKubeconfigUsers(users []User, name string) []User {
	out := make([]User, 0, len(users))
	for _, user := range users {
		if user.Name != name {
			out = append(out, user)
		}
	}
	return out
}

func filterKubeconfigContexts(contexts []Context, name string) []Context {
	out := make([]Context, 0, len(contexts))
	for _, context := range contexts {
		if context.Name != name {
			out = append(out, context)
		}
	}
	return out
}

func documentsEqual(a Document, b Document) bool {
	aBytes, aErr := yaml.Marshal(a)
	bBytes, bErr := yaml.Marshal(b)
	if aErr != nil || bErr != nil {
		return false
	}
	return string(aBytes) == string(bBytes)
}
