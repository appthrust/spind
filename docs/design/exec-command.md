# Exec Command

## 目的

`spind vm exec` は、起動中VM内で指定されたコマンドを実行し、ホスト側へ標準出力、標準エラー、終了コードを返す。

## 対象スコープ

- 起動中VMに対する非対話コマンド実行。
- 標準出力と標準エラーのホスト側への転送。
- VM内コマンドの終了コードの反映。

## 対象外

- ファイル転送。
- 長時間常駐プロセスの管理。
- 停止中VMの自動起動。
- 複数VMへの同時実行。
- 対話TTYの実装。ただし、protocolは将来の対話TTY対応を妨げない。

## CLI

### `spind vm exec <vm-name> -- <command> [args...]`

- `<vm-name>` は既存VM名である。
- `--` 以降をVM内で実行するコマンドと引数として扱う。
- CLI frameworkは `cobra` を使うが、`--` 以降のVM内コマンド列はspind側optionとして解釈しない。
- `--` 以降に `-` で始まる値があっても、VM内コマンドの引数として保持する。
- `<vm-name>` のVMが存在しない場合、失敗する。
- VMが起動中でない場合、失敗する。
- VM内コマンドが見つからない場合、VM内の実行結果として失敗する。

例:

```sh
spind vm exec base -- uname -a
spind vm exec base -- sh -lc 'echo hello'
```

## 接続方式

- `spind vm exec` のホストVM間通信は、SSH over vsockを使う。
- Virtualization.framework backendでは、hostとVMの間の低level transportに `VZVirtioSocketDevice` を使う。
- Cloud Hypervisor backendでは、Cloud Hypervisorのhost側vsock socketを使う。
- ホスト側Go CLIは、起動中VMの状態ファイルから接続情報を読み取る。
- ホスト側Go CLIは、backendが提供するvsock接続経路を使ってguest側SSH endpointへ接続する。
- backend側のrelayは、SSH byte streamを中継するだけで、SSH payloadやコマンド内容を解釈しない。
- SSH protocolが、コマンド、標準入力、標準出力、標準エラー、終了コード、TTY拡張の意味を扱う。
- guest network/TCP、host側TCP port forwarding、独自yamux exec protocolは初期スコープ外である。

## Guest SSH

- ベースイメージの `disk.img` 内にはOpenSSH serverが含まれる。
- OpenSSH serverは、guest OS上でVM起動時に常駐起動する。
- SSHの認証には、spindがVMごとに生成するVM用鍵を使う。
- exec用ユーザー名は `spind` である。
- VM内には、VM用公開鍵が `/home/spind/.ssh/authorized_keys` として配置される。
- `spind` ユーザーはpassword loginを使わない。
- `spind` ユーザーの標準shellは `/bin/sh` である。
- 初期スコープでは、`spind` ユーザーにpasswordless sudoを付与しない。
- SSH endpointは、guest networkではなくvsock経由で到達できる。
- OpenSSH server自体がvsockをlistenできない場合、VM内にはvsockとguest localhostのSSH portを中継する薄いproxyを置く。
- proxyはSSH byte streamを中継するだけで、SSH payloadやコマンド内容を解釈しない。
- guest側SSH endpointとproxyは、guest OS上で動作する。

## SSH鍵

- `create` はVM用SSH鍵を生成する。
- VM用秘密鍵は `~/.spind/vms/<name>/ssh_key` に保存する。
- VM用公開鍵は `~/.spind/vms/<name>/ssh_key.pub` に保存する。
- 秘密鍵のfile modeは `0600` である。
- 公開鍵はVM作成時にVMのwritable diskへ注入される。
- 1つのVM用鍵を別VMへ共有しない。
- image storeにユーザー固有の秘密鍵を含めない。
- 初期スコープではSSH host keyの永続的なknown_hosts管理は行わない。
- host key確認は、local hostから対象VMのvsock endpointへ接続する前提に限定して省略する。
- guest network/TCPでSSH接続する設計へ拡張する場合は、host key検証方式を別途設計する。

## Vsock Relay

- Virtualization.framework backendでは、`start` が起動するSwift実行部がローカルUnix socketをlistenする。
- Virtualization.framework backendでは、Go CLIはSwift実行部のUnix socketへSSH clientとして接続する。
- Virtualization.framework backendでは、Swift実行部がUnix socketのbyte streamを `VZVirtioSocketDevice` 経由でguest側vsock endpointへ中継する。
- Cloud Hypervisor backendでは、Go CLIまたはGo側relayがCloud Hypervisorのhost側vsock socketへ接続する。
- Cloud Hypervisor backendでは、Cloud Hypervisorのhost側vsock socketを使ってguest側vsock endpointへSSH byte streamを中継する。
- Cloud Hypervisor backendでは、host側vsock socketへ接続後、guest側SSH vsock portを指定するCloud Hypervisorの接続手順に従い、接続確立後のbyte streamをSSH clientへ渡す。
- guest側では、OpenSSH serverがvsockを直接listenできる場合はそれを使う。
- OpenSSH serverがvsockを直接listenできない場合、guest側proxyがvsock endpointをlistenし、guest localhostのSSH portへ中継する。
- guest側proxyのvsock portは `22` ではなく、metadataまたはVM状態に明示された専用値を使う。
- 初期値は `10222` とする。
- Swift実行部、Go側relay、guest側proxyはいずれもSSH payloadを解釈しない。

## Protocol

- transportはbackend別のvsock接続経路である。
- Virtualization.framework backendでは `VZVirtioSocketDevice` によるvirtio socketを使う。
- Cloud Hypervisor backendではCloud Hypervisorのhost側vsock socketを使う。
- session protocolはSSHである。
- 最小実装では、SSHのexec requestで非対話execを実行する。
- 標準入力、標準出力、標準エラー、終了コードはSSH protocolの標準機能として扱う。
- 対話TTYは初期実装では提供しないが、SSHのPTY requestへ拡張できる形にする。
- `spind vm exec` は、独自のexec wire protocolを定義しない。

## 入出力

- ホスト側の標準入力は、VM内コマンドの標準入力へ渡す。
- VM内コマンドの標準出力は、ホスト側の標準出力へ渡す。
- VM内コマンドの標準エラーは、ホスト側の標準エラーへ渡す。
- `spind vm exec` の終了コードは、VM内コマンドの終了コードと同じにする。
- VMに接続できない等、spind自体の失敗はVM内コマンドの終了コードとは区別する。

## 状態管理

- `spind vm exec` はVMの永続データを直接変更しない。
- `spind vm exec` はVM状態ファイルを読み取るが、通常成功時に起動状態を書き換えない。
- VM状態ファイルには、host側Unix socket pathまたはCloud Hypervisor host側vsock socket path、guest側vsock port、exec用ユーザー名が含まれる。
- VM用秘密鍵pathは、VMディレクトリ内の `ssh_key` として導出できる。
- `start` は、SSH接続が成功するまで `exec` readyとして扱わない。
- `start` がSSH接続確認に失敗した場合、VM起動成功とexec ready失敗を区別して報告する。

## 受け入れ基準

- `spind vm exec base -- uname` が成功し、VM内の出力を返す。
- `spind vm exec base -- ls` が成功する。
- `spind vm exec base -- pwd` が成功する。
- `spind vm exec base -- ping -c 1 -W 3 127.0.0.1` が成功する。
- VM内コマンドの標準出力と標準エラーが区別して転送される。
- VM内コマンドの終了コードが `spind vm exec` の終了コードになる。
- 存在しないVM名では失敗する。
- 停止中VMでは失敗する。
- Virtualization.framework backendのexec通信は `VZVirtioSocketDevice` を使う。
- Cloud Hypervisor backendのexec通信はCloud Hypervisorのhost側vsock socketを使う。
- exec protocolはSSHを使う。
- guest側SSH endpointはguest OS上で起動している。
- `create` はVMごとのSSH鍵を生成し、秘密鍵を `0600` で保存する。
- `spind vm exec` は `spind` ユーザーとしてSSH接続する。
- host側relay、guest側proxy、Swift実行部はSSH payloadやコマンド内容を解釈しない。
- `spind vm exec <vm-name> -- <command> [args...]` の `--` 以降が、CLI framework導入後もそのままVM内コマンドへ渡される。
