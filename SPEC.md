# minato — Heroku-style PaaS on Kubernetes

> コンセプト: **git push すると、数十秒後にアプリが本番 URL で動いている。**

## 1. スコープ

### やること
- `git push minato main` を起点とした 自動ビルド → 自動デプロイ → URL 発行
- アプリごとの Namespace 分離 (`minato-app-<name>`)
- Deployment / Service / Ingress の自動生成・更新
- CLI `minato`: アプリ作成 / デプロイ状況 / ログのリアルタイムストリーミング / スケール変更 / ロールバック
- kind クラスタ上で完結する自動 e2e テスト (`make e2e`)

### やらないこと (スコープ外)
- 認証・マルチユーザー・課金
- TLS / HTTPS (ローカル検証のため HTTP のみ)
- アドオン (DB 等)、環境変数管理 UI、Procfile の web 以外のプロセスタイプ
- クラウド環境対応 (kind / macOS ローカルのみ)
- 高可用性 (minato-server は単一レプリカ)

## 2. 確定済み設計判断

| 分岐 | 決定 | 理由 |
|---|---|---|
| イメージビルド方式 | **Kaniko Job + Dockerfile 必須** | デーモン不要・非特権で kind と相性が良い。push ごとに k8s Job を生成する構成で k8s API を活用。arm64 対応済み |
| Git 受け口 | **クラスタ内 Git smart HTTP** | minato-server がベアリポジトリをホストし、receive-pack 完了を契機にパイプライン起動。SSH 鍵管理不要で Heroku 同等の UX |
| Ingress | **ingress-nginx + localtest.me** | kind 公式ドキュメントの実績構成。`<app>.localtest.me` は公開 DNS で 127.0.0.1 に解決され /etc/hosts 編集不要 |
| 制御プレーン | **クラスタ内 API サーバ (minato-server)** | Git 受け口・ビルド起動・リソース生成・ログ中継を一元化。CLI は薄い HTTP クライアント |

(以後の設計判断は `docs/adr/` に ADR (Architecture Decision Record) として記録して進める)

## 3. アーキテクチャ

```
 macOS ホスト
 ┌──────────────────────────────────────────────────────────┐
 │  minato CLI ──── HTTP ────┐          git push ───────────┼──┐
 │                           │                              │  │
 │  Docker                   ▼                              │  │
 │  ┌─────────────── kind クラスタ ────────────────────┐    │  │
 │  │ ns: ingress-nginx                                │    │  │
 │  │   ingress-nginx-controller (hostPort 80)  ◄──────┼────┼──┘
 │  │        │ *.localtest.me                          │    │
 │  │ ns: minato-system      ▼                         │    │
 │  │   minato-server (API + Git smart HTTP)           │    │
 │  │     ├─ ベアリポジトリ (PVC/emptyDir)              │    │
 │  │     ├─ push 受信 → Kaniko Job 生成 ─────┐        │    │
 │  │     └─ Deployment/Service/Ingress 生成  │        │    │
 │  │ ns: minato-app-<name> (アプリごと)       ▼        │    │
 │  │   build Job (Kaniko) ──── push ──► [registry]    │    │
 │  │   Deployment / Service / Ingress   (kind ネット   │    │
 │  │      ▲ image: registry:5000/<app>:vN  上の別コンテナ)│  │
 │  └──────────────────────────────────────────────────┘    │
 │  kind-registry (localhost:5001 ⇔ kind-registry:5000)     │
 └──────────────────────────────────────────────────────────┘
```

### コンポーネント

| コンポーネント | 実装 | 役割 |
|---|---|---|
| `minato-server` | Go + client-go。クラスタ内 Deployment | REST API、Git smart HTTP、ビルドパイプライン、k8s リソース生成、ログ中継 (WebSocket/chunked) |
| `minato` CLI | Go (cobra) | server の API を叩く薄いクライアント |
| ローカルレジストリ | `registry:2` (kind 公式 local-registry パターン) | ビルド成果物の保存。ホストからは `localhost:5001`、クラスタ内からは `kind-registry:5000` |
| pull-through ミラー | `registry:2` (proxy mode, docker.io ミラー) | Kaniko の `--registry-mirror` に指定し、ベースイメージ取得を高速化 (90 秒 DoD 対策) |
| ingress-nginx | kind 用公式 manifest | `*.localtest.me` の HTTP ルーティング |

### デプロイパイプライン (push から 200 まで)

1. `git push minato main` → minato-server の `POST /git/<app>.git/git-receive-pack`
2. receive-pack 完了 → 新リリース `vN` を採番、対象 commit を tar 化して ConfigMap ではなく **ビルドコンテキストとして Kaniko Job に供給** (server がコンテキストを HTTP で配る方式、詳細は実装時に決定し ADR に記録)
3. Kaniko Job (ns: `minato-app-<name>`) が Dockerfile をビルドし `kind-registry:5000/<app>:vN` へ push
4. Job 成功を watch で検知 → Deployment の image を `vN` に更新 (初回は Deployment/Service/Ingress を新規生成)
5. rollout 完了 → `http://<app>.localtest.me` で 200

### アプリの規約 (contract)

- リポジトリ直下に `Dockerfile` 必須
- コンテナは `PORT` 環境変数 (デフォルト 8080) で listen
- プロセスタイプは `web` のみ

### リリース管理とロールバック

- リリース履歴 (vN → image digest, commit SHA, 日時) はアプリ Namespace 内の ConfigMap `minato-releases` に保存
- `minato rollback` は「ひとつ前のリリース」の image に Deployment を更新する (新リリース vN+1 として記録 = Heroku 方式)

### Namespace 分離

- アプリごとに `minato-app-<name>` を生成。リソースはすべてその中
- minato-server は `minato-system`。ServiceAccount + RBAC (Namespace/Deployment/Service/Ingress/Job/Pod/ConfigMap の CRUD + pods/log) を付与

### CLI コマンド体系

```
minato apps create <name>     # アプリ作成 (Namespace + Git リポジトリ初期化、git remote URL を表示)
minato apps list
minato apps destroy <name>
minato status  [-a <app>]     # リリース、Pod、URL の状態
minato logs    [-a <app>] -f  # 実行中 Pod のログをリアルタイムストリーミング
minato scale   [-a <app>] web=3
minato releases [-a <app>]
minato rollback [-a <app>]
```
(`-a` 省略時はカレントディレクトリの git remote `minato` から推測)

## 4. マイルストーン

各マイルストーンは **受け入れテストの通過のみ** をもって完了とし、通過ごとに git commit する。

### M0 — 環境ブートストラップ
`make cluster-up` 一発で、kind クラスタ + ローカルレジストリ + pull-through ミラー + ingress-nginx が立つ。

**受け入れテスト** (`make test-m0`):
- `kind get clusters` に `minato` が存在し、node が Ready
- `localhost:5001/v2/` が 200 (レジストリ疎通)
- echo テストアプリ (公開イメージ) を手動 apply し、`http://echo.localtest.me` が 200
- `make cluster-down` で完全に破棄できる

### M1 — 制御プレーンとリソース生成
minato-server と CLI の骨格。**指定イメージ** のデプロイまで (Git/ビルドはまだ)。
- `minato apps create` → Namespace + RBAC 作成
- server 内部 API `deploy(app, image)` → Deployment/Service/Ingress 生成、rollout 監視
- `minato status` / `minato apps list` / `minato apps destroy`

**受け入れテスト** (`make test-m1`):
- `minato apps create demo` → ns `minato-app-demo` が生成される
- 公開イメージ (http-echo 等) を内部 API でデプロイ → `http://demo.localtest.me` が 200
- `apps destroy` で Namespace ごと消える

### M2 — git push → 自動ビルド → 自動デプロイ (コア体験)
Git smart HTTP 受け口 + Kaniko ビルドパイプライン。

**受け入れテスト** (`make test-m2`):
- サンプルアプリ (Go 製、リポジトリ同梱 `sample/`) を `git push` → **90 秒以内に** `http://<app>.localtest.me` が 200
- 2 回目の push (コード変更) で新バージョンが反映される (レスポンス内容の変化で確認)
- Dockerfile が壊れている push はビルド失敗としてレポートされ、既存バージョンは無傷

### M3 — 運用系 CLI (logs / scale / rollback)
- `minato logs -f`: 複数 Pod のログを server 経由でリアルタイムストリーミング
- `minato scale web=N`: replicas 変更 + 完了待ち
- `minato releases` / `minato rollback`

**受け入れテスト** (`make test-m3`):
- `scale web=3` → Ready Pod が 3 つになる
- `logs -f` がリクエストログを 5 秒以内に流す (curl を打って確認)
- v2 デプロイ後 `rollback` → v1 の内容が URL で返り、`releases` に v3 (v1 の再リリース) が記録される

### M4 — e2e 自動化と Definition of Done
`make e2e` がクラスタ構築から全シナリオまでを自動検証。

**受け入れテスト** (`make e2e` グリーン = DoD):
1. push から 90 秒以内に発行 URL で HTTP 200
2. 2 つ目のアプリを push → 両方 200、互いのリソースが相手の Namespace に存在しないことを検証 (分離の実証)
3. デプロイ → scale → logs → rollback が CLI で完結
4. クリーンな状態 (`make cluster-down` 後) から e2e 一式が再現可能

## 5. リポジトリ構成

[golang-standards/project-layout](https://github.com/golang-standards/project-layout) に準拠する。

```
cmd/
  minato/            # CLI エントリポイント
  minato-server/     # 制御プレーン エントリポイント
internal/
  cli/               # CLI コマンド実装 (cobra)
  server/            # REST API + Git smart HTTP ハンドラ
  build/             # Kaniko Job の生成・監視
  deploy/            # Deployment/Service/Ingress の生成・rollout 監視
  release/           # リリース履歴の管理
  kube/              # client-go ヘルパー
deploy/
  kind/              # kind クラスタ設定
  ingress-nginx/     # ingress-nginx manifest (バージョン固定で vendoring)
  server/            # minato-server の manifest (RBAC 含む)
docs/
  architecture.md    # アーキテクチャ詳細設計
  adr/               # Architecture Decision Records
examples/
  sample-app/        # e2e で使う Go 製サンプル Web アプリ (Dockerfile 付き)
test/
  e2e/               # e2e テスト (Go test)
hack/                # cluster-up 等の運用スクリプト
Makefile             # cluster-up / cluster-down / deploy-server / test-m* / e2e
```

開発メモ (NOTES.md 等) は .gitignore で untrack とし、リポジトリには公開品質のファイルのみを含める。

## 6. リスクと対策

| リスク | 対策 |
|---|---|
| 初回ビルドでベースイメージ pull が遅く 90 秒を超える | pull-through ミラー + Kaniko `--registry-mirror` / `--cache=true`。`make cluster-up` 時にミラーをウォームアップ |
| ホストの port 80 が使用中 | kind の extraPortMappings は 80 を試み、衝突時は 8080 にフォールバック (URL に `:8080` が付く)。e2e は環境変数でポートを吸収 |
| macOS arm64 でのイメージ互換性 | Kaniko / registry / ingress-nginx はいずれも arm64 マルチアーチ配布あり。M0 で疎通確認 |
| minato-server 自身のイメージ更新の手間 | `make deploy-server` で build → registry push → rollout を一発化 |

## 7. 検証環境 (確認済み)

- macOS (Darwin 25.4.0, arm64) / Go 1.26.4 / Docker 27.4.0 / kubectl v1.36.1 / kind v0.32.0
