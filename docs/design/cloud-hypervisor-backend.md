# Cloud Hypervisor Backend

## 目的

Linux/KVM環境で、既存の `spind vm create`、`spind vm start`、`spind vm stop`、`spind vm exec` をCloud Hypervisor backendで提供する。

## 対象スコープ

- Cloud Hypervisorによる作成済みVMの起動と停止。
- Cloud Hypervisorのvsockを使ったSSH over vsock exec。
- Cloud Hypervisor snapshot/restoreを使ったsaved state snapshot。
- Docker Host用guest NIC、`passt` vhost-user network、virtio-fs。
- Linux/KVM host上での手動e2e確認。

## 対象外

- Cloud Hypervisor以外のLinux backend。
- macOS上でCloud Hypervisorを実行すること。
- guest network/TCPやhost TCP port forwardingによるexec。
- host public interfaceへのDocker container port公開。

## Backend選択

- backend名は `cloud-hypervisor` とする。
- Linux/KVM環境では、`spind vm create` はbackendを指定しなくても `cloud-hypervisor` を自動選択する。
- ユーザー向けCLIではbackend選択optionを持たない。
- macOSのVirtualization.framework backendとCloud Hypervisor backendは、同じVM metadata modelを使うが、backend固有のrunner configを分ける。
- VM metadataにはbackend名を保存する。
- Cloud Hypervisor VMから作成したsnapshotは、Cloud Hypervisor backendのVM instanceとしてrestoreする。
- Cloud Hypervisor snapshotはbackendをまたいでVirtualization.frameworkへrestoreしない。

例:

```sh
spind vm create base --image docker
spind vm start base
spind vm exec base -- uname
spind vm stop base
```

## 実装境界

- ユーザー向けCLI、VM名解決、image store解決、状態管理はGo側が担当する。
- Cloud Hypervisor backendはGoで実装する。
- Swift実行部はVirtualization.framework backend専用であり、Cloud Hypervisor backendでは使わない。
- Go側はCloud Hypervisor binaryを長寿命processとして起動し、PIDとAPI socket pathをVM状態ファイルへ保存する。
- Cloud HypervisorのVM制御には、Go側で実装する薄いREST API clientを使う。
- REST API clientは、Cloud HypervisorのUnix domain socket APIへ直接requestを送る。
- snapshot作成はGo側REST API clientで行う。
- snapshot由来VMのrestore起動はGo側が `cloud-hypervisor --restore` を実行して行う。
- 初期実装では、Cloud Hypervisor用の非公式Go SDKを採用しない。
- `ch-remote` は手動デバッグ用途に限定し、spindの標準実装経路にはしない。

## VMデータ

Cloud Hypervisor backendのVMディレクトリには、共通ファイルに加えて次を置く。

```text
~/.spind/vms/<name>/
  cloud-hypervisor-config.json
  cloud-hypervisor-api.sock
  cloud-hypervisor-vsock.sock
  cloud-hypervisor-virtiofs.sock
  cloud-hypervisor.log
  cloud-hypervisor-event.log
  passt.sock
  passt.log
  virtiofsd.log
  serial.log
  kernel
  initramfs
  disk.img or image metadataで指定されたdisk群
```

- `cloud-hypervisor-config.json` は、Cloud Hypervisor起動引数に変換できるbackend固有設定である。
- `cloud-hypervisor-api.sock` はCloud Hypervisor REST API socketである。
- `cloud-hypervisor-vsock.sock` はCloud Hypervisorのhost側vsock socket pathである。
- `cloud-hypervisor-virtiofs.sock` はDocker Host用host path共有mountで使うvirtio-fs vhost-user socketである。
- `passt.sock` はDocker Host用guest NICで使う `passt` vhost-user socketである。
- `cloud-hypervisor.log` はVMM logである。
- `passt.log` はhost側 `passt` processのlogである。
- `virtiofsd.log` はhost側 `virtiofsd` processのlogである。
- `serial.log` はguest serial console logである。

## 起動

`spind vm start <name>` は、Cloud Hypervisor backendでは次の形のprocessを起動する。

```sh
cloud-hypervisor \
  --kernel <vm-dir>/kernel \
  --initramfs <vm-dir>/initramfs \
  --cmdline "<kernel-command-line>" \
  --disk path=<vm-dir>/disk.img,readonly=off,image_type=raw \
  --cpus boot=2 \
  --memory size=1024M \
  --rng src=/dev/urandom \
  --vsock cid=<cid>,socket=<vm-dir>/cloud-hypervisor-vsock.sock \
  --api-socket path=<vm-dir>/cloud-hypervisor-api.sock \
  --serial file=<vm-dir>/serial.log \
  --console off \
  --log-file <vm-dir>/cloud-hypervisor.log \
  --event-monitor path=<vm-dir>/cloud-hypervisor-event.log
```

- `cid` はVMごとに割り当て、状態ファイルへ保存する。
- 初期値のCPU数は `2`、memoryは `1024M` とする。
- image metadataにCPU数とmemory量がある場合、Cloud Hypervisor設定はそれを優先する。
- legacy imageでは、partitioned `disk.img` の第1partitionをroot filesystemとして使い、`root=/dev/vda1 rw` をkernel command lineへ追加する。
- `imageType=microvm-nix` のimageでは、metadataのkernel command lineをそのまま使い、spind側では `root=/dev/vda1 rw` を追加しない。
- `imageType=microvm-nix` のimageでは、metadataの `disks` に従って複数の `--disk` を渡す。標準の `docker` imageはread-only `nix-store.img` とwritable `docker-data.img` を使う。
- Cloud Hypervisorへ渡すdisk引数には `image_type=raw` を明示する。これはCloud Hypervisorがraw diskのsector 0 writeを禁止する自動判定経路を避けるためである。
- image metadataが公開鍵用kernel parameter名を持つ場合、VM作成時の公開鍵はroot filesystemへ書き込まず、kernel command lineへbase64で追加する。
- `start` はREST API socketが応答し、SSH over vsockが成功するまでexec readyとして扱わない。
- Docker Host用networkの詳細は `docs/design/docker-host-networking.md` に従う。
- Cloud Hypervisor backendでは、Docker Host用guest NICに `passt` のvhost-user backendを使う。
- Docker Host VMでは、Cloud Hypervisor起動前にhost側 `passt` processを起動する。
- `passt` は `--vhost-user --socket <vm-dir>/passt.sock --foreground --one-off --log-file <vm-dir>/passt.log --runas <uid>:<gid>` で起動する。
- Cloud Hypervisor起動時に `--memory size=<memory>M,shared=on` と `--net vhost_user=on,socket=<vm-dir>/passt.sock,vhost_mode=client,mac=<mac-address>` を追加する。
- `passt` process PID、socket path、log path、backend名、guest MAC addressはVM状態ファイルへ保存する。
- 通常起動で `passt` を設定できない場合でも、VM起動とexec readyが成功していれば `start` は成功する。ただし、Docker networkはunavailableとして表示する。
- snapshot restoreではsaved-state内のvhost-user-net deviceがbackend socketを必要とするため、`passt` を起動できない場合はrestore前に明確に失敗する。
- tap deviceとhost NATは、明示的なlegacy configがある場合だけ使う。
- Docker Host VMでhost path共有mountを使う場合、Cloud Hypervisor起動前にhost側 `virtiofsd` processを起動する。
- `passt` または `virtiofsd` を使う場合、Cloud Hypervisor起動時のmemory設定は `--memory size=<memory>M,shared=on` とする。
- `virtiofsd` が利用できる場合、Cloud Hypervisor起動時に `--fs tag=spind-cwd,socket=<vm-dir>/cloud-hypervisor-virtiofs.sock,num_queues=1,queue_size=512,dax=off` を追加する。
- `virtiofsd` が利用できない場合でも、VM起動とexec readyが成功していれば `start` は成功する。ただし、host path共有mountはunavailableとして表示する。

## Cloud Hypervisor Host Path Sharing

Cloud Hypervisor backendのhost path共有mountはvirtio-fsを使う。

Cloud Hypervisor backendのhost path共有mountは、通常起動VMだけを対象にする。

snapshot由来VMでは、高速saved-state restoreを優先し、host path共有mountを非対応にする。

Cloud Hypervisor v52.0では、snapshot restore後のvirtio-fs hotplug方式、およびsaved-state内のvirtio-fs deviceを維持してrestore前に同条件の `virtiofsd` へ再接続する方式が、実測上安定しない。

そのため、Cloud Hypervisor snapshot由来Docker Hostでは、Docker API、container実行、Docker pull、Docker network、port publishを保証対象にし、host path bind mountは保証対象に含めない。

標準のhost側daemonはRust版 `virtiofsd` binaryであり、実行名は `virtiofsd` とする。

`virtiofsd` はCloud Hypervisorを起動する前に、VMごとに1 process起動する。

標準socket path:

```text
~/.spind/vms/<name>/cloud-hypervisor-virtiofs.sock
```

標準log path:

```text
~/.spind/vms/<name>/virtiofsd.log
```

host側 `virtiofsd` は次の形で起動する。

```sh
virtiofsd \
  --socket-path=<vm-dir>/cloud-hypervisor-virtiofs.sock \
  --shared-dir=<host-cwd> \
  --cache=never \
  --sandbox=namespace
```

- `<host-cwd>` は `spind vm start` 実行時のcurrent working directoryである。
- `--cache=never` を標準にする。
- `--sandbox=namespace` を標準にする。
- `virtiofsd` のstdout/stderrは `virtiofsd.log` へ保存する。
- DAXは初期スコープでは使わず、Cloud Hypervisor側の `--fs` は `dax=off` とする。
- `tag` は `spind-cwd` を標準値にする。
- `num_queues` は `1`、`queue_size` は `512` を標準値にする。
- `virtiofsd` process PID、socket path、log path、shared host path、guest mount path、ready状態をVM状態ファイルへ保存する。
- `virtiofsd` の起動に失敗した場合、host path共有mountはunavailableとして扱い、VM起動とexec readyが成功していれば `spind vm start` は成功する。

guest側mountはguest mount helper経由で行う。

guest mount helperは次を実行する。

```sh
mount -t virtiofs spind-cwd <guest-cwd>
```

`<guest-cwd>` はhost current working directoryと同じabsolute pathである。

`spind vm stop` は次の順でcleanupする。

1. guest mount helperへunmount要求を送る。
2. Cloud Hypervisor processを停止する。
3. `virtiofsd` processを停止する。
4. `cloud-hypervisor-virtiofs.sock` を削除する。
5. stale PIDまたはstale socketがある場合、状態ファイルを修復する。

`spind snapshot create` は、snapshot保存後にCloud Hypervisor processを停止し、その後に `virtiofsd` processを停止する。

snapshot artifactには、`virtiofsd` PID、socket file、host path内容を含めない。

snapshot由来VMの `spind vm start` は、新しい `virtiofsd` processやvirtio-fs socketを用意しない。

snapshot由来VMのCloud Hypervisor restore用 `config.json` にvirtio-fs deviceが含まれる場合、spindはrestore前に該当deviceを使わない構成へ正規化する。

snapshot由来VMでは、guest mount helperによるhost path共有mountの再設定を行わない。

snapshot由来VMのrestore、resume、またはexec ready待ちに失敗した場合、spindはCloud Hypervisor processをcleanupしてから失敗を返す。

Cloud Hypervisor snapshot由来Docker Hostでhost path bind mountが必要な場合、利用者はsaved-state restoreではなく通常起動VMを使う。

snapshot由来VMの `spind vm start <name>` は、Cloud Hypervisor backendでは次の形のprocessを起動する。

```sh
cloud-hypervisor \
  --restore source_url=file://<vm-dir>/cloud-hypervisor-snapshot,memory_restore_mode=ondemand \
  --api-socket path=<vm-dir>/cloud-hypervisor-api.sock \
  --log-file <vm-dir>/cloud-hypervisor.log \
  --event-monitor path=<vm-dir>/cloud-hypervisor-event.log
```

- restore起動では通常起動用の `--kernel`、`--initramfs`、`--cmdline`、`--disk` をCLI引数として再指定しない。
- restore元snapshot directoryに含まれるCloud Hypervisor `config.json`、`memory-ranges`、`state.json` を使う。
- restore元snapshot directoryは作成先VM directory内のcopyであり、`config.json` のhost側pathは作成先VM directory向けに書き換え済みである。
- restore後のVMはpaused状態なので、Go側はCloud Hypervisor REST API socketへresumeを要求する。
- `memory_restore_mode=ondemand` を標準とする。
- `memory_restore_mode=ondemand` が使えない場合、初期スコープではrestoreを失敗させる。
- `memory_restore_mode=ondemand` を使うhostでは、Cloud Hypervisor processから `userfaultfd` を利用できる必要がある。
- restore後、`start` はREST API socketが応答し、resumeが成功し、SSH over vsockが成功するまでexec readyとして扱わない。

## Snapshot

`spind snapshot create <snapshot-name> --vm <vm-name>` は、Cloud Hypervisor backendでは次の順でsnapshotを作成する。

1. 対象VMが起動中かつexec readyであることを確認する。
2. Cloud Hypervisor REST API socketへpauseを要求する。
3. snapshot directoryへCloud Hypervisor snapshotを要求する。
4. VMの `kernel`、`initramfs`、Cloud Hypervisor設定に含まれるdisk群をsnapshot directoryへcopyする。
5. VMのexec用 `ssh_key` と `ssh_key.pub` をsnapshot directoryへcopyする。
6. spindの `metadata.json` を保存する。
7. Cloud Hypervisor processを停止し、元VMを停止済みに更新する。

Cloud Hypervisor snapshot directoryは次を含む。

```text
~/.spind/snapshots/<snapshot-name>/cloud-hypervisor/
  config.json
  memory-ranges
  state.json
  kernel
  initramfs
  disk.img or image metadataで指定されたdisk群
```

- Cloud Hypervisorが作成する `config.json`、`memory-ranges`、`state.json` は組として扱う。
- `kernel`、`initramfs`、disk群も同じsnapshot作成時点のartifactとして扱い、別snapshotのstateと組み合わせない。
- `create --snapshot` は、Cloud Hypervisor snapshot artifactを作成先VM directoryへcopyし、restore用 `config.json` のhost側pathを作成先VM directory向けへ書き換える。
- `create --snapshot` は、作成先VM directory直下にも `kernel`、`initramfs`、disk群を配置する。
- `create --snapshot` は、restore用 `config.json` 内のdisk pathも作成先VM directory向けへ書き換える。
- `create --snapshot` は、`memory-ranges` と `state.json` の内容を書き換えない。
- snapshot作成中に同じVMへ `exec`、`stop`、追加snapshot作成を行おうとした場合、どちらかの操作を失敗させる。
- snapshot作成に失敗した場合、途中作成されたsnapshot directoryは成功扱いにしない。
- Cloud Hypervisor snapshotから作成したVM instanceは、restore前にdiskへ新しい公開鍵を注入しない。

## 停止

`spind vm stop <name>` は、Cloud Hypervisor backendでは次の順で停止する。

1. Cloud Hypervisor REST API socketへVM shutdownを要求する。
2. VM processの終了を待つ。
3. timeoutした場合、VMM shutdownを要求する。
4. それでも終了しない場合、Cloud Hypervisor processへ `SIGTERM` を送る。
5. さらにtimeoutした場合、`SIGKILL` を送る。
6. Docker API relay、port publish relay、host loopback listenerを停止する。
7. guest mount helperへunmount要求を送れる場合は送る。
8. `virtiofsd` processを停止する。
9. `passt` process、vhost-user socket、legacy tap deviceを削除する。
10. stale socketを削除する。
11. 状態ファイルを停止済みに更新する。

- `stop` は保存されたPIDだけに依存しない。
- REST API socketが存在する場合、APIによる停止を優先する。
- stale PIDの場合は、状態ファイルを停止済みに修復する。
- stale `passt` process、stale vhost-user socket、stale tap、stale `virtiofsd`、stale socketを検出した場合、削除または状態修復する。
- cleanupで通常操作対象外のhost path内容を削除しない。

## Exec

- `spind vm exec` はSSH over vsockを使う。
- guest側では、OpenSSH serverが起動している。
- OpenSSH serverがvsockを直接listenできない場合、guest側proxyがvsock port `10222` とguest localhost SSH portを中継する。
- host側では、GoのCloud Hypervisor backendが `cloud-hypervisor-vsock.sock` を使ってSSH byte streamをguest側vsock endpointへ中継する。
- host側では、`cloud-hypervisor-vsock.sock` へ接続したあと、Cloud Hypervisorのhost側vsock socket protocolでguest SSH vsock port `10222` への接続を要求する。
- guest側vsock portへの接続が確立した後のbyte streamはSSH protocolとして扱う。
- host側relayはSSH payloadを解釈しない。
- `spind vm exec` はVMごとのSSH秘密鍵と `spind` ユーザーで接続する。

## 状態管理

VM状態ファイルにはCloud Hypervisor backend用に次を保存する。

- backend名 `cloud-hypervisor`
- Cloud Hypervisor process PID
- REST API socket path
- host側vsock socket path
- guest CID
- Docker Host network backend名
- Docker Host network backend process PID
- Docker Host network backend socket path
- Docker Host network backend log path
- Docker Host用legacy tap device name
- Docker Host用guest MAC address
- Docker Host network ready状態
- Docker Host用virtiofsd process PID
- Docker Host用virtiofsd socket path
- Docker Host用virtiofsd log path
- Docker Host用shared host path
- Docker Host用shared guest path
- Docker Host path sharing ready状態
- guest SSH vsock port
- serial log path
- VMM log path
- event monitor log path
- exec ready状態

## 動作確認

Cloud Hypervisor backendの受け入れ確認は、`docs/design/minimum-cli-runbook.md` の共通チェックとCloud Hypervisor固有チェックに従う。

確認コマンド:

```sh
devbox run task build
devbox run task build-image-docker
devbox run task e2e
```

`tests/e2e/vm-snapshot.test.ts` は少なくとも次を確認する。

- `spind vm create base --image docker`
- `spind vm start base`
- `spind vm exec base -- uname`
- `spind vm exec base -- pwd`
- `spind vm exec base -- ls`
- `spind vm exec base -- sh -lc 'echo hello > /tmp/spind-e2e.txt'`
- `spind vm exec base -- cat /tmp/spind-e2e.txt`
- `spind snapshot create prepared --vm base`
- `spind vm create from-prepared --snapshot prepared`
- `spind vm start from-prepared`
- `spind vm exec from-prepared -- cat /tmp/spind-e2e.txt`
- `spind vm stop from-prepared`

## 受け入れ基準

- Linux/KVM hostで `/dev/kvm` が利用できない場合、Cloud Hypervisor backendのe2eは明確に失敗する。
- Linux/KVM host上の `spind vm create` はVM metadataへ `cloud-hypervisor` backend名を保存する。
- `start` はCloud Hypervisor process、REST API socket、vsock socket、log pathを状態ファイルへ保存する。
- `exec` はSSH over vsockで動作し、guest network/TCPを使わない。
- `exec` はCloud Hypervisor host側vsock socket protocolでguest SSH vsock portへ接続する。
- `snapshot create` はCloud Hypervisor REST APIでpauseとsnapshotを行う。
- snapshot由来VMの `spind vm start` は `cloud-hypervisor --restore` を使い、restore後にREST APIでresumeする。
- `stop` はGo内製REST API clientでREST API socketを使い、必要に応じてprocess signalへfallbackする。
- e2e完了後、Cloud Hypervisor processが残っていない。
- Docker Host用Cloud Hypervisor e2eでは、通常起動VMでguest NIC、Docker pull、container外向き通信、host loopbackへのport publish、host path共有mountが確認できる。
- Docker Host用Cloud Hypervisor e2eでは、snapshot由来VMでDocker API、container実行、Docker pull、container外向き通信、host loopbackへのport publishが確認できる。
- Docker Host用Cloud Hypervisor e2eでは、snapshot由来VMのhost path共有mountを必須確認にしない。
- Linux/KVM環境では `passt` と `virtiofsd` が必須であり、存在しない場合はDocker Host e2eを失敗させる。
- Docker Host e2e完了後、Cloud Hypervisor process、`passt` process、vhost-user socket、legacy tap device、`virtiofsd` process、virtio-fs socketが残っていない。

## 参照

- Cloud Hypervisor Quick Start: https://www.cloudhypervisor.org/docs/prologue/quick-start/
- Cloud Hypervisor Commands: https://www.cloudhypervisor.org/docs/prologue/commands/
- Cloud Hypervisor API: https://raw.githubusercontent.com/cloud-hypervisor/cloud-hypervisor/main/docs/api.md
- Cloud Hypervisor Snapshot/Restore: https://raw.githubusercontent.com/cloud-hypervisor/cloud-hypervisor/main/docs/snapshot_restore.md
- Cloud Hypervisor virtio-fs: https://intelkevinputnam.github.io/cloud-hypervisor-docs-HTML/docs/fs.html
- passt manual: https://passt.top/builds/latest/web/passt.1.html
- Rust virtiofsd options: https://docs.rs/crate/virtiofsd/latest
