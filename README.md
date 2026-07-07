# minato

**git push すると、数十秒後にアプリが本番 URL で動いている。**

minato は Kubernetes 上に構築した Heroku スタイルのセルフホスト PaaS です。
アプリのリポジトリを `git push` すると、コンテナイメージのビルド・デプロイ・URL 発行までを全自動で行います。

```
$ minato apps create hello
Created app "hello".
  git remote:  http://minato.localtest.me/git/hello.git
  url:         http://hello.localtest.me

$ git push minato main
...
remote: -----> Building v1 ...
remote: -----> Deploying v1 ...
remote: -----> https://hello.localtest.me is live!

$ curl http://hello.localtest.me   # HTTP 200
```

## 特徴

- **git push でデプロイ** — Git smart HTTP の受け口をクラスタ内に持ち、push を契機に Kaniko でイメージをビルドして自動デプロイ
- **アプリごとの Namespace 分離** — Deployment / Service / Ingress を自動生成し、アプリ同士は干渉しない
- **運用系 CLI** — ログのリアルタイムストリーミング、`minato scale web=3`、ワンコマンドのロールバック
- **ローカル完結** — kind クラスタ上で動作し、macOS のローカル環境だけで全機能を検証可能

## 必要なもの

- macOS (Apple Silicon / Intel)
- Docker Desktop
- Go 1.26+
- kind / kubectl

## クイックスタート

```console
$ make cluster-up      # kind クラスタ + レジストリ + ingress-nginx を構築
$ make deploy-server   # minato-server をクラスタにデプロイ
$ make install-cli     # minato CLI をビルド
```

サンプルアプリをデプロイする:

```console
$ minato apps create sample
$ cd examples/sample-app
$ git init && git add -A && git commit -m "init"
$ git remote add minato http://minato.localtest.me/git/sample.git
$ git push minato main
$ curl http://sample.localtest.me
```

## CLI コマンド

| コマンド | 説明 |
|---|---|
| `minato apps create <name>` | アプリ作成 (Namespace + Git リポジトリの初期化) |
| `minato apps list` | アプリ一覧 |
| `minato apps destroy <name>` | アプリの完全削除 |
| `minato status` | リリース・Pod・URL の状態 |
| `minato logs -f` | 全 Pod のログをリアルタイムストリーミング |
| `minato scale web=3` | レプリカ数の変更 |
| `minato releases` | リリース履歴 |
| `minato rollback` | ひとつ前のリリースに戻す |

## ドキュメント

- [SPEC.md](SPEC.md) — スコープ・マイルストーン・受け入れ基準
- [docs/architecture.md](docs/architecture.md) — アーキテクチャ詳細設計
- [docs/adr/](docs/adr/) — 設計判断の記録 (ADR)

## テスト

```console
$ make e2e   # kind クラスタ上で push→ビルド→デプロイ→scale→logs→rollback を自動検証
```

## License

[MIT](LICENSE)
