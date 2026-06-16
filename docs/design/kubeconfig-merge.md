# Kubeconfig Merge

## 目的

spindは、kind-ready snapshotから作成したVMに対して、host側kubectlが使えるkubeconfigを生成する。

標準動作ではVM別kubeconfigを生成し、global kubeconfigへの自動mergeは行わない。

Phase 12では、標準動作を変えずに、global kubeconfigへ明示的にmergeまたはunmergeするCLIを実装する。

この文書は、kindのkubeconfig実装をsource codeから調査し、spindが習う点とspind固有に変える点を定める。

調査対象:

- repository: `kubernetes-sigs/kind`
- commit: `d6009ccba8df27aa455e85561b73a0170e60c3d6`
- package: `pkg/cluster/internal/kubeconfig/...`

確認コマンド:

```sh
go test ./pkg/cluster/internal/kubeconfig/...
```

## kindの実装

### kubeconfig生成

kindはcontrol-plane node内の `/etc/kubernetes/admin.conf` を読み、kind用kubeconfigへ変換する。

source:

- `pkg/cluster/internal/kubeconfig/kubeconfig.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/read.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/helpers.go`

挙動:

- kubeadm kubeconfigはcluster、user、contextがそれぞれ1件であることを前提にする。
- cluster名、user名、context名を同じkeyへ変更する。
- keyは `kind-<cluster-name>`。
- context内のcluster参照とuser参照も同じkeyへ変更する。
- current-contextも同じkeyへ変更する。
- external kubeconfigでは、serverだけをhostから到達できるAPI server endpointへ差し替える。
- kindが直接扱わないkubeconfig fieldは `OtherFields` 相当で保持し、読み書き後も落とさない。

### merge

kindの `WriteMerged` は、kind用kubeconfigを既存kubeconfigへmergeする。

source:

- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/merge.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/merge_test.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/encode.go`

挙動:

- 書き込み先pathは `pathForMerge` で1つ選ぶ。
- 書き込み前に `<path>.lock` を `O_CREATE|O_EXCL` で作る。
- 既存fileがなければ空のconfigとして扱う。
- cluster、user、contextは、同名entryがあれば置換し、なければ追加する。
- current-contextはkind側のcontextへ必ず変更する。
- 書き込み先directoryがなければ `0755` で作る。
- kubeconfig fileは `0600` で書く。
- 書き込みは `os.WriteFile` で行う。atomic renameではない。
- 既存YAML commentは保持しない。

commentを保持しない理由:

- 既存fileは `yaml.Unmarshal` でstructへ読み込まれる。
- 書き戻しは `yaml.Marshal` と `sigs.k8s.io/yaml` のround tripで再生成される。
- commentはstructやmapへ保持されない。

### path選択

kindは複数pathの `KUBECONFIG` を拒否しない。

source:

- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/paths.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/paths_test.go`

`paths` の挙動:

- explicit kubeconfig pathがあれば、それだけを使う。
- explicit pathがなく `KUBECONFIG` があれば、path listを使う。
- `KUBECONFIG` のpath listから空文字と重複は除く。
- `KUBECONFIG` がなければ `~/.kube/config` を使う。
- path選択では、path listの各要素をabsolute path化、clean、symlink解決、home展開しない。
- 重複除外はpath文字列の完全一致で判定する。

`pathForMerge` の挙動:

- pathが1つだけならそれを使う。
- pathが複数ある場合、最初に存在するfileへ書く。
- pathが複数あり、どれも存在しない場合、最後のpathへ書く。

### remove

kindの `RemoveKIND` は、mergeとは違い、対象pathを1つに絞らない。

source:

- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/remove.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/remove_test.go`

挙動:

- `paths` が返す全pathを順に処理する。
- 各pathをlockして読む。
- `kind-<cluster-name>` に一致するcluster、user、contextを削除する。
- current-contextが同じkeyなら空にする。
- 変更があったfileだけ書き戻す。

## kindから習う点

spindは次を採用する。

- kubeconfigを文字列置換ではなく、構造化データとして読み書きする。
- guest内のkubeconfigを元にし、hostから到達できるserver endpointだけを差し替える。
- cluster、user、context、current-contextを同じ安定keyで揃える。
- fileを書き換える場合はlockを取る。
- 書き込み先directoryを必要に応じて作成する。
- kubeconfig file modeは `0600` を標準にする。
- 明示merge optionでは、kind互換の `paths` と `pathForMerge` を使う。
- 既存kubeconfigの未知fieldを保持する。
- global kubeconfig mergeではYAML comment保持を保証しない。
- unmergeでは、merge先候補の全pathを調べる。

## spind固有の判断

kindと違い、spindは初期スコープでglobal kubeconfigを自動更新しない。

理由:

- kind-ready snapshot由来VMは、restoreごとにhost portが変わる可能性がある。
- 複数VMを同時起動すると、同じsnapshot由来でも別contextとして扱う必要がある。
- spindはVM lifecycleとsnapshot lifecycleを扱うため、kind cluster lifecycleだけを扱うkindより削除条件が複雑になる。
- global kubeconfigは利用者の既存cluster設定を含むため、破損時の影響が大きい。

標準動作では、spindは次を満たす。

- VM別kubeconfigを `~/.spind/vms/<vm-name>/kubeconfig` に生成する。
- cluster名、user名、context名は `spind-<vm-name>` とする。
- current-contextは `spind-<vm-name>` とする。
- global kubeconfigは読み書きしない。
- `KUBECONFIG` が設定されていても、restore後の出力先はVM別kubeconfigに固定する。
- `spind vm list` は `export KUBECONFIG=...` のhintを表示してよい。

## Phase 12で実装するCLI

Phase 12では、次のコマンドを実装する。

```sh
spind kubeconfig merge <name> [--kubeconfig <path>] [--replace] [--set-current-context]
spind kubeconfig unmerge <name> [--kubeconfig <path>]
```

`<name>` は起動済みVM名である。

`spind kubeconfig merge` は、VM別kubeconfigをsourceとしてglobal kubeconfigへmergeする。

`spind kubeconfig unmerge` は、global kubeconfigからspind管理entryを削除する。

Phase 12では、次は実装しない。

```sh
spind vm start <name> --merge-kubeconfig
```

`spind vm start --merge-kubeconfig` は、merge/unmerge CLIが安定した後の別phaseで再判断する。

### 動作確認環境

Phase 12の手動動作確認は、Linux/KVM上のCloud Hypervisor backendで実施する。

- macOS host上のglobal kubeconfigでは手動動作確認しない。
- Virtualization.framework backendでは、Phase 12のglobal kubeconfig merge手動動作確認を必須にしない。
- 一時HOMEまたは一時 `KUBECONFIG` を作り、その中のkubeconfigだけを書き換える。
- 利用者の既存 `~/.kube/config` を直接対象にしない。
- Cloud Hypervisor backendでkind-ready VMを起動し、そのVMに対してmerge/unmergeを確認する。

### merge

`spind kubeconfig merge` の動作は次にする。

- 書き込み先は、明示optionがあればそのpathを使う。
- 明示optionがない場合、`KUBECONFIG` があればkind互換の `pathForMerge` 規則で書き込み先を1つ選ぶ。
- `KUBECONFIG` が未設定なら `~/.kube/config` を使う。
- VM別kubeconfigが存在しない場合は失敗する。
- 同名 `spind-<vm-name>` entryが存在する場合、既定では失敗する。
- 同名entryを置き換える場合は、明示的な `--replace` を必須にする。
- 既存のspind管理外entryは、未知fieldを含めて保持する。
- 既存のtop-level未知field、cluster未知field、user未知field、context未知fieldを保持する。
- current-contextは既定では変更しない。
- current-contextを変更する場合は、明示的な `--set-current-context` を必須にする。
- 書き込みはlockを取る。
- 書き込みはtemporary fileへ書いてからatomic renameする。
- merge失敗時は元fileを保持する。
- 既存YAML commentの保持は保証しない。

### unmerge

`spind kubeconfig unmerge` の動作は次にする。

- 明示optionがあれば、そのpathだけを対象にする。
- 明示optionがない場合、`KUBECONFIG` があればkind互換の `paths` 規則で候補pathを列挙する。
- `KUBECONFIG` が未設定なら `~/.kube/config` だけを対象にする。
- 対象pathごとにlockを取る。
- `spind-<vm-name>` に一致するcluster、user、contextだけを削除する。
- spind管理外entryは、未知fieldを含めて保持する。
- current-contextが `spind-<vm-name>` なら空にする。
- 対象entryが他のcontextから参照されている場合は失敗する。

kindは同名entryをreplaceしcurrent-contextをkind contextへ変更するが、spindは既定ではそれを採用しない。

spindでは、path選択はkindに揃えつつ、利用者の既存kubeconfigを壊さないことを優先し、replaceとcurrent-context変更を明示操作にする。

kindはatomic renameを使っていないが、spindはglobal kubeconfigを書き換える場合にatomic renameを必須にする。

kindは既存YAML commentを保持しない。spindもglobal kubeconfig mergeではcomment保持を保証しない。

ただし、YAML comment以外のkubeconfig fieldは保持する。comment非保持を理由に、`namespace`、`certificate-authority`、`client-certificate`、`client-key`、`auth-provider`、`exec`、`proxy-url`、`tls-server-name`、`extensions`、その他spindが直接扱わないfieldを落としてはならない。

## Phase 12の完了条件

Phase 12は、次を満たしたら完了とする。

- `spind kubeconfig merge <name>` を実装している。
- `spind kubeconfig unmerge <name>` を実装している。
- VM別kubeconfigを標準とし、`spind vm start` はglobal kubeconfigを自動変更しない。
- kind-compatibleな構造化mergeを実装している。
- kind-compatibleな `paths` と `pathForMerge` を実装している。
- path選択前に `KUBECONFIG` のpathをabsolute path化、clean、symlink解決、home展開しない。
- 既存kubeconfigの未知fieldを保持している。
- lockとatomic writeを実装している。
- 同名cluster、user、contextの扱いをテストしている。
- current-contextを変更しない既定動作をテストしている。
- `--replace` と `--set-current-context` をテストしている。
- `KUBECONFIG` 未設定、単一path、複数path、空path、重複pathをテストしている。
- 複数path時に、最初の既存fileへmergeすることをテストしている。
- 複数path時に、既存fileがなければ最後のpathへmergeすることをテストしている。
- 既存YAML commentの保持を保証しないことをテストまたはrunbookで確認している。
- 既存entryの未知fieldがmerge/unmerge後も保持されることをテストしている。
- merge失敗時に元fileが保持されることをテストしている。
- unmergeが候補path全体からspind管理entryだけを削除することをテストしている。
- 統合runbookにkubeconfig merge安全性確認が入っている。

Phase 12完了後も、標準動作はVM別kubeconfigのままにする。

global kubeconfig mergeを標準に切り替えるかどうかは、Phase 12完了後の別phaseで再判断する。

## 参照source

- `pkg/cluster/internal/kubeconfig/kubeconfig.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/read.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/helpers.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/paths.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/paths_test.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/merge.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/merge_test.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/encode.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/remove.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/remove_test.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/lock.go`
- `pkg/cluster/internal/kubeconfig/internal/kubeconfig/write.go`
