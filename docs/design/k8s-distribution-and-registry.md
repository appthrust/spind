# Kubernetes Distribution and k3d Registry

## 目的

spindは、kindだけでなくk3dも選べるKubernetes-ready snapshotを提供する。

この文書は、既存のkind-ready snapshotを `k8s: kind|k3d` に広げ、spindが提示する `DOCKER_HOST` 環境からk3d local registryを使えるようにする実装方針を定める。

## 決定

- `spind.yaml` は `k8s: kind|k3d` を使う。
- `spind snapshot create` は `--k8s=kind|k3d` を使う。
- 既存の `kind: true` と `--kind` との互換性は持たない。
- k3d registry対応は今回のk3d対応スコープに含める。
- Docker/k3d containerの `HostPort` は書き換えない。
- spindはregistry target portと必要なrelay portを管理する。
- spindは `kube-public/local-registry-hosting` の `localRegistryHosting.v1.host` を、`DOCKER_HOST` が指すVM Docker daemonから使えるregistry URLへ更新する。
- `localRegistryHosting.v1.hostFromContainerRuntime` と `hostFromClusterNetwork` はk3dの値を維持する。
- `k3d registry list` が表示するportと、hostから使えるregistry portが一致しない場合がある。これは制限事項として扱う。
- `spind up` と `spind vm start` は、registryが利用できる場合に `REGISTRY=localhost:<port>` をshell export候補として表示する。このURLは、同時にexportされる `DOCKER_HOST` のDocker daemonから見えるregistry URLである。
- `REGISTRY_FROM_CLUSTER` は表示しない。

## 設定

`spind.yaml` のKubernetes distribution指定:

```yaml
name: sample
image: docker
k8s: k3d
setup:
  - "k3d cluster create sample --registry-create sample-registry --kubeconfig-update-default=false"
  - "k3d kubeconfig get sample > \"$KUBECONFIG\""
  - "kubectl wait node --all --for=condition=Ready --timeout=180s"
```

kindの場合:

```yaml
name: sample
image: docker
k8s: kind
setup:
  - "kind create cluster --kubeconfig \"$KUBECONFIG\""
  - "kubectl wait node --all --for=condition=Ready --timeout=180s"
```

`k8s` を省略した場合はKubernetes-ready snapshotを作らない。

`k8s` に `kind` または `k3d` 以外の値が指定された場合は設定エラーにする。

## CLI

snapshot作成:

```sh
spind snapshot create dev-ready --vm dev-base --k8s=kind
spind snapshot create dev-ready --vm dev-base --k8s=k3d
```

`--kubeconfig` と `--context` は既存と同じ意味を持つ。

```sh
spind snapshot create dev-ready \
  --vm dev-base \
  --k8s=k3d \
  --kubeconfig /path/to/kubeconfig \
  --context k3d-dev
```

`spind snapshot create` の `--k8s` 省略時は、通常snapshotを作る。

## 内部モデル

`CreateOptions.Kind bool` はdistributionを表す値に置き換える。

```go
type Distribution string

const (
	K8sNone Distribution = ""
	K8sKind Distribution = "kind"
	K8sK3d  Distribution = "k3d"
)

type CreateOptions struct {
	K8s            Distribution
	KubeconfigPath string
	Context        string
}
```

既存の `kind` packageとmetadata名は、実装時に `k8s` へ改名する。

snapshot artifactはdistributionを保存する。

```json
{
  "k8sReady": true,
  "distribution": "k3d",
  "sourceKubeconfigPath": "...",
  "sourceContext": "k3d-sample",
  "sourceCluster": "k3d-sample",
  "sourceUser": "admin@k3d-sample",
  "sourceServer": "https://0.0.0.0:59569",
  "apiServerTargetPort": 59569,
  "nodes": []
}
```

`kindReady` は新しいartifactでは使わない。

## Kubernetes API検出

spindは、distributionごとにKubernetes API serverのVM内target portを検出する。

kind:

- `io.x-k8s.kind.role=control-plane` labelを持つcontainerを探す。
- またはcontainer名が `-control-plane` で終わるcontainerを探す。
- そのcontainerの `6443/tcp` published portをAPI server target portとする。

k3d:

- `k3d.role=loadbalancer` labelを持つcontainerを探す。
- そのcontainerの `6443/tcp` published portをAPI server target portとする。
- k3dのserver containerではなくload balancer containerを見る。

kubeconfig server hostの扱い:

- kindはloopback hostを要求する。
- k3dは `0.0.0.0`、`127.0.0.1`、`localhost`、IPv6 loopbackを受け入れる。
- k3dのkubeconfig templateは保存してよいが、restore後に生成するVM別kubeconfigではspindのAPI server relay URLへ必ず差し替える。

## Restore

Kubernetes-ready snapshotから作成されたVMの `spind vm start` は次を行う。

1. Docker API endpointをreadyにする。
2. Kubernetes API server用のhost空きportを割り当てる。
3. Kubernetes API serverへのhost loopback relayを起動する。
4. snapshot内のkubeconfig templateからVM別kubeconfigを生成する。
5. kubeconfig内のserverを `https://127.0.0.1:<api-relay-port>` に書き換える。
6. cluster、user、context名を `spind-<vm-name>` に書き換える。
7. `kubectl --kubeconfig <vm-kubeconfig> get nodes` 相当のready checkを行う。
8. k3d registryがある場合はregistry relayを起動する。
9. `local-registry-hosting` ConfigMapの `host` をVM Docker daemonから見えるregistry URLへ更新する。

registry relayとConfigMap更新は、Kubernetes API ready check成功後に行う。

## k3d Registry検出

k3d registry containerは、VM内Docker APIから検出する。

対象container:

- `k3d.role=registry` labelを持つ。
- 対象k3d clusterに接続されているregistryを優先する。
- cluster専用registryと共有registryの両方を扱えるようにする。

registry target port:

- containerの `5000/tcp` published portをVM内target portとする。
- 実際にlistenしているportを知るため、Docker APIの `NetworkSettings.Ports` を優先する。
- `NetworkSettings.Ports` が取れない場合は `HostConfig.PortBindings` を見る。
- label `k3s.registry.port.external` は補助情報として扱う。

`k3d registry list` は通常 `HostConfig.PortBindings` 由来のportを表示する。spindは、`DOCKER_HOST` が指すDocker daemonから使えるregistry URLのsource of truthとして、spindの出力と `localRegistryHosting.v1.host` を使う。

## Registry Relay

spindはVM内registry target portを検出する。必要に応じてhost側relayも張れるが、`REGISTRY` と `localRegistryHosting.v1.host` は `DOCKER_HOST` が指すVM Docker daemonから見えるURLを使う。

```text
host localhost:<registry-relay-port>
  -> spind relay
  -> VM Linux 127.0.0.1:<registry-target-port>
  -> registry container:5000
```

relay port割り当て:

- VM内target portと同じ番号をhost側で確保できる場合は、その番号を優先する。
- 使えない場合は別の空きportを割り当てる。
- 割り当てたrelay portはVM状態ファイルに保存する。
- 再起動時に保存済みportが使える場合は再利用する。
- 複数VMが同時に起動する場合、host側relay portは衝突しないようにする。

Docker/k3d containerの `HostPort` は変更しない。Dockerのpublished portはcontainer作成時の設定であり、後から安全に追加や変更をしない。

## localRegistryHosting

k3dは `kube-public/local-registry-hosting` ConfigMapでlocal registryを広告する。

spindはrestore後に、このConfigMapの `data.localRegistryHosting.v1` を読み、`host` だけを更新する。

更新後の例:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: localhost:52793
    hostFromContainerRuntime: k3d-sample-registry:5000
    hostFromClusterNetwork: k3d-sample-registry:5000
    help: https://k3d.io/stable/usage/registries/#using-a-local-registry
```

更新ルール:

- `host` は `localhost:<registry-target-port>` にする。これは `DOCKER_HOST` が指すVM Docker daemonから見えるregistry URLである。
- `hostFromContainerRuntime` は既存値を維持する。
- `hostFromClusterNetwork` は既存値を維持する。
- `help` は既存値を維持する。
- ConfigMapが存在しない場合は、registry情報から作成する。
- 複数registryがある場合は、対象clusterに接続されているregistryを優先する。判断できない場合はエラーにする。

Tiltなどのlocal registry auto-detectは、このConfigMapの `host` をpush先として使う。spindの推奨shell exportでは `DOCKER_HOST` もVMへ向くため、`host` はVM Docker daemonから見えるURLである必要がある。

## 起動時出力

registryが利用できる場合、`spind vm start` はregistry情報を表示する。

```text
started VM "sample"
docker: unix:///Users/suin/.spind/vms/sample/docker.sock
kubernetes: ready
kubeconfig: /Users/suin/.spind/vms/sample/kubeconfig
context: spind-sample
api server: https://127.0.0.1:49231
registry: localhost:52793
```

`spind up` はshell export候補に `REGISTRY` を含める。

POSIX shell:

```sh
export DOCKER_HOST='unix:///Users/suin/.spind/vms/sample/docker.sock'
export KUBECONFIG='/Users/suin/.spind/vms/sample/kubeconfig'
export REGISTRY='localhost:52793'
```

fish:

```fish
set -gx DOCKER_HOST 'unix:///Users/suin/.spind/vms/sample/docker.sock'
set -gx KUBECONFIG '/Users/suin/.spind/vms/sample/kubeconfig'
set -gx REGISTRY 'localhost:52793'
```

`REGISTRY_FROM_CLUSTER` は表示しない。

## JSON出力

VM statusやstart結果のJSONにはregistry情報を含める。

```json
{
  "kubernetes": {
    "ready": true,
    "distribution": "k3d",
    "kubeconfig": "/Users/suin/.spind/vms/sample/kubeconfig",
    "context": "spind-sample",
    "apiServer": "https://127.0.0.1:49231"
  },
  "registry": {
    "ready": true,
    "url": "localhost:52793",
    "relayPort": 61234,
    "targetPort": 52793,
    "localRegistryHostingUpdated": true
  }
}
```

`registry.url` は、`DOCKER_HOST` が指すDocker daemonからpush/pullするためのURLである。

## 制限事項

- `k3d registry list` はVM内Docker hostから見たportを表示する。host側relay portと一致しない場合がある。
- `DOCKER_HOST` が指すDocker daemonからpush/pullするregistry URLは、spindの出力または `localRegistryHosting.v1.host` を正とする。
- Docker API responseを書き換えて、`docker inspect` や `k3d registry list` をhost側relay portに見せることはしない。
- spindはk3d registry containerを再作成しない。
- 複数registryがあり対象clusterとの関係を一意に判断できない場合は、自動選択しない。

## E2E

k3d対応のE2Eは、kind-ready E2Eとは別に追加する。

基本E2E:

1. Docker imageからbase VMを作る。
2. base VMを起動する。
3. `k3d cluster create` でclusterを作る。
4. `spind snapshot create --k8s=k3d` でsnapshotを作る。
5. snapshotからwork VMを作る。
6. work VMを起動する。
7. 生成されたkubeconfigで `kubectl get nodes` が成功する。

registry E2E:

1. `spind.yaml` のsetupで `k3d cluster create --registry-create ...` を実行し、registry付きclusterを作る。
2. setup内で小さなtest imageをbuildし、VM内Dockerからk3d registryへpushする。
3. snapshotを作る。
4. snapshotからwork VMを起動する。
5. `REGISTRY` が出力される。
6. spindが出力した `DOCKER_HOST` を使い、Docker clientから `docker pull "$REGISTRY/<image>:<tag>"` が成功する。
7. cluster側でそのimageを使うPodが起動できる。

registry E2Eでは、`local-registry-hosting` ConfigMapの値確認を主検証にしない。`REGISTRY` 経由の `docker pull` により、snapshot内registry dataと、`DOCKER_HOST` が指すDocker daemonからのregistry URLが実際に使えることを確認する。

Tilt連携は別のacceptance E2Eとして扱う。

Tilt acceptance E2E:

1. k3d registry付きsnapshotからwork VMを起動する。
2. Tiltfileに `default_registry()` を書かず、Tiltのlocal registry auto-detectを使う。
3. Tiltが `local-registry-hosting` を読んでimage build、push、deployを完了できることを確認する。
4. PodがReadyになることを確認する。

## 実装順序

1. 設定とCLIを `k8s: kind|k3d` / `--k8s=kind|k3d` に変更する。
2. internal modelとmetadataを `kind-ready` からKubernetes-readyへ改名する。
3. kind adapterを既存実装から移す。
4. k3d API server adapterを追加する。
5. k3d kubeconfig server hostの受け入れとrestore時差し替えを実装する。
6. k3d registry検出を追加する。
7. registry relayを追加する。
8. `local-registry-hosting` ConfigMap更新を追加する。
9. `REGISTRY` の起動時表示とshell exportを追加する。
10. JSON出力にKubernetes distributionとregistry情報を追加する。
11. kindとk3dのE2Eを通す。
