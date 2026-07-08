# ADR-0005: ビルドコンテキストは initContainer 経由、パイプライン起動は post-receive フックとする

- Status: Accepted
- Date: 2026-07-08

## Context

(1) push されたコミットのソースツリーを Kaniko Job へ渡す方法、
(2) push 受信からパイプラインを起動し進捗を pusher に返す方法、の 2 つを決める必要があった。

## Decision

### ビルドコンテキスト

Kaniko は plain HTTP の tar コンテキスト取得をサポートしないため:

1. server が `git archive` でコンテキストを tar.gz 化し PVC に保存
2. build Job の initContainer (busybox) が server の
   `/internal/contexts/...` から wget して emptyDir に展開
3. kaniko は `--context=dir:///workspace` でビルド

### パイプライン起動

ベアリポジトリの **post-receive** フックが hook の stdin をそのまま
`POST /internal/hooks/post-receive` (同一 Pod 内 127.0.0.1) に curl で転送し、
server が streaming response で返す進捗行を stderr に書く。
git receive-pack が stderr を sideband で pusher に中継するため、
`git push` の画面にビルド・デプロイの進捗が `remote:` 行としてリアルタイム表示される。

## Consequences

- pre-receive ではなく post-receive のため、**ビルドが失敗しても push (ref 更新) は成功する**。
  Heroku は pre-receive で push 自体を拒否するが、quarantine 環境のオブジェクトを
  server プロセスから読む複雑さを避けた。失敗は push 出力・`minato releases` の
  failed ステータスで報告され、稼働中のリリースには影響しない
- パイプラインは push のコネクションではなく独立した context (timeout 8 分) で実行
  されるため、`git push` を中断してもデプロイが中途半端に止まらない
- server イメージに git に加えて curl が必要になる
