# spind手動確認ランブック

## 目的

このランブックは、spindの実装が受け入れ可能かを実装者が確認するためのゲートである。

実装完了の判定では、Cloud Hypervisor backendでこのランブックを通す。

backend固有の環境準備は分けるが、CLIとして同じ仕様である部分は共通チェックとして扱う。

## 対象backend

- `cloud-hypervisor`

## 前提

共通:

- `devbox` を使える。
- Taskfileから主要操作を実行できる。
- ローカルimage storeに `docker` ベースイメージを用意できる。
- hostにDocker clientがある。
- kind-ready snapshot確認ではhostに `kind` と `kubectl` がある。

Cloud Hypervisor:

- Linux/KVM環境上で実行する。
- `/dev/kvm` が存在する。
- `vm.unprivileged_userfaultfd=1` である。
- `passt` を `PATH` から実行できる。
- `virtiofsd` を `PATH` から実行できる。

## 実行手順

### Cloud Hypervisor

Linux/KVM環境上で実行する。

```sh
devbox run task build
devbox run task build-image-docker
devbox run task e2e
```

## 共通チェック

共通チェックは、通常 `task e2e` が実行する。

### Build

- spind CLIをビルドできる。
- 対象backendで必要な実行部をビルドできる。
- `spind image build docker` で `docker` imageをビルドできる。

### `image`

- `spind image list` でimage一覧を確認できる。
- `spind image list --json` でimage一覧をJSONで確認できる。
- `spind image list` は、`metadata.json` や必須ファイルが壊れたimageも一覧に出し、healthで問題を表示する。
- `spind image delete <image-name>` でimageを削除できる。
- 削除後、`~/.spind/images/<image-name>/` が存在しない。
- `spind image delete <image-name>` は削除した容量を表示する。
- VM metadataが対象image名を参照している場合、`spind image delete <image-name>` は失敗し、image directoryを残す。
- VM metadataが対象image名を参照している場合でも、`spind image delete <image-name> --force` はimage directoryだけを削除する。
- `spind image delete <image-name>` はVM instanceとsnapshot artifactを削除しない。

### `vm create`

- `spind vm create base --image docker` が成功する。
- `~/.spind/vms/base/` が作成される。
- ベースイメージから必要なファイルがVMディレクトリへコピーされる。
- VM metadataが保存される。
- metadataにbackend名が保存される。
- 同名VMが既に存在する場合、既存VMを上書きせずに失敗する。

### `vm start`

- `spind vm start base` が成功する。
- `base` VMが起動する。
- 起動中状態が記録される。
- exec readyになるまで待つ。
- 存在しないVM名では失敗する。
- 起動中VMに対して追加のVMを起動しない。

### `vm exec`

- 起動中の `base` VM内でコマンドを実行できる。
- `spind vm exec base -- uname` が成功する。
- `spind vm exec base -- pwd` が成功する。
- `spind vm exec base -- ls` が成功する。
- `spind vm exec base -- sh -lc 'echo hello > /tmp/spind-e2e.txt'` が成功する。
- `spind vm exec base -- cat /tmp/spind-e2e.txt` が `hello` を出力する。
- `spind vm exec base -- ping -c 1 -W 3 127.0.0.1` が成功する。
- VM内コマンドの標準出力と標準エラーが区別される。
- VM内コマンドの終了コードが `spind vm exec` の終了コードになる。
- 停止中VMに対しては失敗する。
- 存在しないVM名では失敗する。

### `vm list`

- `spind vm list base` で対象VMの状態、backend、exec ready、log pathを確認できる。
- `spind vm list` でVM一覧を確認できる。
- `spind vm list --json` でVM状態をJSONで確認できる。
- Docker Host VMでは、Docker API、Docker network、port publish relay、host path共有mountのsupport状態とready状態を確認できる。
- Cloud Hypervisor snapshot由来Docker Hostでは、host path共有mountが `unsupported` として表示される。
- 対応対象の機能が使えない場合は、`unavailable` と理由とlog pathを確認できる。
- `spind vm list --json` では、Docker Host能力ごとのsupport状態、ready状態、理由、log pathを確認できる。
- 存在しないVM名では失敗する。

### `snapshot`

- 起動中かつexec readyなVMからsaved state snapshotを作成できる。
- `spind snapshot create prepared --vm base` が成功する。
- snapshot作成後、`base` が停止済みとして扱われる。
- 停止中VMから `spind snapshot create stopped --vm base` を実行すると失敗する。
- `spind snapshot list` でsnapshotが表示される。
- `spind snapshot inspect prepared` でartifact一覧、size、healthが表示される。
- kind-ready snapshotでは、`spind snapshot inspect` でkind-ready metadataとkubeconfig templateの存在を確認できる。
- `spind snapshot prune --dry-run` で削除予定snapshotと削除予定sizeが表示される。
- `spind vm create from-prepared --snapshot prepared` が成功する。
- snapshotから作成されたVM instanceは、snapshot元VMとは別のVM directoryを持つ。
- `spind vm start from-prepared` はsaved state restoreで起動する。
- snapshot由来VMはexec readyになる。
- snapshot由来VMで `spind vm exec from-prepared -- cat /tmp/spind-e2e.txt` が `hello` を出力する。
- snapshot由来VMへ `spind vm exec from-prepared -- sh -lc 'echo changed > /tmp/spind-e2e.txt'` で書き込める。
- snapshot由来VMを停止できる。
- 同じsnapshotから `from-prepared-again` を作成できる。
- `from-prepared-again` はsnapshot作成時点のfile内容 `hello` を読める。
- snapshotから作成されたVM instanceへ書き込んでも、snapshot元VMとsnapshot artifactの内容は変わらない。
- snapshot由来VMはsnapshotのexec用SSH鍵で接続できる。
- `spind vm start` 開始から `spind vm exec true` 成功までのend-to-end時間を記録する。
- `spind snapshot delete prepared` でsnapshotを削除できる。
- snapshot削除後も、既に作成済みのsnapshot由来VM directoryは削除されない。

### `vm stop`

- 起動中VMを停止できる。
- 停止後の状態が記録される。
- 存在しないVM名では失敗する。
- 停止済みVMに対して破壊的な変更をしない。
- 停止後にbackend processが残らない。
- 停止後にDocker API relay、port publish relay、endpoint socket、host loopback listenerが残らない。
- Cloud Hypervisor backendでは、停止後に `passt` process、vhost-user socket、legacy tap device、`virtiofsd` process、virtio-fs socketが残らない。
- stale PIDやstale socketがある状態で `spind vm stop` を再実行すると、停止済み状態へ修復できる。

### `vm delete`

- 停止済みVMに対して `spind vm delete <name>` が成功する。
- 削除後、`~/.spind/vms/<name>/` が存在しない。
- `spind vm delete <name>` は削除した容量を表示する。
- 起動中VMに対する `spind vm delete <name>` は失敗し、VM directoryを残す。
- 起動中VMに対する `spind vm delete <name> --force` は、VMを停止してからVM directoryを削除する。
- `spind vm delete <name> --unmerge-kubeconfig` は、global kubeconfigから対象VMの `spind-<vm-name>` entryを削除してからVM directoryを削除する。
- `spind vm delete <name>` はimage storeとsnapshot artifactを削除しない。

### Docker Host

- `spind image build docker` または `task build-image-docker` が成功する。
- `spind vm create docker-host --image docker` が成功する。
- `spind vm start docker-host` 後にDocker endpointが表示される。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker ps` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm -v "$PWD:/work" busybox ls /work` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox echo hello` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker pull busybox:latest` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox echo hello world` が成功し、stdoutに `hello world` が返る。
- `printf 'hello stdin\n' | DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -i --rm busybox cat` が成功し、stdoutに `hello stdin` が返る。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm --user 1000:1000 busybox ping -c 1 -W 3 127.0.0.1` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox nslookup example.com` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox wget -qO- http://example.com` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -d --name spind-http -p 18080:8080 busybox sh -c 'mkdir -p /www && echo hello > /www/index.html && httpd -f -p 8080 -h /www'` が成功する。
- `curl http://127.0.0.1:18080/` が `hello` を含む応答を返す。
- `DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker rm -f spind-http` が成功する。
- Docker daemonが存在しないVMでも、Docker endpointなしで `spind vm start` は成功する。
- `spind vm start` 実行時のcurrent working directoryがVM内でも同じabsolute pathで見える。
- host path共有mountはguest mount helper経由で設定される。
- Virtualization.framework snapshot restore後は、`spind vm start` 実行時のcurrent working directoryでhost path共有mountが再設定される。
- Cloud Hypervisor snapshot restore後は、高速saved-state restoreを優先し、host path共有mountを必須確認にしない。
- Cloud Hypervisor snapshot由来Docker Hostの `spind vm list` は、host path共有mountを `unsupported` として表示する。
- `spind vm stop docker-host` 後、host Docker relay processとDocker endpoint socketが残らない。
- `spind vm stop docker-host` 後、host path共有mountが残らない。

### kind-ready snapshot

- host側kindでDocker Host上にkind clusterを作成できる。
- `spind snapshot create kind-ready --vm kind-base --k8s=kind --kubeconfig ~/.kube/config --context kind-dev` が成功する。
- 指定contextが存在しない場合は失敗する。
- snapshot作成前ready checkで `kubectl get nodes` 相当が成功する。
- nodeが `Ready` でない場合は失敗する。
- kind-ready snapshotから別VMを作成できる。
- `spind vm start kind-work` 後、`~/.spind/vms/kind-work/kubeconfig` が生成される。
- 生成kubeconfigのcontext、cluster、user名は `spind-kind-work` である。
- `KUBECONFIG=$HOME/.spind/vms/kind-work/kubeconfig kubectl get nodes` が成功する。
- `spind vm list kind-work` でKubernetes API server URL、context、kubeconfig pathを確認できる。
- 複数kind-ready VMを同時起動してもhost portとkubeconfig pathが衝突しない。
- `~/.kube/config` は自動変更されない。
- Cloud Hypervisor snapshot由来VMでは、host path共有mountなしでもKubernetes APIを利用できる。
- `spind vm stop kind-work` 後、Kubernetes API server用port relayとhost listenerが残らない。

### kubeconfig merge安全性

この確認はLinux/KVM上のCloud Hypervisor backendで実施する。

- macOS host上のglobal kubeconfigでは実施しない。
- Virtualization.framework backendではこの確認を必須にしない。
- 利用者の既存 `~/.kube/config` は直接対象にしない。
- 一時HOMEまたは一時 `KUBECONFIG` を作り、その中のkubeconfigだけを書き換える。
- Cloud Hypervisor backendでkind-ready VMを起動してから確認する。

確認手順:

- `task build` を実行する。
- Cloud Hypervisor backendのkind-ready VMを作成し、`spind vm start kind-work` で起動する。
- `TMP_HOME="$(mktemp -d)"` と `TMP_KUBECONFIG_DIR="$(mktemp -d)"` を用意する。
- `HOME="$TMP_HOME"` または `KUBECONFIG="$TMP_KUBECONFIG_DIR/config"` を指定して、以降のmerge/unmergeを実行する。
- 確認終了後、kind-ready VMを停止する。

- VM別kubeconfigが標準であり、`spind vm start kind-work` はglobal kubeconfigを変更しない。
- `spind kubeconfig merge kind-work --kubeconfig <path>` は明示pathへmergeする。
- 明示pathがない場合、`KUBECONFIG` 未設定では `~/.kube/config` をmerge先候補にする。
- `KUBECONFIG` が単一pathの場合、そのpathをmerge先候補にする。
- `KUBECONFIG` が複数pathの場合、空pathと重複pathを除外する。
- `KUBECONFIG` のpath選択では、absolute path化、clean、symlink解決、home展開を追加で行わない。
- `KUBECONFIG` が複数pathで既存fileを含む場合、最初の既存fileへmergeする。
- `KUBECONFIG` が複数pathで既存fileを含まない場合、最後のpathへmergeする。
- 既存の同名 `spind-<vm-name>` entryがある場合、`--replace` なしでは失敗する。
- `--replace` ありでは、同名 `spind-<vm-name>` entryだけを置き換える。
- mergeは既定でcurrent-contextを変更しない。
- `--set-current-context` ありでは、current-contextを `spind-<vm-name>` に変更する。
- merge対象fileにYAML commentがある場合、merge後のcomment保持は保証されない。
- merge対象fileにあるspind管理外entryの `namespace`、`proxy-url`、`tls-server-name`、`extensions` などの未知fieldは保持される。
- merge失敗時、元のkubeconfig file内容は保持される。
- `spind kubeconfig unmerge kind-work` は候補path全体から `spind-<vm-name>` entryだけを削除する。
- unmerge後もspind管理外entryの未知fieldは保持される。
- unmergeは他のcontextから参照されているentryを削除しない。

## Backend固有チェック

### Virtualization.framework

- macOS上で確認できる。
- Swift実行部がVirtualization.framework backend専用の薄い実行部として起動する。
- 通常起動、停止、saved state保存、saved state restoreがSwift実行部経由で成功する。
- execとDocker API relayは `VZVirtioSocketDevice` 経由で動作する。
- Docker Host networkingはVirtualization.frameworkのNAT network deviceで動作する。
- Docker Hostのhost path共有mountはVirtualization.frameworkのdirectory sharing機能で動作する。

### Cloud Hypervisor

- Linux/KVM環境で確認できる。
- `/dev/kvm` がない場合、e2eは明確に失敗する。
- `vm.unprivileged_userfaultfd=1` でない場合、snapshot restore e2eは明確に失敗する。
- `passt` がない場合、Docker Host e2eは明確に失敗する。
- `virtiofsd` がない場合、Docker Host e2eは明確に失敗する。
- `spind vm start` はCloud Hypervisor process、REST API socket、vsock socket、log pathを状態ファイルへ保存する。
- `spind vm exec` はCloud Hypervisor host側vsock socket protocolでguest SSH vsock portへ接続する。
- `snapshot create` はCloud Hypervisor REST APIでpauseとsnapshotを行う。
- snapshot由来VMの `spind vm start` は `cloud-hypervisor --restore` を使い、restore後にREST APIでresumeする。
- `spind vm stop` はCloud Hypervisor REST API socketを優先し、必要に応じてprocess signalへfallbackする。
- Docker Host networkingは `passt` のvhost-user backendで動作する。
- Docker Hostのhost path共有mountはvirtio-fsとhost側Rust版 `virtiofsd` で動作する。
- Docker HostのCloud Hypervisor起動では、`--memory size=<memory>M,shared=on`、`--net vhost_user=on,socket=<socket>,vhost_mode=client,mac=<mac>`、`--fs tag=spind-cwd,socket=<socket>,num_queues=1,queue_size=512,dax=off` を使う。
- `passt` process PID、socket path、log path、ready状態がVM状態ファイルへ保存される。
- `virtiofsd` は `--cache=never` と `--sandbox=namespace` で起動される。
- `virtiofsd` process PID、socket path、log path、shared host path、shared guest path、ready状態がVM状態ファイルへ保存される。
- Cloud Hypervisor snapshot由来Docker Hostでは、Docker API、container実行、Docker pull、Docker network、port publishを確認し、host path共有mountは確認対象外にする。
- e2e完了後、Cloud Hypervisor process、`passt` process、vhost-user socket、legacy tap device、`virtiofsd` processが残らない。

## 記録

実装者は、ランブック確認時に次を記録する。

- 対象backend。
- 実行したTaskfileタスク。
- 実行したgit commit hash。
- 実行環境。
- 成功または失敗の結果。
- `start` からexec readyまでの時間。
- snapshot restore startからexec readyまでの時間。
- 失敗時のコマンド出力とログ。

Virtualization.frameworkとCloud Hypervisorのどちらか一方だけを確認した状態では、このランブックは完了扱いにしない。
