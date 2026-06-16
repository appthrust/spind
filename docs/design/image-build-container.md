# Image Build Container

## 目的

`spind image build` は、hostのNix環境やLinux固有toolに依存せず、Docker container内でimage成果物を生成する。

この設計では、hostの必須要件をDockerに寄せる。Nix、flake support、`mke2fs` などのbuild toolはcontainer内で用意する。

## 対象スコープ

- `spind image build <name>` はDocker container内でNix buildを実行する。
- Docker containerのbase imageはNixが利用できる公開imageを使う。
- container実行部分はbuilder runtimeとして抽象化し、将来Docker以外のOCI container runnerへ差し替えられる構造にする。
- build toolchainは、image templateのflakeとは別のbuilder専用 `flake.nix` と `flake.lock` で固定する。
- builder専用flakeはrepository内で管理する。
- Nix build成果物の実体化とwritable data disk作成はcontainer内で完了する。
- host側spindはcontainerが生成したimage directoryを検証し、local image storeへatomicに配置する。

## 対象外

- host上の `nix build` 実行経路。
- Darwin専用builder VM経路。
- `SPIND_NIX_BUILD_LOCAL` のような互換fallback flag。
- builder専用Docker imageの配布と保守。
- image template flakeとbuilder専用flakeの統合。
- Docker以外のbuilder runtime実装。ただし内部設計は将来追加できる境界を持つ。

## Builder Runtime

builder runtimeは、OCI-compatible imageを実行するための内部interfaceである。

初期実装のbuilder runtimeはDockerとする。Docker固有のcommand生成、volume作成、container削除、label指定はDocker runtime実装内に閉じ込める。image build本体は、Docker command lineを直接組み立てず、builder runtime interfaceを通してcontainerを起動する。

builder runtime interfaceは、少なくとも次の能力を提供する。

- runtime名を返す。
- named volumeを作成または存在確認する。
- container名、image、env、mount、named volume、working directory、command、labelを指定してcontainerを実行する。
- containerのstdout、stderr、exit codeを呼び出し側へ返す。
- 失敗時またはcancel時に、指定されたcontainer名をbest-effortで削除する。

将来追加できるbuilder runtime候補:

- Docker。
- Apple `container`。
- `nerdctl` 経由のcontainerd。

runtime差分は、host側commandの表現、volume管理、container削除、label support、file ownership、mount性能に限定する。container内で実行されるbuilder scriptと成果物契約はruntimeに依存しない。

初期の利用者向け要件はDockerでよい。builder runtimeの選択UI、config、環境変数は初期スコープ外とする。ただし実装内部では、後から選択肢を追加できるようにDocker実装へ密結合しない。

## Builder Flake

builder専用flakeは、container内で必要な補助toolを提供する。Nix本体はbase imageに含まれるものを使う。

必要なtool:

- `bash`
- `coreutils`
- `e2fsprogs`
- `jq`

builder専用flakeは、image template flakeとは別のlock fileを持つ。templateがどのNixOS systemを作るかと、builder container内の補助toolをどう固定するかは別の関心として扱う。

## Build Flow

host側spindは、build work directoryを作る。

```text
~/.spind/image-build/<name>/
  template/
  builder/
  output/
```

host側spindは、image templateを `template/` へ、builder専用flakeを `builder/` へ配置する。

host側spindはDocker containerを起動し、work directoryをcontainerへmountする。

```text
/work/template  image template
/work/builder   builder専用flake
/work/output    image output
```

build container名は次の形式にする。

```text
spind-image-builder-<image-name>-<build-id>
```

`<build-id>` は同時buildや前回異常終了時の衝突を避けるための短い一意値である。containerはbuild完了後に削除される短命resourceであり、cache目的では使わない。

host側spindは、Nix store cache用のDocker volumeをcontainerへmountする。cacheはhost上のNix installを前提にせず、Dockerが管理するvolumeとして保持する。

```text
spind-image-builder-nix-store:/nix
```

Nix storeはarchitectureごとにvolumeを分けない。Nix store pathはderivationごとに分かれるため、異なるarchitectureの成果物は同じstore内に共存できる。

cache volume名にはversionを含めない。cacheは補助dataであり、壊れた場合や互換性を切り替える場合はvolumeを削除して再作成する。

base imageもNix本体を `/nix` 以下に持つため、空のcache volumeを初めて使う場合、base image内の `/nix` が利用可能な状態で初期化されていなければならない。

container内では、builder専用dev shellからbuild scriptを起動する。

```sh
nix develop /work/builder#image-builder -c /work/builder/build-image.sh
```

`build-image.sh` は次を行う。

1. `/work/template` で `nix build` を実行する。
2. Nix build resultをsymlinkのままhostへ出さず、`cp -aL` で `/work/output/image` へ実体copyする。
3. `/work/output/image/metadata.json` を読み、`disks[].create` を持つdiskを作成する。
4. disk fileを `truncate` で指定sizeにし、`mke2fs` でformatする。
5. `/work/build` と `/work/output` の所有者・modeをhost userから削除・移動できる状態にする。

`--data-size` が指定された場合、host側spindはその値をcontainerへ渡す。containerは `disks[].create.size` の代わりに渡されたsizeでwritable data diskを作成する。metadata上の作成仕様はNix build resultを正とし、host側spindは既存仕様と同じく `name` と `createdAt` だけを最終化する。

このbuild scriptと `/work` 以下のdirectory layoutはbuilder runtime非依存である。Docker、Apple `container`、`nerdctl` のいずれを使っても、container内から見えるpathと環境変数は同じ契約に揃える。

## 成果物契約

containerは `/work/output/image` に完成したimage directoryを生成する。

`/work/output/image` には少なくとも次を含める。

- `metadata.json`
- `kernel`
- `initramfs`
- `metadata.json` の `disks` に書かれたdisk file

`metadata.json` の `disks[].create` は、container内でdisk fileを作るための指示として扱う。host側spindはdata diskを作成しない。

host側spindは、containerが生成したimage directoryを検証し、`metadata.json` の `name` と `createdAt` を最終化してimage storeへ配置する。

## Host要件

hostに必要な外部toolはDockerだけである。

hostにNix、flake support、`mke2fs`、`jq`、Linux filesystem toolは要求しない。

## Cache

Nix store cacheはDocker volumeとして保持する。cache volumeはbuild work directoryとは分ける。

cache volume名は `spind-image-builder-nix-store` とする。

cache volumeはbuildの再現性を決めるsource of truthではない。builder専用flake lock、image template flake lock、base image digestがsource of truthであり、cacheは再build時間を短くするためだけに使う。

## 受け入れ基準

- `spind image build docker` はhostにNixがなくても成功する。
- `spind image build docker` はhostに `mke2fs` がなくても成功する。
- Nix build resultはsymlinkではなく実体directoryとしてhost側work directoryへ出力される。
- `docker-data.img` はcontainer内で作成、formatされる。
- build成功まで既存image directoryは置き換えない。
- Docker containerが失敗した場合、stderrにcontainer内build logを表示する。
- Docker containerが生成した成果物が不足している場合、image storeを更新せず失敗する。
- 2回目以降のbuildはDocker volume上のNix store cacheを再利用できる。
- build container名は `spind-image-builder-` で始まる。
- Nix store cache volume名は `spind-image-builder-nix-store` である。
- Docker固有の処理はbuilder runtime実装に閉じており、image build本体はbuilder runtime interface越しにcontainerを実行する。
