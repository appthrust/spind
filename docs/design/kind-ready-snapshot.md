# Kind Ready Snapshot

## 目的

spindは、kind clusterが起動済みのDocker Host VMをsaved state snapshot化し、次回以降に短時間でKubernetes APIへ接続できるVMを作成できる。

初期スコープでは、spindはkind clusterの作成や削除を直接管理しない。

利用者はhost側のkindとDocker clientを使ってclusterを作成し、spindはその状態をsnapshot artifactとして保存、restore時にhostから使えるkubeconfigを生成する。

Cloud Hypervisor backendでは、kindの標準cluster設定でkind-ready snapshotを作成できる。spindはrestore時にhost側の空きportを割り当て、VM別kubeconfigのserverを `https://127.0.0.1:<allocated-port>` に再生成する。

Virtualization.framework backendでは、hostからguest IPへ直接relayする経路があるため、必要に応じてkindの `apiServerAddress` や証明書SANを明示したcluster設定を使う。

## 対象スコープ

- kind cluster起動済みDocker Host VMからkind-ready snapshotを作成する。
- snapshot作成時に、host側kubeconfigとcontextを取り込む。明示指定がない場合は現在のkubectl設定を使う。
- snapshot artifactにkubeconfig templateとkind metadataを保存する。
- snapshot由来VMの `spind vm start` 時に、空きhost portを割り当てる。
- Kubernetes API serverへのhost loopback port relayを起動する。
- VM別kubeconfigを `~/.spind/vms/<name>/kubeconfig` に生成する。
- `spind vm list` でKubernetes接続情報を表示する。
- Virtualization.framework backendとCloud Hypervisor backendの両方でkind-ready snapshotを扱う。

## 対象外

- `spind kind` command。
- spindによる `kind create cluster` の実行。
- spindによるkind node container lifecycle管理。
- spindによるkubeconfigの `~/.kube/config` への自動merge。
- spindによるkind cluster削除。
- 複数kubeconfig fileへのmerge。
- kind以外のKubernetes distributionの自動検出。

## ユーザー体験

kind-ready snapshotを作成する利用者は、通常のDocker HostとしてVMを起動し、host側kindでclusterを作る。

```sh
spind image build docker
spind vm create kind-base --image docker
spind vm start kind-base

export DOCKER_HOST=unix://$HOME/.spind/vms/kind-base/docker.sock
kind create cluster --name dev
kubectl --context kind-dev get nodes

spind snapshot create kind-ready --vm kind-base --kind
```

次回以降、利用者はkind-ready snapshotからVMを作成して起動する。

```sh
spind vm create work --snapshot kind-ready
spind vm start work

export KUBECONFIG=$HOME/.spind/vms/work/kubeconfig
kubectl get nodes
```

`spind vm start work` は次を表示する。

```text
started VM "work"
docker: unix:///Users/suin/.spind/vms/work/docker.sock
kubernetes: ready
kubeconfig: /Users/suin/.spind/vms/work/kubeconfig
context: spind-work
api server: https://127.0.0.1:<port>
```

## Snapshot作成

`spind snapshot create <snapshot-name> --vm <vm-name> --kind` は、kind-ready snapshotを作成する。

- `--kind` は、このsnapshotをkind-ready snapshotとして扱う明示optionである。
- `--kubeconfig <path>` はhost側kubeconfig pathである。省略時は `KUBECONFIG` が1 pathならそれを使い、未設定なら `~/.kube/config` を使う。
- `--context <name>` は取り込むcontext名である。省略時は選択したkubeconfigの `current-context` を使う。
- `KUBECONFIG` が複数pathの場合、snapshot対象を一意に扱えないため `--kubeconfig` の明示指定を要求する。
- spindは、解決したcontextのAPI serverが `127.0.0.1`、`localhost`、またはIPv6 loopbackを指し、そのportが対象VM内のkind control-plane containerのDocker公開portと一致することを確認する。

snapshot作成時にspindは次を行う。

1. 対象VMが起動中かつexec readyであることを確認する。
2. 対象VMでDocker API readyであることを確認する。
3. kubeconfig pathを解決する。
4. contextを解決する。
5. 解決したcontextのAPI serverが対象VMのDocker上のkind control-plane公開portと一致することを確認する。
6. 解決したcontextからcluster、user、server、certificate authority、client certificate、client key、tokenのうち存在する情報を取り込む。
7. `kubectl --kubeconfig <path> --context <name> get nodes` 相当のready checkを行う。
8. ready check成功後、通常のsaved state snapshotを作成する。
9. snapshot metadataへkind-ready情報を保存する。

ready checkでは、少なくともKubernetes APIへ接続でき、node一覧を取得できることを確認する。

nodeが `Ready` であることは初期スコープの必須条件にする。

`kube-system` の全podがRunningであることは初期スコープでは必須にしない。

## Snapshot Artifact

kind-ready snapshotは、通常snapshot artifactに加えて次を保存する。

```text
~/.spind/snapshots/<snapshot-name>/
  kind/
    kubeconfig.template
    metadata.json
    certs/
```

`kind/metadata.json` には次を保存する。

- `kindReady: true`
- 元kubeconfig path。
- 元context名。
- 元cluster名。
- 元user名。
- 元server URL。
- API server container port。
- snapshot作成時のnode一覧。
- snapshot作成時のready check結果。

`kubeconfig.template` はrestore後にhost側portを差し替えられる形にする。

template内のcontext、cluster、user名はsnapshot作成時点の名前を保持してよい。

restore後に生成するVM別kubeconfigでは、context、cluster、user名を `spind-<vm-name>` に書き換える。

## Restore Start

kind-ready snapshotから作成されたVMの `spind vm start <name>` は、通常のsnapshot restoreに加えて次を行う。

1. Docker API endpointをreadyにする。
2. Kubernetes API server用のhost空きportを割り当てる。
3. Kubernetes API serverへのhost loopback port relayを起動する。
4. `kind/kubeconfig.template` からVM別kubeconfigを生成する。
5. kubeconfig内のserverを `https://127.0.0.1:<allocated-port>` に書き換える。
6. kubeconfig内のcluster、user、context名を `spind-<vm-name>` に書き換える。
7. `~/.spind/vms/<name>/kubeconfig` に保存する。
8. `kubectl --kubeconfig ~/.spind/vms/<name>/kubeconfig get nodes` 相当のready checkを行う。

host空きportは `spind vm start` 時に割り当てる。

割り当てたportはVM状態ファイルへ保存する。

既に保存済みのportがあり、そのportが利用可能な場合は再利用してよい。

保存済みportが利用できない場合は、新しい空きportを割り当て、kubeconfigを再生成する。

## Kubeconfig

初期スコープでは、spindは `~/.kube/config` へ自動mergeしない。

標準出力先:

```text
~/.spind/vms/<name>/kubeconfig
```

VM別kubeconfigの命名規則:

- cluster名: `spind-<vm-name>`
- user名: `spind-<vm-name>`
- context名: `spind-<vm-name>`
- current-context: `spind-<vm-name>`

`<vm-name>` は既存のVM名validationに従う。

`~/.spind/vms/<name>/kubeconfig` が既に存在する場合、`spind vm start` は同じVM用の生成物として上書きしてよい。

`~/.kube/config` へのmergeは将来の明示optionで扱う。

merge方針と切り替え条件は `docs/design/kubeconfig-merge.md` に従う。

## Port Relay

kind-ready snapshotのKubernetes API serverは、host loopbackへだけ公開する。

```text
127.0.0.1:<allocated-port>
```

public interfaceへは公開しない。

複数のkind-ready VMを同時起動する場合、VMごとに別のhost portを割り当てる。

port衝突時は新しい空きportを割り当てる。

Kubernetes API server用port relayは、Docker Hostのport publish relayと同じcleanup対象である。

## Backend差分

Virtualization.framework backend:

- snapshot由来VMでもhost path共有mountを使える。
- kind workloadがhost path bind mountを使う場合も初期スコープで扱える。

Cloud Hypervisor backend:

- snapshot由来VMでは高速saved-state restoreを優先し、host path共有mountを非対応にする。
- kind API、node container、Docker pull、container network、port publishは保証対象にする。
- hostPath volumeやhost bind mountに依存するkind workloadはsnapshot由来VMでは制限ありとして扱う。
- 通常起動VMではhost path共有mountを使える。

## `spind vm list`

kind-ready VMの `spind vm list <name>` は次を表示する。

- Kubernetes support状態。
- Kubernetes ready状態。
- kubeconfig path。
- context名。
- API server URL。
- API server port relay PID。
- ready check結果。
- unavailableまたはunsupportedの理由。
- 関連log path。

例:

```text
kubernetes: ready
kubeconfig: /Users/suin/.spind/vms/work/kubeconfig
context: spind-work
api server: https://127.0.0.1:49321
```

Cloud Hypervisor snapshot由来VMでhost path共有mountが非対応でも、Kubernetes APIがreadyなら `kubernetes: ready` と表示する。

## Cleanup

`spind vm stop <name>` は、kind-ready VMで次をcleanupする。

- Kubernetes API server用port relay。
- host loopback listener。
- VM状態ファイル内のKubernetes ready状態。

`~/.spind/vms/<name>/kubeconfig` はVM固有生成物であり、`spind vm stop` では削除しない。

`~/.kube/config` は初期スコープでは変更しないため、cleanup対象外である。

## E2E

kind-ready e2eはDocker Host e2eとは別のテストケースにする。

Taskfile task:

```sh
task e2e
```

確認手順:

```sh
spind vm create kind-base --image docker
spind vm start kind-base
export DOCKER_HOST=unix://$HOME/.spind/vms/kind-base/docker.sock
kind create cluster --name dev
spind snapshot create kind-ready --vm kind-base --kind
spind vm create kind-work --snapshot kind-ready
spind vm start kind-work
KUBECONFIG=$HOME/.spind/vms/kind-work/kubeconfig kubectl get nodes
spind vm list kind-work
spind vm stop kind-work
```

Cloud Hypervisor backendでは、snapshot由来VMのhost path共有mountを確認対象にしない。

## 受け入れ基準

- `--kind` 付きでkind-ready snapshotを作成できる。
- `--kubeconfig` と `--context` を省略した場合、選択されたkubeconfigとcurrent-contextを使う。
- 解決したcontextが対象VMのkind control-plane公開portを指していない場合は失敗する。
- 指定contextが存在しない場合は失敗する。
- snapshot作成前ready checkで `kubectl get nodes` 相当が成功する。
- nodeが `Ready` でない場合は失敗する。
- kind-ready snapshotから別VMを作成できる。
- `spind vm start` がKubernetes API server用host空きportを割り当てる。
- VM別kubeconfigが `~/.spind/vms/<name>/kubeconfig` に生成される。
- VM別kubeconfigのcontext、cluster、user名は `spind-<vm-name>` である。
- `KUBECONFIG=~/.spind/vms/<name>/kubeconfig kubectl get nodes` が成功する。
- 複数kind-ready VMを同時起動してもhost portとkubeconfig pathが衝突しない。
- `~/.kube/config` は自動変更されない。
- `spind vm stop` 後、Kubernetes API server用port relayとhost listenerが残らない。
- Virtualization.framework backendでkind-ready e2eが通る。
- Cloud Hypervisor backendでkind-ready e2eが通る。
