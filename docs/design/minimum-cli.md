# 最小CLI

## 目的

spind は、ローカルに用意されたベースイメージからVMを作成し、起動と停止を行うCLIを提供する。

## 対象スコープ

- ベースイメージからVMを作成する。
- 作成済みVMを起動する。
- 起動中または作成済みVMを停止する。
- 停止済みVMを削除する。
- 起動中VMからsaved state snapshotを作成する。
- snapshotから別のVM instanceを作成する。
- VM一覧とVM詳細を確認する。
- snapshot一覧表示とsnapshot削除を行う。
- snapshot UXの詳細表示、health表示、prune、失敗時診断を行う。
- VMをDocker Hostとしてhost Docker clientから利用できる。
- kind-ready snapshotからVMを作成し、host側kubectlでKubernetes APIへ接続できる。
- project rootの `spind.yaml` からsetup済みsnapshotを作り、開発VMを起動する。

## 対象外

- ベースイメージをユーザー向けCLIでビルドする機能。
- ベースイメージをネットワークから取得する機能。
- Kubernetes、Docker、kindの自動構築。
- spind CLIによるkind cluster lifecycle管理。
- 停止済みVMのdisk copyだけをsnapshotとして扱う方式。
- snapshotから既存VMを巻き戻す操作。
- spind CLIによるDocker container lifecycle管理。
- 条件分岐や依存関係を持つ独自provisioning言語。

## 実装境界

- ユーザー向けCLIはGoで実装する。
- CLI frameworkは `cobra` を用いる。
- CLI command、subcommand、option、argumentの定義は、手書きparserではなく `cobra.Command` のcommand treeを主に使う。
- `cobra` のcommand treeは、`vm create`、`vm start`、`vm stop`、`vm exec`、`snapshot create` のCLI構造を表現する。
- backendは `virtualization-framework` と `cloud-hypervisor` を扱う。
- Virtualization.frameworkを直接呼び出す部分はSwiftで実装する。
- Swift部分は、Virtualization.framework backendのVM起動、停止、saved state保存、saved state restoreに必要なVirtualization.framework呼び出しだけを担当する薄い実行部にする。
- Cloud Hypervisor backendはGoで実装し、Swift実行部を使わない。
- Cloud Hypervisor backendのsnapshot/restoreはGo側がCloud Hypervisor REST APIと `--restore` を使って実装する。
- CLI引数の解釈、設定ファイルの読み書き、image storeとVM管理ディレクトリの解決、エラー表示、状態管理はGo側が担当する。
- Go側は、backendに応じてSwift実行部またはCloud Hypervisor processを起動する。
- snapshot由来VMの `spind vm start` では、Go側がbackendに応じてrestore用saved state pathまたはsnapshot directoryを渡す。
- Swift実行部は、Go側から渡された明示的なパスと設定値だけを使う。
- Swift実行部は、spind固有の永続状態、image store探索、VM名解決を持たない。

## ベースイメージ

- Docker Host用の標準ベースイメージは、`spind image build docker` でビルドされる。
- spindは、`~/.spind/images/<image-name>/` にある名前付きベースイメージを参照する。
- `docker` という名前のベースイメージが `~/.spind/images/docker/` にある場合、ユーザーは `--image docker` と指定できる。
- legacy imageの必須ファイルは `metadata.json`, `kernel`, `initramfs`, `disk.img` である。
- Microvm.nix imageの必須ファイルは `metadata.json`, `kernel`, `initramfs` とmetadataの `disks` に書かれたdisk群である。
- `spind image list` は、ローカルimage storeにあるベースイメージを一覧表示する。
- `spind image delete <image-name>...` は、ローカルimage storeから対象ベースイメージのディレクトリを削除する。
- `disk.img` はpartition tableを持つdisk imageである。
- `disk.img` の第1partitionはlegacy image用のroot filesystemである。
- backendは、通常起動時のroot filesystemとして `root=/dev/vda1 rw` を使う。
- legacy imageのVM起動後の通常実行環境は、initramfsではなく `disk.img` 内のroot filesystemである。
- `spind vm exec` を利用できるベースイメージでは、`disk.img` 内にOpenSSH serverとexec用ユーザー設定が含まれる。
- OpenSSH serverはOpenRC serviceとしてdefault runlevelでVM起動時に常駐起動する。
- OpenSSH server自体がvsockをlistenできない場合、`disk.img` 内にはvsockとguest localhostのSSH portを中継する薄いproxyが含まれる。
- Docker Host用のimageとendpoint仕様は `docs/design/docker-host.md` に従う。
- Docker Host用のnetwork仕様は `docs/design/docker-host-networking.md` に従う。
- Docker Hostでは、`spind vm start` 実行時のcurrent working directoryをVM内へ同じabsolute pathで共有mountする。

## VMデータ

- `vm create` で作成されたVMデータは `~/.spind/vms/<name>/` に保存する。
- VMごとの writable disk、metadata、状態ファイルは、そのVMのディレクトリ内に置く。
- VMごとのSSH秘密鍵は `~/.spind/vms/<name>/ssh_key` に保存し、file modeは `0600` とする。
- VMごとのSSH公開鍵は `~/.spind/vms/<name>/ssh_key.pub` に保存する。
- `vm create <name> --image <image-name>` は、ベースイメージの必要なファイルを `~/.spind/vms/<name>/` にコピーしてVMを作成する。
- `vm create <name> --image <image-name>` は、VM用公開鍵をVMのwritable disk内の `/home/spind/.ssh/authorized_keys` へ注入する。
- `vm create <name> --snapshot <snapshot-name>` は、snapshotのexec用SSH鍵をVMディレクトリへcopyし、restore前のdiskへ新しい公開鍵を注入しない。
- VM名は `~/.spind/vms/` の直下のディレクトリ名として扱う。
- 同名ディレクトリが既に存在する場合、`vm create` は失敗する。

## Snapshotデータ

- snapshotは `~/.spind/snapshots/<snapshot-name>/` に保存する。
- snapshotは起動対象ではなく、restore起動するVM instanceを作るための読み取り元artifactである。
- snapshotの詳細は `docs/design/snapshot.md` に従う。
- snapshot UXの詳細は `docs/design/snapshot-ux.md` に従う。
- kind-ready snapshotの詳細は `docs/design/kind-ready-snapshot.md` に従う。
- kubeconfig merge方針は `docs/design/kubeconfig-merge.md` に従う。
- 実開発環境への投入段階は `docs/design/development-environment-rollout.md` に従う。
- project単位の `spind up` は `docs/design/project-up.md` に従う。
- 直近の実装roadmapは `docs/design/roadmap.md` に従う。

## Kubeconfig

- `spind kubeconfig merge <name>` は、VM別kubeconfigをglobal kubeconfigへ明示的にmergeする。
- `spind kubeconfig unmerge <name>` は、global kubeconfigからspind管理entryを明示的に削除する。
- `spind vm start` はglobal kubeconfigを自動変更しない。
- `spind vm start --merge-kubeconfig` はPhase 12では実装しない。

## Docker Host

- Docker Hostの詳細は `docs/design/docker-host.md` に従う。
- Docker Host networkingの詳細は `docs/design/docker-host-networking.md` に従う。
- `spind vm start` は、VM起動とexec readyを必須条件にする。
- Docker endpointは追加能力としてbest-effortで起動する。
- Docker endpointが使えない場合でも、VM起動とexec readyが成功していれば `spind vm start` は成功する。
- Docker Host用のhost path共有mountもbest-effortであり、失敗してもVM起動とexec readyが成功していれば `spind vm start` は成功する。
- Docker Host用のguest network、Docker bridge、port publish relayもbest-effortであり、失敗してもVM起動とexec readyが成功していれば `spind vm start` は成功する。

## プロセス管理

- `vm start` はbackendに応じた長寿命processを起動する。
- snapshot由来VMの `vm start` は、通常起動ではなくsaved state restoreでVMを起動する。
- Virtualization.framework backendでは、起動中の `VZVirtualMachine` インスタンスはSwift実行部プロセス内に保持される。
- Cloud Hypervisor backendでは、Cloud Hypervisor processがVM本体である。
- Go側は、起動したbackend processのPIDをVMの状態ファイルへ保存する。
- `vm stop` は保存されたPIDとbackend固有の制御経路を使って停止を要求する。
- Virtualization.framework backendでは、Swift実行部が保持中の `VZVirtualMachine` に停止要求を出す。
- Cloud Hypervisor backendでは、Cloud Hypervisor REST API socketを優先して停止要求を出す。
- 最小CLIでは、VM管理daemonやIPCによる制御は設計対象外である。

## CLI

- CLI引数のparse、help表示、補完script生成、必須optionの検証、unknown optionの扱いは `cobra` に寄せる。
- `cobra` で表現しにくいVM内コマンド列などの扱いだけ、薄い補助処理を追加してよい。
- VM操作は `spind vm <subcommand>` namespaceにまとめる。

### `spind vm create <name> --image <image-name> [--cpu <count>] [--memory <size>]`

- `<name>` のVMを作成する。
- `<image-name>` はローカルimage store上のベースイメージ名である。
- `--cpu` はVMのCPU数を指定し、image metadataの `cpuCount` を上書きする。
- `--memory` はVMのmemory量を指定し、image metadataの `memoryMiB` を上書きする。単位なしはMiBとして扱い、`MiB`、`GiB`、`TiB` も受け付ける。
- backendは実行platformから自動選択する。Linuxでは `cloud-hypervisor`、macOSでは `virtualization-framework` を使う。
- 選択されたbackend名をVM metadataへ保存する。
- 指定されたベースイメージが存在しない場合、VMを作成せずに失敗する。
- 同名のVMが既に存在する場合、既存VMを上書きせずに失敗する。
- 作成されたVMは、起動前の状態で管理される。

### `spind up [--reprovision]`

- current directoryから親directoryへ `spind.yaml` を探す。
- project名は `spind.yaml` の `name`、省略時はproject root basenameとする。
- image名は `spind.yaml` の `image`、省略時は `docker` とする。
- work VM名はproject名そのものとする。
- provisioned snapshot名は `<project>-provisioned` とする。
- 一時provisioning VM名は `<project>-provisioning` とする。
- provisioned snapshotがない場合、provisioning VMを作成し、`setup` command列をhost側で実行し、snapshotを作成する。
- setup commandには `DOCKER_HOST`、`KUBECONFIG`、`SPIND_DOCKER_HOST`、`SPIND_KUBECONFIG`、`SPIND_PROJECT_NAME`、`SPIND_PROJECT_ROOT`、`SPIND_VM_NAME` を渡す。
- `kind: true` の場合、snapshot作成時にkind-ready snapshotとして扱う。
- provisioning VMはsnapshot作成成功後に削除する。
- 起動後、Docker endpointまたはVM別kubeconfigが利用できる場合は、`SHELL` に応じてfishまたはPOSIX shell向けの環境変数設定を表示する。
- `--reprovision` は既存provisioned snapshotを使わず、setupからやり直す。
- 同名VMまたはsnapshotが別project rootに属する場合、またはproject metadataがない場合はエラーにする。

### `spind image build <image-name> [--config <source>] [--force] [--data-size <size>]`

- `<image-name>` のimageをbuildして local image storeへ配置する。
- `--config` が省略された場合、`template://<image-name>` を使う。
- `template://docker` は `~/.spind/templates/docker` を参照する。存在しない場合、リポジトリ内のbuiltin template `templates/docker/` をそこへ配置してから使う。
- build config sourceは `flake.nix` を含むdirectoryである。
- spindはconfig source内で `nix build` を実行し、flakeのdefault packageを使う。
- build結果の `metadata.json` を正とし、そこに書かれたdisk一覧をimage storeへ配置する。
- `--force` が指定されていない場合、同名imageが既に存在すると失敗する。
- `--data-size` はbuild結果metadataの `create` に従って作るwritable data diskのsizeを上書きする。
- buildが成功するまで、既存image directoryは置き換えない。

### `spind image list`

- local image storeにあるベースイメージを一覧表示する。
- 少なくともimage名、architecture、作成時刻、size、healthを表示する。
- `metadata.json` や必須ファイルが壊れているimageも一覧から除外せず、healthで問題を表示する。
- imageが存在しない場合も成功し、空であることを表示する。
- `--json` が指定された場合、機械処理用のJSONを出力する。

### `spind image delete <image-name>...`

- 対象imageの `~/.spind/images/<image-name>/` を削除する。
- 複数のimage名を指定できる。
- `<image-name>` のimageが存在しない場合、失敗する。
- VM metadataが対象image名を参照している場合、既定では削除せず、対象VMを削除するか `--force` を使うよう促す。
- `--force` が指定された場合、VM metadataが参照していてもimage directoryだけを削除する。
- image削除はVM instanceとsnapshot artifactを削除しない。
- snapshot metadata上のsource image参照は、self-containedなsnapshot artifactの来歴情報として扱い、image削除のblock条件にしない。
- 削除前にimage directory sizeを計測し、削除した容量を表示する。

### `spind vm create <name> --snapshot <snapshot-name>`

- `<snapshot-name>` のsnapshotからVMを作成する。
- snapshotから作成されたVMは、snapshot元VMとは別のVM instanceである。
- snapshotから作成されたVMは、起動前の状態で管理され、`start` 時にsaved state restoreされる。
- saved state snapshotではbackend overrideを受け付けない。
- snapshotからのVM作成では `--cpu` と `--memory` を受け付けない。saved stateとsnapshot metadataのresource量をそのまま使う。
- 詳細は `docs/design/snapshot.md` に従う。

### `spind snapshot create <snapshot-name> --vm <vm-name>`

- 起動中かつexec readyな `<vm-name>` からsaved state snapshotを作成する。
- 停止中VMからのsnapshot作成は失敗する。
- `--kind --kubeconfig <path> --context <name>` が指定された場合、kind-ready snapshotとして作成する。
- 詳細は `docs/design/snapshot.md` に従う。
- kind-ready snapshotの詳細は `docs/design/kind-ready-snapshot.md` に従う。

### `spind snapshot list`

- local snapshot storeにあるsnapshotを一覧表示する。
- 少なくともsnapshot名、backend名、作成元VM名、作成時刻を表示する。
- snapshotが存在しない場合も成功し、空であることを表示する。

### `spind snapshot delete <snapshot-name>`

- `<snapshot-name>` のsnapshot artifactを削除する。
- `<snapshot-name>` が存在しない場合、失敗する。
- 起動中VMや既存VM instanceは削除しない。
- snapshotから作成済みのVM instanceは削除しない。

### `spind snapshot inspect <snapshot-name>`

- `<snapshot-name>` の詳細を表示する。
- 表示項目とhealth checkは `docs/design/snapshot-ux.md` に従う。

### `spind snapshot prune`

- 条件に一致するsnapshotをまとめて削除する。
- `--all` で全snapshotを明示的に削除対象にできる。
- `--dry-run` で削除予定だけを表示できる。
- 詳細は `docs/design/snapshot-ux.md` に従う。

### `spind vm list [<name>]`

- `<name>` が指定された場合、そのVMの状態を表示する。
- `<name>` が省略された場合、local VM storeにあるVMを一覧表示する。
- VM詳細には、VM名、backend名、起動状態、exec ready状態、snapshot由来かどうか、必要に応じてlog pathを含める。
- Docker Host VMの詳細には、Docker API、Docker network、port publish relay、host path共有mountのsupport状態とready状態を含める。
- support状態は `supported` または `unsupported` として表す。
- ready状態は `ready` または `unavailable` として表す。
- `unsupported` は仕様上の非対応であり、VM起動失敗として扱わない。
- `unavailable` は対応対象だが環境不足や起動失敗により使えない状態であり、理由とlog pathを表示する。

### `spind vm start <name>`

- 作成済みVMを起動する。
- `<name>` のVMが存在しない場合、失敗する。
- 既に起動中の場合、追加のVMを起動しない。

### `spind vm stop <name>`

- 対象VMを停止する。
- `<name>` のVMが存在しない場合、失敗する。
- 既に停止している場合、破壊的な変更を行わない。

### `spind vm delete <name>...`

- 対象VMの `~/.spind/vms/<name>/` を削除する。
- 複数のVM名を指定できる。
- `<name>` のVMが存在しない場合、失敗する。
- 起動中VMは既定では削除せず、停止してから再実行するよう促す。
- `--force` が指定された場合、起動中VMを `stop` 相当のcleanupで停止してから削除する。
- `--unmerge-kubeconfig` が指定された場合、削除前に `spind kubeconfig unmerge <name>` 相当を実行する。
- 削除対象はVM instanceのディレクトリだけであり、image storeとsnapshot artifactは削除しない。
- 削除前にVM directory sizeを計測し、削除した容量を表示する。

## 受け入れ基準

- ローカルimage storeにある `docker` ベースイメージからVMを作成できる。
- 作成済みVMを起動できる。
- 起動したVMを停止できる。
- 停止済みVMを削除できる。
- 起動中VMの削除は既定では失敗し、`--force` ありで停止後に削除できる。
- `spind vm delete --unmerge-kubeconfig` はglobal kubeconfigから対象VMの `spind-<vm-name>` entryを削除できる。
- `image list` でローカルimage storeにあるimage一覧を確認できる。
- `image delete` で不要なimageを削除できる。
- VM metadataが参照するimageの削除は既定では失敗し、`--force` ありでimage directoryだけを削除できる。
- `vm stop` はPIDベースのローカル制御でVMを停止できる。
- 起動後のVMはimage metadataに定義されたroot filesystemを通常実行環境として使う。
- 起動中VMからsaved state snapshotを作成できる。
- snapshotから別のVM instanceを作成し、saved state restoreで起動できる。
- Cloud Hypervisor backendでは、Cloud Hypervisor snapshot/restoreでsnapshot由来VMを起動できる。
- `vm list` でVM一覧とVM詳細を確認できる。
- `snapshot list` でsnapshot一覧を確認できる。
- `snapshot delete` で不要なsnapshotを削除できる。
- `snapshot inspect` でsnapshot artifactとhealthを確認できる。
- `snapshot prune --dry-run` で削除予定snapshotと削除予定sizeを確認できる。
- `docker` imageから作成したVMをDocker Hostとして利用できる。
- Docker Hostでは、attached `docker run` のstdout/stderr/stdinがhost Docker clientへ返る。
- `docker` imageから作成したVMで `docker pull`、container外向き通信、host loopbackへの `docker run -p` が利用できる。
- Docker Hostのhost path共有mountはguest mount helper経由で設定できる。
- kind-ready snapshotから作成したVMは、`spind vm start` 後にVM別kubeconfigでKubernetes APIへ接続できる。
- Docker daemonが存在しないVMでも、Docker endpointなしで `spind vm start` が成功する。
- snapshot由来VMの `vm start` は、restore startからexec readyまでの所要時間を表示する。
- `vm start` 失敗時は、原因調査に使うlog pathを表示する。
- CLI command定義は `cobra` のcommand treeで表現され、主要なparse処理が手書き実装に依存していない。
- Go側だけを見れば、CLI仕様、パス解決、状態管理の大半を理解できる。
- Virtualization.framework backendでは、Swift側は薄い実行部として独立している。
- Cloud Hypervisor backendでは、`docs/design/cloud-hypervisor-backend.md` の仕様に従う。
- 実装完了のゲートとして、`docs/design/minimum-cli-runbook.md` の手動確認ランブックがVirtualization.framework backendとCloud Hypervisor backendの両方で通る。
