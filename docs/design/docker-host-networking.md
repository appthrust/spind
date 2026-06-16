# Docker Host Networking

## 目的

spindのDocker Hostは、host Docker clientからDocker APIを使えるだけでなく、開発用Docker環境として `docker pull`、container外向き通信、TCP port publishを使える状態にする。

Docker API relay、exec、container networkは別の通信経路として扱う。

```text
host Docker client
  -> Docker API relay over vsock
  -> guest dockerd

guest dockerd / container
  -> guest NIC
  -> backend NAT or user-mode NAT
  -> external network

host 127.0.0.1:<published-port>
  -> spind port publish relay
  -> guest TCP forward proxy over vsock
  -> guest 127.0.0.1:<published-port>
  -> container
```

## 対象スコープ

- `docker` VMに外向き通信可能なguest NICを提供する。
- guest VM内でDNS、default route、Docker default bridgeを使えるようにする。
- Docker daemonがregistryへ接続し、`docker pull` できるようにする。
- container内からDNSとHTTP/HTTPSの外向き通信を使えるようにする。
- `docker run -p <host-port>:<container-port>` で公開されたTCP portをhost loopbackから利用できるようにする。
- snapshot由来VMの `spind vm start` でもDocker networkとport publish relayを再設定する。

## 対象外

- hostのpublic interfaceへcontainer portを直接公開すること。
- UDP port publish。
- IPv6-only構成。
- 複数network profileのユーザー選択。
- spind CLIによるDocker imageのpull/load管理。
- Docker Compose専用のnetwork制御。
- VPN、proxy、企業network向けの特別設定。

## Guest Network

`docker` VMは、Docker Host用にguest NICを持つ。

guest NICは、Docker API relayやexecの必須経路ではない。Docker API relayとexecはvsockを使い続ける。

guest NICは、次の用途に使う。

- Docker daemonのregistry接続。
- containerの外向き通信。
- guest内DNS解決。

guest VM内では、次をready条件として扱う。

- loopback以外のnetwork interfaceが存在する。
- default routeが存在する。
- `/etc/resolv.conf` が存在し、利用可能なnameserverを持つ。
- `docker pull` が外部registryへ到達できる。

Virtualization.framework backendでは、Virtualization.frameworkのNAT network deviceを使う。

Cloud Hypervisor backendでは、Linux/KVM host上で `passt` のvhost-user backendを使う。spindはVMごとに `passt` processを起動し、Cloud Hypervisorのguest NICをvhost-user socketへ接続する。

Cloud Hypervisor backendでは、標準経路としてhost tap device、host IP forwarding、host iptables/nftables NATを変更しない。

Cloud Hypervisor backendの通常起動で `passt` を設定できない場合でも、VM起動とexec readyが成功していれば `spind vm start` は成功にする。ただし、Docker networkはunavailableとして表示し、warningとlog pathを出す。

Cloud Hypervisor backendのsnapshot restoreでは、saved-state内のvhost-user-net deviceがbackend socketを必要とする。restore前に `passt` を起動できない場合、`spind vm start` は明確なエラーで失敗する。

明示的なlegacy configがある場合だけ、Cloud Hypervisor backendはtap deviceとhost NATを使う。

## Docker Daemon Network

`docker` imageのDocker daemonは、Dockerのdefault bridge networkを使う。

`docker` imageでは、次のようなDocker networkを無効化する設定を標準設定にしない。

```json
{
  "iptables": false,
  "ip-forward": false,
  "bridge": "none"
}
```

guest VM内では、次を設定する。

- `net.ipv4.ip_forward=1`
- Docker default bridge `docker0`
- container用veth
- Dockerが必要とするiptablesまたはnftables rule

Docker daemonが必要とするkernel module、iptables/nftables userspace、iproute2相当のtoolは `docker` imageに含める。

Docker Hostでは、Docker container内のroot以外のユーザーでも疎通確認できる状態にする。

Docker daemonがnetwork初期化に失敗しても、VM起動とexec readyが成功していれば `spind vm start` は成功にする。ただし、Docker networkはunavailableとして表示する。

## DNS

guest VMの `/etc/resolv.conf` は、backendのNAT network、`passt` がDHCPで配るDNS server、またはhost環境から得たDNS serverを指す。

container内DNSはDocker daemonの標準DNS機構に任せる。

e2eでは、container内から名前解決できることを確認する。

## Port Publish

Docker containerのport publishは、host loopbackへだけ公開する。

利用者が次を実行した場合:

```sh
docker run -p 8080:80 ...
```

spindはhost側で次をlistenする。

```text
127.0.0.1:8080
```

host public interface、`0.0.0.0`、外部hostからの接続は初期スコープ外である。

Docker publish指定にhost IPが含まれない場合、spindはhost loopback公開として扱う。

Docker publish指定に `0.0.0.0` が含まれる場合も、初期スコープではhost loopback公開に制限する。

同じhost portがすでに使われている場合、該当portのrelay起動は失敗扱いにする。VM起動自体は成功にし、`spind vm list` とwarningでport publish relayがunavailableであることを示す。

## Port Publish Relay

spindはDocker API relayとは別に、port publish relayを持つ。

port publish relayはVM起動中だけ動作し、次を行う。

1. Docker APIをclientとして監視し、公開portを持つcontainerを検出する。
2. host `127.0.0.1:<host-port>` にTCP listenerを作る。
3. 接続ごとにguest TCP forward proxyへvsockで接続する。
4. guest側の `127.0.0.1:<host-port>` へTCP接続を作る。
5. byte streamを双方向に中継する。

Docker API relay自体は、引き続きDocker protocolを解釈しないbyte stream relayである。port publish relayのDocker API利用は、port publish検出のための別clientとして扱う。

guest TCP forward proxyは、Docker API用guest proxyとは別processである。

guest TCP forward proxyは、VM内で標準vsock port `10241` をlistenする。

host relayからguest TCP forward proxyへの接続要求は、最小の行指向protocolを使う。

```text
CONNECT 127.0.0.1 <port>\n
```

- `<port>` はguest内TCP portである。
- 初期スコープでは接続先hostは `127.0.0.1` のみ受け付ける。
- guest proxyはDocker protocolを解釈しない。
- 接続確立後はbyte streamを双方向に中継する。

## `spind vm start`

`spind vm start <name>` は、Docker Host VMで次をbest-effortに設定する。

1. guest NIC。
2. guest DNSとdefault route。
3. Docker default bridge。
4. Docker API host endpoint。
5. host path共有mount。
6. port publish watcher。
7. 既存containerのpublished port relay。

VM起動とexec readyは必須条件である。

Docker API、Docker network、port publish relayは追加能力である。これらの一部が失敗しても、VM起動とexec readyが成功していれば `spind vm start` は成功する。

ただし、`docker` imageではDocker API、Docker network、port publish relayがreadyになることを期待する。失敗時はwarningとlog pathを表示する。

## `spind vm list`

`spind vm list <name>` はDocker Host networkingについて次を表示する。

- Docker API endpoint URI。
- Docker API ready状態。
- guest network ready状態。
- guest IP address。
- network backend名。
- network backend PID、socket path、log path。
- default route ready状態。
- DNS ready状態。
- Docker bridge ready状態。
- Docker pull ready状態。
- published port一覧。
- port publish relay ready状態。
- host path共有mount ready状態。

例:

```text
docker: unix:///Users/suin/.spind/vms/docker-host/docker.sock
docker network: ready
docker ports: 127.0.0.1:8080->80/tcp
```

```text
docker: unix:///Users/suin/.spind/vms/docker-host/docker.sock
docker network: unavailable
warning: guest default route is missing
```

## Snapshot Restore

snapshot artifactには、host側network backend process、vhost-user socket、host listener、legacy tap device、legacy NAT rule、Docker API endpoint socket、port publish relay processを含めない。

snapshot由来VMの `spind vm start` は、restore後に次を再設定する。

- guest NICの `passt` backend接続。
- guest DNSとdefault route。
- Docker API host endpoint。
- host path共有mount。
- port publish watcher。
- restore時点で存在するpublished portのhost relay。

restore後にDocker daemonまたはcontainerが古いnetwork stateを持っている場合、spindはnetwork readinessを再確認する。readyにならない場合、VM起動とexec readyが成功していれば `spind vm start` は成功にし、Docker networkをunavailableとして表示する。

## E2E

Docker Host networking e2eは、Docker Host e2eに含める。

確認手順:

```sh
spind image build docker
spind vm create docker-host --image docker
spind vm start docker-host
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker pull busybox:latest
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox echo hello world
printf 'hello stdin\n' | DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -i --rm busybox cat
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox nslookup example.com
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run --rm busybox wget -qO- http://example.com
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker run -d --name spind-http -p 18080:8080 busybox sh -c 'mkdir -p /www && echo hello > /www/index.html && httpd -f -p 8080 -h /www'
curl http://127.0.0.1:18080/
DOCKER_HOST=unix://$HOME/.spind/vms/docker-host/docker.sock docker rm -f spind-http
spind vm stop docker-host
```

snapshot由来VMでも、同じDocker Host networking e2eを実行する。

## 受け入れ基準

- `docker` VMでguest NIC、default route、DNSがreadyになる。
- host Docker clientからVM内dockerdに対して `docker pull busybox:latest` が成功する。
- `docker run --rm busybox echo hello world` が成功し、attached stdoutとして `hello world` がhostへ返る。
- `docker run -i --rm busybox cat` が成功し、stdinとattached stdoutがhost Docker clientとcontainer間で中継される。
- root以外のcontainer userで `ping -c 1 -W 3 127.0.0.1` が成功する。
- container内からDNS解決できる。
- container内から外向きHTTPが成功する。
- `docker run -p 18080:8080 ...` でhost `127.0.0.1:18080` からcontainerへ接続できる。
- `docker run -p` はhost public interfaceへ公開しない。
- Docker API relayはDocker protocolを解釈しないbyte stream relayのままである。
- port publish relayはDocker API relayとは別の経路で動作する。
- snapshot由来VMでもDocker API、Docker network、port publish relayを再設定できる。
- Docker Host networking e2eがVirtualization.framework backendとCloud Hypervisor backendで通る。
