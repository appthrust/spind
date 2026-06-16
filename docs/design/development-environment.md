# 開発環境

## 要件

- 開発環境は `devbox` を用いる。
- 実装、テスト、生成、検証、ローカル実行の手順は `devbox` 上で動くことを前提にする。
- 下流エージェントは、`devbox` を経由しない固有の開発環境手順を標準手順として追加してはならない。
- 開発用タスクランナーはTaskfileを用いる。
- Docker Host用の標準imageは、Microvm.nixで生成する `docker` とする。
- Docker Host用のbuiltin template原本は `templates/docker/` に置く。リポジトリルートの `flake.nix` には置かない。
- `spind image build docker` は、`template://docker` を用いて `templates/docker/flake.nix` 由来のdefault packageをbuildし、`kernel`、`initramfs`、read-only `nix-store.img`、writable `docker-data.img`、`metadata.json` を `~/.spind/images/docker` へ生成する。
- `template://docker` は、初回build時にbuiltin templateから `~/.spind/templates/docker` へ配置する。
- writable diskの作成仕様はNix build結果の `metadata.json` に含める。spind固有の `template.json` は使わない。
- `task build-image-docker` は `spind image build docker --force` を呼び出す。
- `docker` imageは、Microvm.nixの標準的なtmpfs root、read-only `/nix/store` disk、writable volume diskの構成に従う。
- `docker` imageの公開鍵注入はroot filesystemへのhost側書き込みではなく、kernel command lineへ `spind.ssh_authorized_key=<base64>` を追加してguest起動時に反映する。
- `disk.img` の第1partitionへ `debugfs` で公開鍵を注入する方式は、legacy single-disk image向けの互換経路とする。
- exec用ユーザー名は `spind` であり、標準shellは `/bin/sh` である。
- exec用ユーザーはpassword loginを使わず、VM作成時に注入される公開鍵で認証する。
- Docker API用guest proxyは、vsock port `10240` とguest `/var/run/docker.sock` を中継する。
- host path共有mount用guest mount helperは、vsock port `10242` でmount、unmount、状態確認だけを受け付ける。
- `docker` imageにはDocker daemon、Docker API用guest proxy、host path共有mount用guest mount helper、Docker port publish relay用guest TCP forward proxyを含める。
- guest側toolは単一のLinux binary `spind-guest-agent` としてbuildし、systemd serviceごとに `ssh-proxy`、`docker-proxy`、`tcp-forward-proxy`、`mount-helper` subcommandを別processで起動する。
- `docker` imageにはDocker default bridge、container外向き通信、`docker pull`、kind node起動に必要なkernel module、networkd設定、DNS設定を含める。
- initramfsやoverlayによってSSH環境やproxyをVM起動時に配置する方式は、初期スコープ外である。
- `.envrc` は `devbox generate direnv` で作成する。
- `.envrc` はGitで管理する。
- Cloud Hypervisorの実VM動作確認は、macOSではなくLinux/KVM環境で行う。
- Cloud Hypervisor向けビルド、Docker imageビルド、e2eはLinux/KVM環境上で実行する。
- Cloud Hypervisor検証環境には、Docker Hostのguest NICに使う `passt` と、host path共有mountに使うRust版 `virtiofsd` が必要である。

## 受け入れ基準

- 開発者向け手順が追加または更新される場合、主要なコマンドは `devbox` を用いる形で示されている。
- 実装や設定が開発環境に依存する場合、`devbox` 上で再現できる。
- `devbox` 以外のツールを補助的に使う場合でも、標準の開発環境は `devbox` のままである。
- 開発者向けの主要な操作はTaskfileから実行できる。
- Docker Host用の `docker` imageを `spind image build docker` とTaskfileからビルドできる。
- VM作成時の公開鍵注入に必要なhost側toolは、`devbox` 環境またはLinux/KVM検証環境で利用できる。
- `docker` image metadataには、Microvm.nix由来であること、kernel command line、CPU数、memory量、disk list、公開鍵を受け取るkernel parameter名が含まれる。
- `docker` の writable `docker-data.img` にはDocker daemonの永続dataを保存できる。
- `docker` imageにはDocker networkingとport publish relayに必要なguest側toolが含まれる。
- Docker API relayは、backendのvsock half-close挙動に依存せず、Docker APIのupgrade、hijack、attach、exec streamのwrite側closeを明示的に中継する。
- Cloud Hypervisor backendで起動した `docker` VMをDocker Hostとして使い、host側の通常の `kind create cluster --name dev` がkind設定変更なしで成功する。
- ベースイメージで起動したVM内では、`sh`、`ls`、`pwd`、`cat`、`echo` が実行できる。
- ベースイメージで起動したVM内では、`spind` ユーザーで `ping` を実行できる。
- `.envrc` がリポジトリに存在し、devboxのdirenv連携を有効にしている。
- Cloud Hypervisor向けe2eはLinux/KVM環境で実行する前提になっている。
- Cloud Hypervisor向けの成果物は、e2eを実行するLinux/KVM環境上でビルドされる。
- Cloud Hypervisor検証環境では `passt` と `virtiofsd` を `PATH` から実行できる。
