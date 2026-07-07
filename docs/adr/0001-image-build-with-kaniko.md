# ADR-0001: イメージビルドは Kaniko Job + Dockerfile 方式とする

- Status: Accepted
- Date: 2026-07-08

## Context

git push を受けてコンテナイメージをクラスタ内で自動ビルドする必要がある。
候補: Kaniko / Cloud Native Buildpacks (kpack) / BuildKit 常駐 / ホスト側 docker build。

## Decision

push ごとに Kubernetes Job として Kaniko を起動し、アプリ同梱の Dockerfile をビルドする。

## Consequences

- デーモン常駐なし・非特権で動き、kind との相性が良い。ビルドの単位が Job になるため
  k8s API (Job watch) だけでパイプラインを構成できる
- arm64 (Apple Silicon) のマルチアーチイメージが公式に提供されている
- アプリ側に Dockerfile を要求する (Buildpacks のような Dockerfile-less UX は諦める)。
  規約は「Dockerfile 必須 + PORT で listen」の 2 点に収まるため許容
- ベースイメージ pull がビルド時間を支配するため、docker.io の pull-through ミラーを
  併設し `--registry-mirror` で参照する (90 秒 DoD 対策)
