# Snapshot

## 目的

spindは、起動済みVMのsaved stateをsnapshotとして保存し、そのsnapshotから別のVM instanceをrestoreして、短時間で使える状態へ戻す。

## 対象スコープ

- Virtualization.framework backendのsaved state snapshot。
- Cloud Hypervisor backendのsnapshot/restore。
- 起動中かつexec readyなVMからsnapshotを作成する。
- snapshotから新しいVM instanceを作成する。
- snapshotから作成したVM instanceを `spind vm start` でcold bootせずrestoreする。
- restore後に `spind vm exec` を使える。
- kind-ready snapshotとして、起動済みkind clusterとkubeconfig templateを保存する。

## 対象外

- 停止済みVMのdisk copyだけをsnapshotとして扱う方式。
- memory状態を含まないdisk-only snapshot。
- 差分snapshot。
- snapshot chain。
- snapshotへの上書き保存。
- snapshotから既存VMを巻き戻す操作。
- backendをまたいだsnapshot restore。
- spindによるkind cluster作成。
- `~/.kube/config` への自動merge。

## 用語

- saved state snapshotは、VMのmemory状態、device状態、実行中VMに対応するwritable diskを含むrestore元artifactである。
- VM instanceは、`~/.spind/vms/<name>/` に作成される起動可能なVMである。
- snapshotから作成されるVM instanceは、snapshot元VMとは別のVMである。
- snapshot由来VMの `spind vm start` は、通常起動ではなくsaved state restoreである。

## 保存場所

snapshotは `~/.spind/snapshots/<snapshot-name>/` に保存する。

共通必須ファイル:

```text
~/.spind/snapshots/<snapshot-name>/
  metadata.json
  ssh_key
  ssh_key.pub
  kind/                 # kind-ready snapshotの場合だけ存在する
```

- `metadata.json` にはsnapshot名、作成元VM名、作成時刻、backend名、memory size、CPU数、guest SSH vsock port、kernel command lineに必要な情報を保存する。
- `ssh_key` と `ssh_key.pub` は、snapshot作成時点のexec用SSH鍵である。
- `ssh_key` のfile modeは `0600` である。
- snapshot metadataにはPID、socket path、起動中状態を含めない。
- saved state snapshotでは、restore前にsnapshotの `disk.img` 内の `/home/spind/.ssh/authorized_keys` を差し替えない。
- saved stateとdiskは対応する組として扱い、片方だけを別snapshotや別VMのartifactと組み合わせない。
- host path共有mountの内容はsnapshot artifactに含めない。
- kind-ready snapshotの場合、kind metadataとkubeconfig templateをsnapshot artifactに含める。

Virtualization.framework backendの必須ファイル:

```text
~/.spind/snapshots/<snapshot-name>/
  vf/
    state.vzvmsave
    kernel
    initramfs
    disk.img
```

- `vf/state.vzvmsave` はVirtualization.frameworkのsaved state fileである。
- `vf/disk.img` はsnapshot作成時点のVM writable diskのcopyである。
- `vf/kernel` と `vf/initramfs` はrestore用VM構成を作るために必要な起動artifactである。

Cloud Hypervisor backendの必須ファイル:

```text
~/.spind/snapshots/<snapshot-name>/
  cloud-hypervisor/
    config.json
    memory-ranges
    state.json
    kernel
    initramfs
    disk.img
```

- `cloud-hypervisor/config.json` はCloud Hypervisorが保存したVM configurationである。
- `cloud-hypervisor/memory-ranges` はguest memory内容である。
- `cloud-hypervisor/state.json` はCloud Hypervisorが保存したVM stateである。
- `cloud-hypervisor/kernel` と `cloud-hypervisor/initramfs` は、`config.json` が参照するpayload artifactと同じ世代のcopyである。
- `cloud-hypervisor/disk.img` はsnapshot作成時点のVM writable diskのcopyである。
- Cloud Hypervisorのsnapshot directoryは、Cloud Hypervisor restoreの `source_url=file://...` として使える形にする。
- Cloud Hypervisor snapshotの `config.json` にはhost側pathが含まれるため、snapshotからVM instanceを作成するときに作成先VM directory向けへ書き換えたcopyを作る。

## CLI

### `spind snapshot create <snapshot-name> --vm <vm-name>`

- `<vm-name>` のVMから `<snapshot-name>` のsaved state snapshotを作成する。
- `<vm-name>` のVMが存在しない場合、失敗する。
- `<vm-name>` のVMが起動中でない場合、失敗する。
- `<vm-name>` のVMがexec readyでない場合、失敗する。
- `<vm-name>` のbackendが `virtualization-framework` または `cloud-hypervisor` でない場合、失敗する。
- `<snapshot-name>` が既に存在する場合、上書きせずに失敗する。
- Virtualization.framework backendのsnapshot作成は、Swift実行部にsaved state保存を要求し、`vf/state.vzvmsave`、`vf/disk.img`、`vf/kernel`、`vf/initramfs`、必要なmetadataを保存して行う。
- Cloud Hypervisor backendのsnapshot作成は、Cloud Hypervisor REST APIでVMをpauseし、snapshot directoryへ `config.json`、`memory-ranges`、`state.json` を保存し、対応する `kernel`、`initramfs`、`disk.img` と必要なmetadataを保存して行う。
- `--k8s=kind|k3d --kubeconfig <path> --context <name>` が指定された場合、Kubernetes-ready snapshotとして扱い、詳細は `docs/design/k8s-distribution-and-registry.md` に従う。
- snapshot作成後、元VMは停止済みとして扱う。

### `spind snapshot list`

- snapshot store内のsnapshot metadataを読み取り、一覧表示する。
- 表示項目には、snapshot名、backend名、作成元VM名、作成時刻、CPU数、memory sizeを含める。
- 壊れたsnapshot metadataを見つけた場合は、無視せず明確に失敗する。

### `spind snapshot delete <snapshot-name>`

- `<snapshot-name>` のsnapshot directoryを削除する。
- `<snapshot-name>` が存在しない場合、失敗する。
- snapshotから作成済みのVM instanceは削除しない。
- snapshot削除後も、既に作成済みのsnapshot由来VMは自分のVM directory内のrestore artifactで起動できる。

### `spind vm create <name> --snapshot <snapshot-name>`

- `<snapshot-name>` のsaved state snapshotから、新しいVM instance `<name>` を作成する。
- `<snapshot-name>` が存在しない場合、失敗する。
- `<name>` のVMが既に存在する場合、上書きせずに失敗する。
- 作成されたVM instanceは、snapshot元VMとは別の `~/.spind/vms/<name>/` を持つ。
- 作成先VMには、snapshotの `disk.img` とrestoreに必要なartifactをcopyする。
- 作成されたVM instanceは、snapshotとは独立して書き込める。
- backendはsnapshot metadataのbackend名を使う。
- `create --snapshot` は新しいVM用SSH鍵を生成しない。
- `create --snapshot` はsnapshotの `ssh_key` と `ssh_key.pub` を作成先VMへcopyする。
- snapshot由来VMは、初期スコープではsnapshot作成元VMと同じexec用SSH鍵を使う。
- restore前にdiskへ新しい公開鍵を注入する方式は、saved state内のguest memoryやfilesystem cacheとdisk内容の対応を壊す可能性があるため採用しない。
- snapshot由来VMごとのSSH鍵rotationは、restore後に別機能として行う余地を残すが、初期スコープ外である。
- 作成先VM metadataには、snapshot由来であること、参照元snapshot名、restore用saved state pathを保存する。
- kind-ready snapshotから作成する場合、作成先VM metadataにはkind-ready由来であることと、生成先kubeconfig pathを保存する。
- Virtualization.framework snapshot由来VMでは、作成先VMへ `kernel`、`initramfs`、`disk.img`、`state.vzvmsave` を配置する。
- Cloud Hypervisor snapshot由来VMでは、作成先VMへ `kernel`、`initramfs`、`disk.img` と `cloud-hypervisor-snapshot/` を配置する。
- Cloud Hypervisor snapshot由来VMでは、作成先VMの `cloud-hypervisor-snapshot/config.json` 内のdisk path、vsock socket path、serial log path、console/log/event pathなどhost側pathを作成先VM directory向けに書き換える。
- Cloud Hypervisor snapshot由来VMでは、作成先VMの `cloud-hypervisor-snapshot/memory-ranges` と `cloud-hypervisor-snapshot/state.json` はsnapshot artifactからcopyし、内容を書き換えない。

## Swift実行部

- Swift実行部は、Virtualization.framework backend専用の薄い実行部である。
- Swift実行部は、通常起動、停止、saved state保存、saved state restoreを扱う。
- 通常起動時のSwift実行部は、exec relay用Unix socketとは別にcontrol用Unix socketをlistenする。
- control用Unix socket pathはGo側がVM状態ファイルへ保存する。
- Go側は、Virtualization.framework snapshot作成時にcontrol用Unix socketへsave要求を送る。
- save要求には、保存先 `state.vzvmsave` pathを含める。
- control用Unix socketのmessageはnewline-delimited JSONとする。
- save要求は `{"op":"save","statePath":"<path>"}` とする。
- save成功responseは `{"ok":true}` とする。
- save失敗responseは `{"ok":false,"error":"<message>"}` とする。
- Swift実行部は、save要求を受け取ると保持中の `VZVirtualMachine` にsaved state保存を要求する。
- Swift実行部は、saved state保存が完了するまでsuccessを返さない。
- Swift実行部は、save成功後にVMを停止済みとして扱える状態にし、processを終了する。
- Go側は、Swift実行部のsave成功とprocess終了を確認した後に、元VMの `disk.img`、`kernel`、`initramfs`、`ssh_key`、`ssh_key.pub`、metadataをsnapshot directoryへcopyする。
- Go側は、copy完了後に元VMの状態ファイルを停止済みに更新する。
- saved state restoreでは、Go側から渡されたVM構成、restore元 `state.vzvmsave`、exec relay socket path、control socket pathだけを使う。
- snapshot由来VMのrestore起動では、Swift実行部に `restore --config <path>` を渡す。
- restore用configには、restore元 `state.vzvmsave` path、exec relay socket path、control socket path、log pathを含める。
- Swift実行部は、restore起動中のlifecycle eventをnewline-delimited JSONで標準出力へ出す。
- restore起動中のlifecycle eventは、少なくとも `restore-completed`、`resume-completed`、`exec-relay-ready` を持つ。
- Go側は、Swift実行部のlifecycle eventとexec relay socketの接続確認を使って、restore完了、resume完了、exec readyを区別する。
- Swift実行部は、snapshot名、VM名、image store、snapshot storeの探索を行わない。

## Cloud Hypervisor実行部

- Cloud Hypervisor backendのsnapshot/restoreはGo側で実装する。
- Go側は、Cloud Hypervisor REST API socketへpause、snapshot、resume、shutdown要求を送る。
- snapshot作成では、Cloud Hypervisor VMをpauseし、snapshot保存完了後にCloud Hypervisor processを停止する。
- Cloud Hypervisor snapshotはrestore時にpaused状態で復元されるため、`start` はrestore完了後にresumeを要求する。
- Cloud Hypervisor restoreでは、`cloud-hypervisor --restore source_url=file://<snapshot-dir>` を使う。
- 初期設定では `memory_restore_mode=ondemand` を使う。
- `memory_restore_mode=ondemand` が使えない環境ではrestoreを失敗させ、暗黙にcopy方式へfallbackしない。
- copy方式を使う場合は、利用者が明示的に設定した場合だけにする。
- Cloud Hypervisorの `prefault=on` は `memory_restore_mode=ondemand` と併用しない。

例:

```sh
spind snapshot create prepared --vm base
spind vm create worker --snapshot prepared
spind vm start worker
```

## Restore

- snapshotから作成されたVM instanceに対する `spind vm start <name>` は、backend固有のsaved state restoreを使う。
- Virtualization.framework backendでは、Swift実行部がGo側から渡されたVM構成とsaved state pathを使ってVMをrestoreする。
- Virtualization.framework backendでは、Swift実行部は `restoreMachineStateFrom` と `resume` を区別して実行し、可能な限り別々にlogへ記録する。
- Cloud Hypervisor backendでは、Go側がCloud Hypervisor processを `--restore source_url=file://<vm-dir>/cloud-hypervisor-snapshot,memory_restore_mode=ondemand` 付きで起動し、restore完了後にresume要求を送る。
- Cloud Hypervisor backendでは、restore完了、resume完了、`exec` readyを区別してlogへ記録する。
- `spind vm start` はrestore後、SSH over vsockが成功するまでexec readyとして扱わない。
- snapshot由来VMの `spind vm start` は、restore開始からexec readyまでのend-to-end時間を表示する。
- `spind vm start` がrestoreまたはexec ready待ちで失敗した場合、CLIは関連log pathを表示する。
- `spind vm start` はrestoreに失敗した場合、VM状態を起動中にしない。
- restoreで使うmemory size、CPU数、device構成、kernel、initramfs、kernel command lineはsnapshot作成時のmetadataと整合していなければならない。
- saved state snapshot由来VMでは、`spind vm start` 時にmemory sizeやCPU数を変更しない。
- restore attemptごとにwritable diskを共有しない。

## 状態管理

- snapshot自体は起動状態を持たない。
- `spind vm start` と `spind vm stop` はsnapshotを直接対象にしない。
- `spind vm exec` はsnapshotを直接対象にしない。
- snapshotから作成されたVM instanceは、通常のVMと同じ状態ファイルを持つ。
- snapshot作成元VMとsnapshot作成先VMは、PID、socket path、CIDを共有しない。
- snapshot由来VMは、初期スコープではsnapshot lineage内でexec用SSH鍵を共有する。
- snapshot由来VMの状態ファイルには、restoreで起動したSwift実行部のPIDとexec socket pathを保存する。
- Cloud Hypervisor snapshot由来VMの状態ファイルには、restoreで起動したCloud Hypervisor processのPID、REST API socket path、host側vsock socket pathを保存する。

## 整合性

- snapshot作成対象は、起動中かつexec readyなVMに限定する。
- snapshot作成前に、利用者はVM内の必要なserviceやfileをready状態まで作り込む。
- snapshot作成中に元VMへ `spind vm exec`、`spind vm stop`、追加のsnapshot作成を行おうとした場合、どちらかの操作を失敗させる。
- snapshot作成後すぐにrestoreする場合でも、backend固有のsaved state artifactと関連artifactが読み取り可能であることを確認してから成功扱いにする。
- snapshot作成時点のVM内時刻、process状態、service状態がrestore後に見えるため、長期間保存したsnapshotでは時刻差や期限切れの影響を受ける可能性がある。
- Docker Hostのhost path共有mountはsnapshotへ保存しない。
- Virtualization.framework backendのDocker Hostでは、snapshot由来VMの `spind vm start` 時にその時点のcurrent working directoryをguest mount helper経由で共有mountする。
- Cloud Hypervisor backendのDocker Hostでは、高速saved-state restoreを優先し、snapshot由来VMのhost path共有mountを非対応にする。
- Docker Hostのguest NIC backend接続、vhost-user socket、legacy host NAT、Docker API endpoint socket、port publish relay processはsnapshotへ保存しない。
- snapshot由来VMの `spind vm start` は、restore後にDocker Host networkingを再設定する。

## 性能計測

- saved state snapshotの価値は、restoreしてから利用可能になるまでのend-to-end時間で評価する。
- `restoreMachineStateFrom`、`resume`、`restore_start -> exec ready` は区別して計測する。
- Cloud Hypervisor backendでは、`restore_start -> restored paused`、`resume`、`restore_start -> exec ready` を区別して計測する。
- `resume` だけの時間を起動時間として扱わない。
- 受け入れ確認では、`spind vm start <snapshot由来VM>` の開始から `spind vm exec <name> -- true` 成功までの時間を記録する。

## 受け入れ基準

- 起動中かつexec readyなVirtualization.framework VMからsnapshotを作成できる。
- 起動中かつexec readyなCloud Hypervisor VMからsnapshotを作成できる。
- 停止中VMからsnapshotを作成しようとすると失敗する。
- 既存snapshot名へsnapshotを作成しようとすると失敗する。
- snapshotから別名のVM instanceを作成できる。
- Cloud Hypervisor snapshotから作成されたVM instanceはCloud Hypervisor backendでrestoreされる。
- snapshotから作成されたVM instanceは、元VMとは別のVM directory、disk、状態ファイルを持つ。
- snapshot由来VMは、snapshotのexec用SSH鍵を使ってrestore後のSSH接続を行う。
- snapshotから作成されたVM instanceを `spind vm start <name>` でrestore起動できる。
- restore起動後、`spind vm exec <name> -- true` が成功する。
- `spind snapshot list` で作成済みsnapshotを確認できる。
- `spind snapshot delete <snapshot-name>` でsnapshotを削除できる。
- snapshotから作成されたVM instanceで `spind vm exec <name> -- cat <file>` が使える。
- snapshotから作成されたVM instanceへ書き込んでも、snapshot元VMとsnapshot artifactの `disk.img` は変更されない。
