# Docker Host

## 目的

spindは、VM内のDocker daemonをhostのDocker clientから利用できるDocker Hostとして扱える。

最初の段階では、snapshot restore最適化の前に、通常起動したVMでDocker APIをhostから使えることを確認する。

## 対象スコープ

- Docker API用guest proxyをベースイメージへ含める。
- Docker入り標準image `docker` を用意する。
- `spind vm start` 時にDocker API用host endpointをbest-effortで起動する。
- hostのDocker clientから `DOCKER_HOST=unix://...` でVM内Docker daemonを使う。
- hostのcurrent working directoryをVM内へ同じabsolute pathで共有mountする。
- Docker daemonとcontainerが外向きnetworkを使える。
- `docker run -p` で公開されたTCP portをhost loopbackから使える。
- Docker Host e2eをTaskfileから実行できる。
- host側kindから通常のDocker Hostとして使える。

## 対象外

- Docker daemonをspind CLIでinstallする機能。
- spind CLIによるDocker imageのpull/load管理。
- Docker Compose。
- container lifecycle管理をspind CLIで包むこと。
- remote TCP Docker APIの公開。
- Docker Host snapshotの最適化。
- spind CLIによるkind/Kubernetes lifecycle管理。
- 任意host pathの複数mount管理。

## Image

### `docker`

`docker` imageは、Cloud Hypervisor backendで使う標準Docker Host imageである。

`docker` imageはMicrovm.nixで生成する。

Docker Host用のbuiltin template原本は `templates/docker/` に置く。

`spind image build docker` は `template://docker` を使い、初回build時にbuiltin templateを `~/.spind/templates/docker` へ配置する。配置されたtemplateの `flake.nix` はdefault packageだけを公開し、spindは `nix build` の結果に含まれる `metadata.json` を正としてimage storeへ配置する。

Nix build結果の `metadata.json` にはwritable diskの作成仕様も含める。spindはその仕様に従って `docker-data.img` を作成する。

`docker` imageには、次を含める。

- NixOS guest kernel。
- initramfs。
- read-only `/nix/store` 用の `nix-store.img`。
- Docker data用の writable `docker-data.img`。
- Docker daemon。
- OpenSSH server。
- exec用ユーザー `spind`。
- SSH over vsock用guest proxy。
- Docker API用guest proxy。
- Docker port publish relay用guest TCP forward proxy。
- host path共有mount用guest mount helper。

公開鍵は、VM作成時に `metadata.json` で指定されたkernel parameterへbase64で追加し、guest起動時に `/home/spind/.ssh/authorized_keys` へ反映する。

`docker` imageは、標準の `kind create cluster` が期待するDocker Host互換性を満たす。利用者はkind設定を変更せず、host側で次のように実行できる。

```sh
export DOCKER_HOST=unix://$HOME/.spind/vms/kind-base/docker.sock
kind create cluster --name dev
```

このため、Docker Host guestの `/lib/modules` は、`modules.dep` などのmetadataとkind node起動時に使われる実体fileが一致していなければならない。現時点では `kindest/node` 内のsystemd起動で失敗が観測された `autofs4`、`configfs`、`dm_mod` を実体fileとして含める。`binfmt_misc`、`loop`、`fuse` はこの検証ではkind成功にもsystemd unit正常化にも追加必須とは判定していない。

例:

```sh
spind image build docker
spind vm create docker-host --image docker
```

## Guest Proxy

Docker API用guest proxyは、VM内でvsock port `10240` をlistenする。

```text
guest vsock:10240
  -> /var/run/docker.sock
```

- vsock port `10240` はDocker API用の標準値である。
- TCP port `10240` は使わない。
- guest proxyはDocker protocolを解釈しない。
- guest proxyはDocker protocolを解釈せず、spind relay frameをDocker daemonのbyte streamへ戻す。
- Docker API用guest proxyは、SSH exec用guest proxyとは別processである。
- guest proxyはOpenRC default runlevelで起動する。

複数VMが同じhost上に存在しても、guest側vsock port `10240` は重複してよい。

VMごとにguest CIDまたはbackend固有接続情報が異なるため、host側relayが対象VMを区別する。

## Host Endpoint

host側Docker endpointはUnix domain socketを使う。

標準path:

```text
~/.spind/vms/<name>/docker.sock
```

Docker clientからは次の形で使う。

```sh
DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker ps
```

host local endpointとguest proxyの間はbackend別のvsock transportで中継する。

```text
Docker CLI
  -> ~/.spind/vms/<name>/docker.sock
  -> spind host docker relay
  -> VZVirtioSocketDevice or Cloud Hypervisor host-side vsock socket
  -> guest vsock:10240
  -> /var/run/docker.sock
  -> dockerd
```

- host relayはDocker protocolを解釈しない。
- host relayはDocker protocolを解釈せず、Docker clientのbyte streamをspind relay frameへ変換する。
- host relayとguest proxyは、Docker APIのHTTP connection upgrade、hijack、attach streamを透過的に中継する。
- `docker run`、`docker attach`、`docker logs -f` のstdout/stderr/stdin streamはhost Docker clientへ返る。
- relayはHTTP request/responseを独自に生成せず、Docker daemonから返るupgrade responseと以後のstreamをそのまま通す。
- relayはstdin終了やcontainer終了時のhalf-closeを正しく扱い、片方向closeだけで反対方向のstreamを早期終了しない。
- backendのvsock transportがhalf-closeを期待どおりに伝えない場合でも、host relayとguest proxyはspind relay frameでwrite側closeを明示的に伝える。
- host relayはVMごとに1つのUnix socket pathをlistenする。
- Unix socket pathはVMごとに異なるため、host側port衝突は起きない。
- TCP endpointは初期スコープ外である。

officialな確認コマンドは、shell aliasやfunctionに依存しない形で示す。

利用者環境でDocker CLIに `--detach-keys` が自動挿入される場合でも、relayはDocker API streamを透過するため、attached runのstdout/stderr/stdinを壊してはならない。

## Host Path Sharing

Docker clientはhost上で実行されるが、Docker daemonはVM内で実行される。

そのため、次のようなhost path bind mountを使うには、同じpathがVM内にも存在する必要がある。

```sh
docker run --rm -v "$PWD:/work" busybox ls /work
```

spindはDocker Host用に、`spind vm start` を実行したhost current working directoryをVM内へ同じabsolute pathで共有mountする。

例:

```text
host: /Users/suin/project
guest: /Users/suin/project
container: -v /Users/suin/project:/work
```

- 共有対象は初期スコープでは `spind vm start` 実行時のcurrent working directory 1つだけである。
- VM内mount pathはhostのabsolute pathと同じにする。
- 共有mountはread/write可能にする。
- host pathが存在しない場合、共有mountは作成しない。
- host pathへアクセスできない場合、共有mountは作成しない。
- 共有mountに失敗しても、VM起動とexec readyが成功していれば `spind vm start` は成功する。
- 共有mount失敗時はwarningと関連log pathを表示する。
- snapshot artifactには共有mountの内容を含めない。
- Virtualization.framework backendのsnapshot由来VMでは、restore時ではなく `spind vm start` 実行時のcurrent working directoryを共有mountする。
- Cloud Hypervisor backendのsnapshot由来VMでは、高速saved-state restoreを優先し、host path共有mountを非対応にする。
- `spind vm stop` は共有mountを解除する。

backend別の方式:

- Virtualization.framework backendでは、Virtualization.frameworkのdirectory sharing機能を使う。
- Cloud Hypervisor backendでは、Docker Host用guest NICに `passt` のvhost-user backendを使う。
- Cloud Hypervisor backendでは、host側に `passt` binaryが必要である。
- Cloud Hypervisor backendでは、virtio-fsを使う。
- Cloud Hypervisor backendでは、host側にRust版 `virtiofsd` binaryが必要である。
- Cloud Hypervisor backendでは、VMごとに `virtiofsd` processと `cloud-hypervisor-virtiofs.sock` を起動し、Cloud Hypervisorへ `--fs tag=spind-cwd,socket=<socket>,num_queues=1,queue_size=512,dax=off` を渡す。
- Cloud Hypervisor backendでvirtio-fsを利用できない場合、共有mountはunavailableとして扱い、VM起動自体は成功させる。
- Linux/KVM環境では、`passt` とvirtio-fsを利用できない状態はDocker Host e2e失敗として扱う。
- Cloud Hypervisor backendのvirtio-fs共有mountは通常起動VMだけを対象にする。

VM内では、必要に応じて親directoryを作成してからmountする。

host pathと同じabsolute pathをVM内に作れない場合、共有mountは失敗扱いにする。

### Guest Mount Helper

host path共有mountはguest内root権限を必要とする。

`docker` imageには、host path共有mount用のguest mount helperを含める。

guest mount helperは、VM内で標準vsock port `10242` をlistenする。

guest mount helperは、次の最小操作だけを受け付ける。

- mount対象parent directoryの作成。
- backendから見える共有deviceを、指定されたguest absolute pathへmountする。
- 指定されたguest absolute pathのunmount。
- mount状態確認。

guest mount helperは任意command実行機能を持たない。

guest mount helperが受け付けるmount pathは、`spind vm start` が指定したhost current working directoryと同じabsolute pathに限定する。

guest mount helperはpathを正規化し、次を拒否する。

- relative path。
- 空path。
- `/`。
- `..` を含むpath。
- 既存mount pointの外側を指すsymlink経由のpath。

host側は、共有mountに必要なbackend deviceをVMへ接続してからguest mount helperへmount要求を送る。

Virtualization.framework backendのsnapshot restore後は、restore完了とexec ready後に、`spind vm start` 実行時のcurrent working directoryを使って同じ手順で共有mountを再設定する。

Cloud Hypervisor backendのsnapshot restore後は、host path共有mountを再設定しない。

共有mountの状態はVM状態ファイルへ保存するが、snapshot artifactへは保存しない。

`spind vm stop` は、guest mount helperへunmount要求を送り、その後にhost側共有deviceまたは `virtiofsd` processを停止する。

guest mount helperへ接続できない場合、共有mountはunavailableとして扱い、VM起動とexec readyが成功していれば `spind vm start` は成功する。

## Docker Host Support Matrix

Docker Hostの能力はbackendとVM起動方式によって異なる。

| capability | Virtualization.framework normal | Virtualization.framework snapshot | Cloud Hypervisor normal | Cloud Hypervisor snapshot |
| --- | --- | --- | --- | --- |
| Docker API endpoint | supported | supported | supported | supported |
| attached `docker run` stream | supported | supported | supported | supported |
| `docker pull` | supported | supported | supported | supported |
| container外向き通信 | supported | supported | supported | supported |
| host loopbackへのport publish | supported | supported | supported | supported |
| host path共有mount | supported | supported | supported | unsupported |

`unsupported` は仕様上の非対応であり、`spind vm start` の失敗理由にしない。

`unavailable` は対応対象だが、環境不足や起動失敗で使えない状態である。

Cloud Hypervisor snapshot由来VMでhost path共有mountが必要な場合、利用者はsaved-state restoreではなく通常起動VMを使う。

## `spind vm start`

`spind vm start <name>` は、VM起動とexec readyを必須条件にする。

Docker endpointは追加能力としてbest-effortで起動する。

`spind vm start <name>` の流れ:

1. VMを起動またはrestoreする。
2. exec readyを待つ。
3. current working directoryの共有mountをbest-effortで設定する。
4. Docker API用guest proxyが到達可能か確認する。
5. 到達可能な場合、host Docker relayを起動し、`~/.spind/vms/<name>/docker.sock` をlistenする。
6. Docker API `_ping` が成功する場合、Docker endpointをreadyにする。
7. Docker APIが使えない場合でも、VM start自体は成功にする。

Docker daemonがVM内にない場合:

```text
started VM "base"
docker: unavailable
```

Docker daemonがVM内にあり、Docker API readyの場合:

```text
started VM "docker-host"
docker: unix:///Users/suin/.spind/vms/docker-host/docker.sock
```

Docker relay起動やDocker API readinessに失敗しても、VM起動とexec readyが成功していれば `spind vm start` は成功する。

ただし、CLIはwarningと関連log pathを表示する。

共有mountに成功した場合、CLIは共有pathを表示する。

```text
mount: /Users/suin/project
```

Docker Host networkingの詳細は `docs/design/docker-host-networking.md` に従う。

Docker networkやport publish relayに失敗しても、VM起動とexec readyが成功していれば `spind vm start` は成功する。

ただし、`docker` imageではDocker network readyを期待するため、失敗時はwarningと関連log pathを表示する。

## `spind vm list`

`spind vm list <name>` はDocker endpoint状態を表示する。

表示項目:

- Docker endpoint availability。
- Docker endpoint URI。
- Docker relay PID。
- Docker guest vsock port。
- Docker API ready状態。
- Docker network ready状態。
- Docker port publish relay ready状態。
- published port一覧。
- shared path。
- shared path ready状態。
- shared path unavailable reason。
- shared path support状態。
- 各能力のlog path。

Docker Host能力の状態は、support状態とready状態を分ける。

- `support`: `supported` または `unsupported`。
- `ready`: `ready`、`unavailable`、または `not-applicable`。
- `reason`: `unsupported` または `unavailable` の理由。
- `log`: 調査に使うlog path。

`unsupported` の場合、`ready` は `not-applicable` とする。

`spind vm list --json` では、Docker Host能力を次の形で表す。

```json
{
  "docker": {
    "api": {
      "support": "supported",
      "ready": "ready",
      "endpoint": "unix:///Users/suin/.spind/vms/docker-host/docker.sock"
    },
    "sharedPath": {
      "support": "unsupported",
      "ready": "not-applicable",
      "reason": "cloud-hypervisor snapshot restore prioritizes saved-state restore speed"
    }
  }
}
```

例:

```text
docker: unavailable
```

```text
docker: unix:///Users/suin/.spind/vms/docker-host/docker.sock
```

Cloud Hypervisor snapshot由来VMでは、host path共有mountをunsupportedとして表示する。

```text
shared path: unsupported
shared path reason: cloud-hypervisor snapshot restore prioritizes saved-state restore speed
```

対応対象だが利用できない場合はunavailableとして表示する。

```text
shared path: unavailable
shared path reason: virtiofsd not found
log: /Users/suin/.spind/vms/docker-host/virtiofsd.log
```

## `spind vm stop`

`spind vm stop <name>` は、VM processと一緒にhost Docker relayを停止する。

- `~/.spind/vms/<name>/docker.sock` は削除する。
- stale relay processがある場合、停止または状態修復する。
- host path共有mountを解除する。
- host Docker port publish relayを停止する。
- host loopback listenerを閉じる。
- Docker daemon内のcontainer停止はdockerdとguest shutdownの責務であり、spindはcontainerを個別管理しない。

## E2E

Docker Host e2eは通常のminimum e2eとは分ける。

Taskfile task:

```sh
task e2e
```

確認手順:

```sh
spind image build docker
spind vm create docker-host --image docker
spind vm start docker-host
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker ps
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm -v "$PWD:/work" busybox ls /work
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox echo hello
printf 'hello stdin\n' | DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -i --rm busybox cat
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker pull busybox:latest
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox nslookup example.com
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox wget -qO- http://example.com
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -d --name spind-http -p 18080:8080 busybox sh -c 'mkdir -p /www && echo hello > /www/index.html && httpd -f -p 8080 -h /www'
curl http://127.0.0.1:18080/
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker rm -f spind-http
spind vm stop docker-host
```

Docker Host e2eは、hostにDocker clientがあることを前提にする。

Docker daemonはhost側ではなくVM内のdockerdを使う。

## 受け入れ基準

- `docker` imageにはDocker daemonとDocker API用guest proxyが含まれる。
- `docker` で作成したVMは、`spind vm start` 後にhost Docker endpointを表示する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker ps` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker run --rm -v "$PWD:/work" busybox ls /work` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker run --rm busybox echo hello` が成功する。
- `DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker run --rm busybox echo hello world` が成功し、stdoutに `hello world` が返る。
- `printf 'hello stdin\n' | DOCKER_HOST=unix://$HOME/.spind/vms/<name>/docker.sock docker run -i --rm busybox cat` が成功し、stdoutに `hello stdin` が返る。
- Docker APIのHTTP connection upgrade、hijack、attach streamが透過的に中継される。
- Docker host relay、guest proxyはいずれもDocker protocolを解釈しない。
- `spind vm start` 実行時のcurrent working directoryがVM内で同じabsolute pathとして見える。
- host path共有mountはguest mount helper経由でroot権限のmount/unmountを行える。
- shared pathはsnapshot artifactに含まれない。
- `spind vm stop` 後、host Docker relay processとDocker endpoint socketが残らない。
- `spind vm stop` 後、host path共有mountが残らない。
- Docker Host e2eはTaskfileから実行できる。
