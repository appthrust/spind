# Roadmap

## 目的

spindは、最小VM CLI、saved state snapshot、Docker Hostを両backendで安定して使える状態へ段階的に進める。

このroadmapは、直近の実装優先度と受け入れゲートを定義する。

## Phase 7: Docker Host仕様の安定化

Phase 7では、Docker Hostのbackend差分を実装へ反映し、利用者に見える状態表示とwarningを整える。

対象:

- Virtualization.framework backend。
- Cloud Hypervisor backend。
- 通常起動VM。
- snapshot由来VM。

要件:

- Virtualization.framework Docker Hostでは、通常起動VMとsnapshot由来VMの両方でhost path共有mountを使える。
- Cloud Hypervisor Docker Hostでは、通常起動VMでhost path共有mountを使える。
- Cloud Hypervisor snapshot由来Docker Hostでは、高速saved-state restoreを優先し、host path共有mountを非対応にする。
- Cloud Hypervisor snapshot由来Docker Hostでも、Docker API、container実行、Docker pull、container外向き通信、host loopbackへのport publishは使える。
- `spind vm list` は、Docker Host能力ごとにready、unavailable、unsupportedを区別して表示する。
- `spind vm start` は、追加能力が使えない場合にwarningと関連log pathを表示する。
- unsupportedは失敗ではなく、仕様上の非対応として扱う。

Phase 7の完了条件:

- `docs/design/docker-host.md` のDocker Host support matrixに従って実装されている。
- `vm list` でhost path共有mountが非対応なのか、失敗なのかを区別できる。
- stale relay、`passt` process、vhost-user socket、legacy tap、`virtiofsd`、socketのcleanupが行われる。
- Docker Host e2eが通常起動VMで通る。
- Cloud Hypervisor snapshot由来Docker Hostでは、host path共有mountを確認対象外にしたe2eが通る。

## Phase 8: 両backend runbookを通す

Phase 8では、統合runbookを実装完了ゲートとして通す。

対象runbook:

- `docs/design/minimum-cli-runbook.md`

実行するtask:

```sh
devbox run task build
devbox run task build-image-docker
devbox run task e2e
```

Phase 8の完了条件:

- Virtualization.framework backendで統合runbookが通る。
- Cloud Hypervisor backendで統合runbookが通る。
- 実行commit hash、実行環境、成功または失敗、restore時間、log pathが記録される。
- どちらか片方だけでは完了扱いにしない。

## Phase 9: UX整理

Phase 9では、CLI表示とエラーメッセージを整理する。

要件:

- `spind vm list` のDocker Host表示を読みやすくする。
- unsupportedな機能は、失敗ではなく非対応として表示する。
- unavailableな機能は、原因とlog pathを表示する。
- snapshot由来VMで使える機能と使えない機能を明確にする。
- `spind vm start` のwarningは、VM起動成功と追加能力の失敗を分けて表示する。
- `spind vm list --json` は、人間向け表示と同じsupport状態、ready状態、理由、log pathを含める。

Phase 9の完了条件:

- `docs/design/docker-host.md` のstatus表示仕様に従って実装されている。
- CHV snapshot由来VMのhost path共有mountがunsupportedとして表示される。
- 対応対象の機能が失敗した場合、unavailableと理由とlog pathが表示される。
- runbookのstatus確認が両backendで通る。

## Phase 10: cleanup堅牢化

Phase 10では、停止、snapshot、restore失敗時のcleanupと状態修復を堅牢化する。

対象:

- backend process。
- Docker API relay。
- port publish relay。
- Unix socket。
- `passt` process。
- vhost-user socket。
- legacy tap device。
- `virtiofsd` process。
- virtio-fs socket。
- directory sharing mount。
- VM状態ファイル。

要件:

- `spind vm stop` は通常停止後に関連process、socket、mount、`passt` process、legacy tapを残さない。
- `spind vm stop` はstale PID、stale socket、消えたprocessを検出し、停止済み状態へ修復する。
- `spind snapshot create` は成功時も失敗時も、対象VMのbackend processと補助processを整合した状態にする。
- snapshot restore失敗時は、途中起動したbackend process、relay、port listener、`passt` process、vhost-user socket、legacy tap、`virtiofsd`、socketをcleanupする。
- cleanup失敗時は、VM状態を起動成功として扱わず、原因とlog pathを表示する。
- cleanupは通常操作で `~/.spind` 外のユーザーデータを削除しない。

Phase 10の完了条件:

- stop後にbackend process、Docker relay、port relay、`passt` process、vhost-user socket、legacy tap、`virtiofsd`、endpoint socketが残らない。
- restore失敗を注入しても、状態ファイルが起動中のまま残らない。
- stale状態から再度 `spind vm start` または `spind vm stop` を実行して状態修復できる。
- cleanup観点が統合runbookに含まれる。

## Phase 11: kind-ready snapshot UX

Phase 11では、kind cluster起動済みDocker Host VMをsnapshot化し、restore後すぐにKubernetes APIへ接続できる体験を提供する。

詳細仕様は `docs/design/kind-ready-snapshot.md` に従う。

要件:

- spindは初期スコープでは `kind create cluster` を包まない。
- 利用者はhost側kindとDocker Host endpointを使ってkind clusterを作る。
- `spind snapshot create --kind --kubeconfig <path> --context <name>` でkind-ready snapshotを作る。
- `spind vm start` はkind-ready snapshot由来VMにhost空きportを割り当て、VM別kubeconfigを生成する。
- 標準kubeconfig pathは `~/.spind/vms/<name>/kubeconfig` とする。
- `~/.kube/config` へのmergeは初期スコープ外にする。
- Cloud Hypervisor snapshot由来VMではhost path共有mountなしでもkind API利用を保証する。

Phase 11の完了条件:

- Virtualization.framework backendでkind-ready e2eが通る。
- Cloud Hypervisor backendでkind-ready e2eが通る。
- `kubectl get nodes` がVM別kubeconfigで成功する。
- 複数kind-ready VMを同時起動してもhost portとkubeconfig pathが衝突しない。
- `~/.kube/config` が自動変更されない。

## Phase 12: kubeconfig merge CLI実装

Phase 12では、kindのkubeconfig merge方式を参考にしつつ、spindでglobal kubeconfigへ明示的にmergeまたはunmergeするCLIを実装する。

詳細仕様は `docs/design/kubeconfig-merge.md` に従う。

実開発環境への投入段階は `docs/design/development-environment-rollout.md` に従う。

要件:

- VM別kubeconfigを引き続き標準にする。
- `spind vm start` はglobal kubeconfigを自動変更しない。
- `spind kubeconfig merge <name>` を実装する。
- `spind kubeconfig unmerge <name>` を実装する。
- `spind vm start --merge-kubeconfig` はPhase 12では実装しない。
- kindと同じ構造化mergeを参考にする。
- kindと同じ `KUBECONFIG` path選択規則を参考にする。
- path選択前に `KUBECONFIG` のpathをabsolute path化、clean、symlink解決、home展開しない。
- 既存kubeconfigの未知fieldを保持する。
- spindでは既定で同名entryを置き換えない。
- spindでは既定でcurrent-contextを変更しない。
- global kubeconfigを書き換える場合はlockとatomic writeを使う。

Phase 12の完了条件:

- kubeconfig merge安全性runbookがある。
- 手動動作確認はLinux/KVM上のCloud Hypervisor backendで実施する。
- macOS hostとVirtualization.framework backendでは、global kubeconfig mergeの手動動作確認を必須にしない。
- 一時HOMEまたは一時 `KUBECONFIG` だけを書き換え、利用者の既存 `~/.kube/config` は直接対象にしない。
- `spind kubeconfig merge <name>` がVM別kubeconfigをglobal kubeconfigへmergeできる。
- `spind kubeconfig unmerge <name>` がglobal kubeconfigからspind管理entryを削除できる。
- `KUBECONFIG` 未設定、単一path、複数path、空path、重複pathの扱いが確認されている。
- 複数path時のmerge先が、kindと同じく最初の既存file、または既存fileなしの場合は最後のpathになる。
- 既存entryの `namespace`、`proxy-url`、`tls-server-name`、`extensions` などの未知fieldがmerge/unmerge後も保持される。
- 同名entry、replace、current-context、merge失敗、unmergeの挙動が確認されている。
- merge失敗時に元kubeconfigが保持される。
- Phase 12完了後もglobal mergeを標準にしない。

## Phase 13: 実開発環境への限定投入

Phase 13では、spindを検証用開発環境から日常の開発環境へ限定投入できる状態にする。

詳細仕様は `docs/design/development-environment-rollout.md` に従う。

要件:

- 検証用開発環境への投入条件を満たしている。
- 日常の開発環境ではVM別kubeconfigを標準運用にする。
- `~/.kube/config` へのmergeは明示操作だけにする。
- `spind kubeconfig merge` は `--kubeconfig <tmp-path>` または専用の検証用kubeconfigを推奨する。
- Cloud Hypervisor snapshot由来VMのhost path共有mountは `unsupported` として利用者へ表示する。
- snapshot restoreの速度と、初回create、kind cluster作成、Docker registry accessの時間を分けて説明する。

Phase 13の完了条件:

- `docs/design/development-environment-rollout.md` の検証用開発環境条件が満たされている。
- 限定投入時の制約がstatus、runbook、利用者向け説明へ反映されている。
- Docker registry timeoutとkind readiness待ち失敗のretry条件を、標準投入前の未解決事項として記録している。
- 標準開発環境としての投入はまだ行わないことが明記されている。

## Later

- Cloud Hypervisor upstreamのvirtio-fs snapshot/restore修正後に再検証する。
- Cloud Hypervisor snapshot由来Docker Hostでhost path共有mountを復活できる場合は、設計を更新する。
- `spind vm start --merge-kubeconfig` と `~/.kube/config` へのmerge標準化は、Phase 12完了後に再判断する。
- Docker registry timeoutとkind readiness recoveryのretry条件を定義する。
- 標準開発環境への投入条件を満たしたら、`docs/design/development-environment-rollout.md` の現時点判定を更新する。
- Docker Compose連携は、Docker Hostの基本e2eが安定した後に扱う。
