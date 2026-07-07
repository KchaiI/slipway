# ADR-0004: 制御プレーンはクラスタ内 API サーバ、CLI は薄いクライアントとする

- Status: Accepted
- Date: 2026-07-08

## Context

Git 受け口・ビルド起動・リソース生成・ログ中継をどこに置くか。
候補: クラスタ内 API サーバ / CLI が client-go で直接クラスタを操作するサーバレス構成。

## Decision

`minato-server` をクラスタ内 Deployment (ns: `minato-system`) として動かし、
すべての操作を REST API + Git smart HTTP に集約する。CLI は kubeconfig に依存しない
薄い HTTP クライアントとする。

## Consequences

- git push の受け口とデプロイ実行者が同一コンポーネントになり、構成が一元化される
- CLI 利用者に kubeconfig や RBAC 設定が不要 (実際の PaaS と同じ信頼モデル)
- server 自身のビルド・デプロイ手順が必要になる (`make deploy-server` で自動化)
- server には ClusterRole (Namespace 作成ほか) を付与する。操作対象 Namespace の
  `minato-app-*` への限定はアプリケーションコードで担保する
