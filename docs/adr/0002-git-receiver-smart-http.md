# ADR-0002: Git 受け口は minato-server 内の Git smart HTTP とする

- Status: Accepted
- Date: 2026-07-08

## Context

「git push でデプロイ」の受け口が必要。候補: SSH (本家 Heroku 方式) / smart HTTP /
CLI からの tarball アップロード。

## Decision

minato-server がベアリポジトリをホストし、Git smart HTTP プロトコル
(`info/refs` + `git-receive-pack`) を実装する。push 受信の完了を server 内で検知して
ビルド・デプロイのパイプラインを起動する。

## Consequences

- `git push minato main` という Heroku 同等の UX を、SSH 鍵管理なしで実現できる
- 受け口とパイプラインが同一プロセスのため、ビルド進捗を push 応答の
  `remote:` 行にリアルタイムで流し込める (post-receive フック方式ではプロセスが
  分断され、この体験を作りにくい)
- リポジトリの永続化に PVC が必要になる (server は完全ステートレスにはならない)
- 認証は実装しない (ローカル検証専用。スコープ外)
