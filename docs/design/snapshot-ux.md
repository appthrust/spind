# Snapshot UX

## 目的

spindは、saved state snapshotを「作った後に探しやすく、restore失敗時に調べやすく、不要になったら安全に消せる」CLI体験として提供する。

## 対象スコープ

- `vm list` の表示改善。
- `snapshot list` の表示改善。
- snapshot由来VMのrestore時間表示。
- restore失敗時のlog path表示。
- 壊れたsnapshotの検出。
- snapshot削除とpruneの安全な導線。

## 対象外

- snapshot export/import。
- remote snapshot registry。
- Docker Hostやkind向けの専用表示。
- 差分snapshot。
- snapshot内容の自動修復。

## 表示方針

- 人間向けの標準表示はtable形式にする。
- script向けのmachine-readable出力は `--json` で提供する。
- 時刻はlocal timeではなくRFC3339 UTCで表示する。
- durationは `1.234s` のような秒単位で表示する。
- sizeはbyte数ではなく、人間向け表示では `MiB` / `GiB` を使う。
- pathは通常表示では必要なものだけを出し、詳細表示で完全pathを出す。

## `spind vm list [<name>]`

VM名を指定しない場合、VM一覧を表示する。

表示項目:

- VM名
- backend
- status
- exec ready
- snapshot由来かどうか
- source snapshot
- last started at

VM名を指定した場合、対象VMの詳細を表示する。

詳細項目:

- VM名
- backend
- status
- PID
- exec ready
- exec socket pathまたはhost側vsock socket path
- source imageまたはsource snapshot
- restore mode
- last restore duration
- serial log path
- backend log path
- event log path
- VM directory

`--json` を指定した場合、同じ情報をJSON objectまたはJSON arrayで出力する。

## `spind snapshot list`

snapshot一覧を表示する。

表示項目:

- snapshot名
- backend
- source VM
- created at
- CPU数
- memory size
- disk size
- state size
- health

`health` は次のいずれかである。

- `ok`: 必須metadataとbackend artifactが揃っている。
- `missing-artifact`: 必須artifactが不足している。
- `invalid-metadata`: `metadata.json` が読めない、または必須fieldがない。
- `unsupported-backend`: backend名が現在のspindで扱えない。

壊れたsnapshotが存在しても、`snapshot list` は一覧表示自体を成功させる。

ただし、壊れたsnapshotの行は `health` で明示し、標準エラーに警告を出す。

`--strict` を指定した場合、壊れたsnapshotを1件でも検出したら失敗する。

## `spind snapshot inspect <snapshot-name>`

snapshotの詳細を表示する。

表示項目:

- snapshot名
- backend
- source VM
- created at
- CPU数
- memory size
- guest SSH vsock port
- kernel command line
- artifact一覧
- artifact size
- health
- snapshot directory
- kind-ready metadataがある場合は、kind-readyであること
- kind-ready metadataがある場合は、元context名、元cluster名、snapshot作成時node一覧、kubeconfig templateの有無

`--json` を指定した場合、同じ情報をJSON objectで出力する。

## `spind snapshot delete <snapshot-name>`

snapshot artifactを削除する。

- 起動中VMは削除しない。
- snapshot由来VMは削除しない。
- snapshot由来VMが存在しても、snapshot artifactは削除できる。
- 削除前に対象snapshotのhealthを確認し、metadataが壊れていてもdirectory名が一致すれば削除対象にできる。
- 削除成功後、削除したsnapshot名と削除した総sizeを表示する。

初期スコープでは確認promptを出さない。

## `spind snapshot prune`

不要なsnapshotをまとめて削除する。

初期スコープでは、次のoptionを提供する。

- `--older-than <duration>`: 指定durationより古いsnapshotを削除対象にする。
- `--backend <backend-name>`: backendで絞り込む。
- `--all`: すべてのsnapshotを削除対象にする。
- `--dry-run`: 削除せず、対象一覧と削除予定sizeだけ表示する。

`snapshot prune` は、`--all`、`--older-than`、`--backend` のいずれも指定されていない場合は失敗する。

`--all` は全snapshotを明示的に対象にするoptionであり、`--older-than` や `--backend` と同時指定できない。

`snapshot prune` は、`--dry-run` なしでも対象一覧と削除総sizeを表示する。

初期スコープでは、snapshot由来VMが存在するsnapshotも削除対象にしてよい。
理由は、snapshot由来VMはrestoreに必要なartifactをVM directory内に持つためである。

## Restore時間表示

snapshot由来VMの `spind vm start <name>` は、restore開始からexec readyまでのend-to-end時間を表示する。

表示項目:

- VM名
- backend
- restore duration
- exec ready duration
- total duration

Virtualization.framework backendでは、可能な場合に次も表示する。

- `restoreMachineStateFrom` duration
- `resume` duration

Cloud Hypervisor backendでは、可能な場合に次も表示する。

- restore to paused duration
- resume duration
- SSH ready duration

## 失敗時診断

`start`、`snapshot create`、`exec` が失敗した場合、CLIは原因調査に使うlog pathを表示する。

表示するpath:

- VM directory
- state file path
- serial log path
- backend log path
- event log path
- Swift runner log path
- Cloud Hypervisor API socket path

存在しないlog pathは表示しない。

失敗時のmessageは、ユーザー操作の失敗とbackend内部失敗を区別する。

例:

```text
spind: restore failed: Cloud Hypervisor restore exited before API socket became ready
logs:
  vm: ~/.spind/vms/from-prepared
  backend: ~/.spind/vms/from-prepared/cloud-hypervisor.log
  serial: ~/.spind/vms/from-prepared/serial.log
```

## 壊れたsnapshotの扱い

snapshotのhealth checkは、次を確認する。

- `metadata.json` が読める。
- backend名がある。
- 共通artifact `ssh_key` と `ssh_key.pub` がある。
- Virtualization.framework snapshotでは `vf/state.vzvmsave`、`vf/kernel`、`vf/initramfs`、`vf/disk.img` がある。
- Cloud Hypervisor snapshotでは `cloud-hypervisor/config.json`、`cloud-hypervisor/memory-ranges`、`cloud-hypervisor/state.json`、`cloud-hypervisor/kernel`、`cloud-hypervisor/initramfs`、`cloud-hypervisor/disk.img` がある。

壊れたsnapshotから `create --snapshot` しようとした場合は失敗する。

## 受け入れ基準

- `spind vm list` でVM一覧を確認できる。
- `spind vm list <name>` でVM詳細とlog pathを確認できる。
- `spind snapshot list` でsnapshot一覧とhealthを確認できる。
- 壊れたsnapshotがある場合、`snapshot list` はhealthで異常を表示する。
- `spind snapshot inspect <snapshot-name>` でartifactとsizeを確認できる。
- `spind snapshot delete <snapshot-name>` でsnapshot artifactを削除できる。
- `spind snapshot prune --dry-run` で削除予定snapshotと削除予定sizeを確認できる。
- snapshot由来VMの `spind vm start` はrestore開始からexec readyまでのend-to-end時間を表示する。
- restore失敗時に調査に必要なlog pathが表示される。
