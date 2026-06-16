# Project Up

## 目的

spindは、プロジェクトごとの `spind.yaml` から、初回だけsetup済みsnapshotを作り、以後はそのsnapshotから開発VMを起動する `spind up` を提供する。

この機能は、独自の大きなprovisioning言語ではなく、プロジェクトに書かれた単純なsetup command列を実行する薄いorchestrationである。

## 設定

`spind up` はcurrent directoryから親directoryへ向かって `spind.yaml` を探す。

最小設定:

```yaml
setup:
  - kind create cluster --name dev
  - kubectl --context kind-dev wait node --all --for=condition=Ready --timeout=180s
```

設定項目:

- `name`: project名。省略時は `spind.yaml` があるdirectoryのbasename。
- `image`: base image名。省略時は `docker`。
- `kind`: kind-ready snapshotとして作るかどうか。省略時は `false`。
- `setup`: 初回provision時にhost側で実行するcommand列。省略時は空。

## 生成名

project名が `kido` の場合:

- work VM: `kido`
- provisioned snapshot: `kido-provisioned`
- 一時provisioning VM: `kido-provisioning`

`kido-provisioning` はsnapshot作成に成功したら削除する。setupやsnapshot作成に失敗した場合は調査のため残してよい。

## `spind up`

通常の `spind up` は次の順で動く。

1. `spind.yaml` を探してproject rootを決める。
2. `name` と `image` を決める。
3. imageがなければ `spind image build <image>` 相当を実行する。
4. `<name>-provisioned` snapshotがなければ、一時VMを作ってsetupを実行し、snapshotを作る。
5. `<name>` VMがなければ、snapshotから作る。
6. `<name>` VMを起動する。

setup commandはproject rootをworking directoryにして、host側で `/bin/sh -c` により実行する。

setup commandには次の環境変数を渡す。

- `DOCKER_HOST`: provisioning VMのDocker endpoint。
- `KUBECONFIG`: provisioning VM用に使う一時kubeconfig path。
- `SPIND_DOCKER_HOST`: `DOCKER_HOST` と同じ値。
- `SPIND_KUBECONFIG`: `KUBECONFIG` と同じ値。
- `SPIND_PROJECT_NAME`: project名。
- `SPIND_PROJECT_ROOT`: project root。
- `SPIND_VM_NAME`: provisioning VM名。

`kind: true` の場合、snapshot作成時にkind-ready snapshotとして扱い、setup commandへ渡した `KUBECONFIG` をsnapshot作成時にも使う。

起動後、Docker endpointまたはVM別kubeconfigが利用できる場合、`spind up` は現在のshellで使える環境変数設定を表示する。

`SHELL` のbasenameが `fish` の場合:

```fish
set -gx DOCKER_HOST 'unix:///Users/suin/.spind/vms/sample/docker.sock'
set -gx KUBECONFIG '/Users/suin/.spind/vms/sample/kubeconfig'
```

それ以外の場合:

```sh
export DOCKER_HOST='unix:///Users/suin/.spind/vms/sample/docker.sock'
export KUBECONFIG='/Users/suin/.spind/vms/sample/kubeconfig'
```

## `spind up --reprovision`

`--reprovision` は既存のprovisioned snapshotを使わず、setupからやり直す。

動作:

1. 同じproject rootに属する既存work VMを削除する。
2. 同じproject rootに属する既存provisioned snapshotを削除する。
3. 一時provisioning VMを作る。
4. setup commandを実行する。
5. provisioned snapshotを作る。
6. 一時provisioning VMを削除する。
7. work VMをsnapshotから作って起動する。

## 重複

project名の推論は、`spind.yaml` の `name`、なければproject root basenameだけを使う。

同名VMまたはsnapshotが既に存在する場合、metadataの `projectRoot` が現在のproject rootと一致する場合だけ再利用できる。

`projectRoot` が違う、またはproject metadataがない既存artifactと衝突した場合はエラーにする。自動suffixは付けない。利用者は `spind.yaml` の `name` で衝突を避ける。
