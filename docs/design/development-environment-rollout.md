# Development Environment Rollout

## 目的

spindを実開発環境へ投入する段階と制約を定義する。

この文書は、Cloud Hypervisor backendの実測結果を踏まえ、検証用開発環境、日常の開発環境、標準開発環境を分ける。

## 投入段階

### 検証用開発環境

検証用開発環境は、spindの機能を開発者が限定用途で試す段階である。

次を満たす場合、Cloud Hypervisor backendの検証用開発環境へ投入できる。

- Linux/KVM環境上で `task build` が成功する。
- Linux/KVM環境上で `task build-image-docker` が成功する。
- Linux/KVM環境上で `task e2e` が成功する。
- Linux/KVM環境上の一時 `KUBECONFIG` を使った `spind kubeconfig merge` と `spind kubeconfig unmerge` が成功する。
- VM別kubeconfigを標準運用にしている。
- 検証用VM名と検証用snapshot名だけを対象にしている。
- 検証後にVM、snapshot、relay、socket、`passt` process、vhost-user socket、legacy tap deviceがcleanupされる。

検証用開発環境では、利用者の普段使い `~/.kube/config` を直接書き換えてはならない。

### 日常の開発環境への限定投入

日常の開発環境への限定投入は、開発者が通常作業の一部でspindを使う段階である。

限定投入では次を守る。

- VM別kubeconfigを標準運用にする。
- `~/.kube/config` へのmergeは明示操作だけにする。
- `spind vm start` はglobal kubeconfigを自動変更しない。
- `spind kubeconfig merge` を使う場合、`--kubeconfig <tmp-path>` または専用の検証用kubeconfigを推奨する。
- macOSの普段使い `~/.kube/config` を対象にする運用は標準にしない。
- Docker bind mountでhost source treeを使う作業では、Cloud Hypervisor snapshot由来VMのhost share非対応を制約として表示する。
- snapshot restoreの速度と、初回create、kind cluster作成、Docker registry accessの時間を分けて説明する。

Cloud Hypervisor snapshot由来VMは、高速saved-state restoreを優先するためhost path共有mountを `unsupported` とする。

host source treeをcontainerへbind mountする作業では、次のいずれかを使う。

- 通常起動VM。
- Virtualization.framework backend。
- Cloud Hypervisor snapshot由来VM以外のDocker Host。

### 標準開発環境

標準開発環境は、spindを日常の標準手順として案内できる段階である。

標準投入には次を満たす必要がある。

- Virtualization.framework backendとCloud Hypervisor backendの統合runbookが、同じ設計snapshotに対して通っている。
- Docker registry timeoutのretry条件を設計で定義している。
- kind control-plane readiness待ち失敗のretry条件を設計で定義している。
- global kubeconfig mergeを日常利用へ広げる場合、backup、rollback、lock競合、comment非保持、既存field保持の利用者向け説明が揃っている。
- Cloud Hypervisor snapshot由来VMでhost shareを非対応のまま許容するのか、別phaseで復活させるのかが決まっている。
- snapshotからのcreate/startに関する性能目標を、初回create、saved-state restore、Kubernetes readyの3つに分けて定義している。

標準開発環境では、runbookが通った事実だけで、利用者の普段使いkubeconfigやhost source tree bind mountを安全に扱えるとはみなさない。

## 外部要因とretry

次は外部要因で揺らぐ可能性がある。

- Docker registry access。
- image pull。
- kind control-plane API readiness。
- Kubernetes node readiness。

これらの失敗を成功扱いにするには、runbook内で明示されたretry条件を満たす必要がある。

retry条件が未定義の失敗は、標準開発環境の投入ゲートでは失敗扱いにする。

## 性能目標の分離

spindの速度評価は、次を分けて記録する。

- 初回createにかかる時間。
- saved-state restore開始からexec readyまでの時間。
- kind-ready snapshot由来VMでKubernetes APIがreadyになるまでの時間。

kind-ready snapshotの価値は、主にsaved-state restore後にKubernetes APIへ短時間で接続できる点で評価する。

初回image作成、kind cluster作成、Docker registry accessの時間をsaved-state restoreの性能として扱わない。

## 現時点の判定

現時点の判定は次である。

- 検証用開発環境には投入できる。
- 日常の開発環境には限定投入できる。
- 標準開発環境としての投入はまだ行わない。

global kubeconfig自動変更とCloud Hypervisor snapshot由来host shareは、標準投入前の未解決事項である。
