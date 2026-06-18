# spind

spind is a fast local development environment for people who build web applications that use the Kubernetes API, and for people who build Kubernetes Operators.

It supports kind-based and k3d-based Kubernetes environments and makes clean environments fast to create, reset, and start.

## Why spind?

Kubernetes development and testing often need a clean Kubernetes environment. Creating that environment every time is slow.

Common setup steps can take seconds or minutes:

- `kind create cluster` can take about 30 seconds.
- `helm install` can take several minutes.
- End-to-end tests and integration tests often need a clean cluster.
- CI runs can spend several minutes only preparing the environment.

During development, you often create an environment, test something, break it, delete it, and create it again. That wait makes the development loop slow.

spind reduces setup time from minutes to less than 1000 ms by restoring a ready VM snapshot.

This makes development, manual checks, end-to-end tests, integration tests, CI, and CD faster.

## How it works

spind is built around microVMs and snapshots.

On macOS, spind starts a small Linux VM with Virtualization.framework. On Linux, spind starts a small Linux VM with Cloud Hypervisor.

The Linux VM is not a special Kubernetes system. It is a small Docker machine. You can use your normal Docker client, kind, kubectl, and helm.

spind sets up the runtime VM once, then saves that ready state as a snapshot. After that, spind restores from the snapshot. This lets you start an environment where the Kubernetes cluster and Helm charts are already prepared.

## Use cases

- Web applications that use the Kubernetes API
- Kubernetes Operator development
- Local Kubernetes development with kind or k3d
- Clean environments for end-to-end tests and integration tests
- Faster Kubernetes environment setup in CI/CD

## Install

Install spind with `go install`.

```sh
go install github.com/suin/spind/cmd/spind@latest
```

After installing, check your host dependencies.

```sh
spind doctor
```

## Dependencies

`spind doctor` checks the dependencies needed on your current host. This section gives the basic list.

Common:

- Go
- Docker CLI

macOS:

- Apple Swift compiler
- `codesign`
- Xcode Command Line Tools
- A macOS environment that supports Virtualization.framework

Linux:

- A Linux environment with KVM support
- `/dev/kvm`
- `cloud-hypervisor`
- `passt`
- `virtiofsd`

Docker image builds need the Docker CLI. On Linux, `passt` and `virtiofsd` are also used for Docker host support, snapshot restore support, and host path sharing.

## Basic usage

Put `spind.yaml` in your project root.

```yaml
name: sample
image: docker
k8s: kind
setup:
  - "kind create cluster"
  - "kubectl --context kind-kind wait node --all --for=condition=Ready --timeout=180s"
```

Start the development environment.

```sh
spind up
```

On the first run, spind creates a VM, runs the commands in `setup`, and saves the ready state as a snapshot. Later runs restore from that snapshot.

## Guide for kind users

Use `k8s: kind` when your project uses kind.

```yaml
name: sample
image: docker
k8s: kind
setup:
  - "kind create cluster"
  - "kubectl --context kind-kind wait node --all --for=condition=Ready --timeout=180s"
```

Run `spind up`.

```sh
spind up
```

After the environment starts, spind prints shell exports for the restored VM.

```sh
export DOCKER_HOST='unix:///.../.spind/vms/sample/docker.sock'
export KUBECONFIG='/.../.spind/vms/sample/kubeconfig'
```

Use those values in the current shell, then use your usual tools.

```sh
kubectl get nodes
docker ps
```

spind does not update your global kubeconfig by default. The kubeconfig path printed by `spind up` is the VM-specific kubeconfig.

## Guide for k3d users

Use `k8s: k3d` when your project uses k3d.

```yaml
name: k3d-sample
image: docker
k8s: k3d
setup:
  - "k3d cluster create k3d-sample --registry-create k3d-sample-registry --kubeconfig-update-default=false"
  - 'k3d kubeconfig get k3d-sample > "$SPIND_KUBECONFIG"'
  - "kubectl --context k3d-k3d-sample wait node --all --for=condition=Ready --timeout=180s"
```

`SPIND_KUBECONFIG` is a path that spind sets for setup commands. Write the k3d kubeconfig there so spind can save it in the snapshot and rewrite it after restore.

When the k3d cluster has a local registry, spind detects it and prints `REGISTRY`.

```sh
export DOCKER_HOST='unix:///.../.spind/vms/k3d-sample/docker.sock'
export KUBECONFIG='/.../.spind/vms/k3d-sample/kubeconfig'
export REGISTRY='localhost:61702'
```

Use `REGISTRY` with the `DOCKER_HOST` value printed by spind.

```sh
docker build -t "$REGISTRY/my-app:dev" .
docker push "$REGISTRY/my-app:dev"
```

For k3d, `k3d registry list` may show a port that is different from the `REGISTRY` value printed by spind. In a spind environment, use the `REGISTRY` value printed by `spind up` or `spind vm start`.

See `examples/k3d/spind.yaml` for a k3d sample with cert-manager.

## Guide for Tilt users

Tilt works with the same `DOCKER_HOST` and `KUBECONFIG` values that spind prints.

Start the spind environment first.

```sh
spind up
```

Then export the values printed by spind in your shell.

```sh
export DOCKER_HOST='unix:///.../.spind/vms/tilt-sample/docker.sock'
export KUBECONFIG='/.../.spind/vms/tilt-sample/kubeconfig'
export REGISTRY='localhost:61702'
```

Tilt can auto-detect a k3d local registry through the `kube-public/local-registry-hosting` ConfigMap. For that flow, do not set `default_registry()` in the Tiltfile.

A minimal Tiltfile looks like this.

```python
allow_k8s_contexts("spind-tilt-sample")

docker_build("tilt-sample", ".")

k8s_yaml("k8s.yaml")

k8s_resource("tilt-sample", port_forwards=8080)
```

Run Tilt after the spind environment is ready.

```sh
tilt up
```

See `examples/tilt/` for a small Tilt sample.

## Development

Use these commands when working on this repository.

```sh
task build
task check
task test
task test:ts
```

Run end-to-end tests:

```sh
task e2e
```

## Design docs

Detailed design notes are in `docs/design/`.

- `docs/design/minimum-cli.md`
- `docs/design/project-up.md`
- `docs/design/docker-host.md`
- `docs/design/kind-ready-snapshot.md`
- `docs/design/snapshot.md`
- `docs/design/roadmap.md`
