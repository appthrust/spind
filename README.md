# spind

spind is a fast local development environment for people who build web applications that use the Kubernetes API, and for people who build Kubernetes Operators.

It supports kind-based Kubernetes environments and makes clean environments fast to create, reset, and start.

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

spind sets up the runtime VM once, then saves that ready state as a snapshot. After that, spind restores from the snapshot. This lets you start an environment where the kind cluster and Helm charts are already prepared.

## Use cases

- Web applications that use the Kubernetes API
- Kubernetes Operator development
- Local Kubernetes development with kind
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
kind: true
setup:
  - "kind create cluster"
  - "kubectl --context kind-kind wait node --all --for=condition=Ready --timeout=180s"
```

Start the development environment.

```sh
spind up
```

On the first run, spind creates a VM, runs the commands in `setup`, and saves the ready state as a snapshot. Later runs restore from that snapshot.

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
