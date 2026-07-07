# ADR-0003: Ingress は ingress-nginx、ドメインは localtest.me とする

- Status: Accepted
- Date: 2026-07-08

## Context

アプリごとに URL を発行するため、ホスト名ベースのルーティングと、
127.0.0.1 に解決されるワイルドカードドメインが必要。

## Decision

- Ingress Controller: **ingress-nginx** (kind 公式ドキュメントの構成。hostPort 80 で公開)
- ドメイン: **`<app>.localtest.me`** (公開 DNS で任意のサブドメインが 127.0.0.1 に解決される)
- ingress-nginx の manifest はバージョン固定で `deploy/ingress-nginx/` に vendoring する

## Consequences

- /etc/hosts の編集や dnsmasq が不要。ブラウザ・curl がそのまま使える
- 外部 DNS に依存するためオフライン環境では解決できない (許容。代替は /etc/hosts)
- Traefik / Gateway API と比べ機能は保守的だが、kind での実績と情報量を優先
- vendoring により `make cluster-up` がネットワーク状況に左右されず再現可能
