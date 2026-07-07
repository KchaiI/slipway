# minato アーキテクチャ設計

本書は [SPEC.md](../SPEC.md) の設計を実装レベルまで具体化したもの。個々の設計判断の経緯は [adr/](adr/) を参照。

## 1. 全体像

```
 macOS ホスト
 ┌────────────────────────────────────────────────────────────────┐
 │  minato CLI                       git push                     │
 │      │ REST / chunked stream          │ Git smart HTTP         │
 │      ▼                                ▼                        │
 │  http://minato.localtest.me  (ingress-nginx, hostPort 80)      │
 │  ┌────────────── kind クラスタ "minato" ───────────────────┐   │
 │  │                                                          │   │
 │  │  ns: minato-system                                       │   │
 │  │    minato-server ── Namespace/Deploy/Svc/Ingress 生成    │   │
 │  │      │  │            Kaniko Job 生成・watch              │   │
 │  │      │  └─ ベアリポジトリ + ビルドコンテキスト置き場      │   │
 │  │      ▼                                                   │   │
 │  │  ns: minato-app-<name>   (アプリごとに 1 つ)             │   │
 │  │    Job(kaniko) ─ push ─► kind-registry:5000/<app>:vN     │   │
 │  │    Deployment / Service / Ingress (<app>.localtest.me)   │   │
 │  └──────────────────────────────────────────────────────────┘   │
 │  kind-registry (localhost:5001)   kind-registry-mirror (proxy)  │
 └────────────────────────────────────────────────────────────────┘
```

制御プレーンは単一バイナリ `minato-server` に集約する。CLI と git はどちらも
ingress-nginx 経由 (`minato.localtest.me`) で server に到達する。

## 2. コンポーネント設計

### 2.1 minato-server (`cmd/minato-server`)

単一の HTTP サーバ。責務ごとに internal パッケージへ分割する。

| パッケージ | 責務 |
|---|---|
| `internal/server` | HTTP ルーティング、REST API ハンドラ、Git smart HTTP ハンドラ |
| `internal/build`  | ビルドコンテキスト tar の生成、Kaniko Job の生成と完了 watch |
| `internal/deploy` | Deployment/Service/Ingress の生成 (server-side apply)、rollout 監視 |
| `internal/release`| リリース履歴 (ConfigMap) の read/write、採番、ロールバック解決 |
| `internal/kube`   | client-go の初期化 (in-cluster / kubeconfig 両対応)、共通ヘルパー |

状態は 2 か所にのみ持つ:

1. **Git ベアリポジトリ** — server Pod の PersistentVolumeClaim (`/data/repos/<app>.git`)。
   ビルドコンテキスト tar も `/data/contexts/<app>/<release>.tar.gz` に置く
2. **リリース履歴** — 各アプリ Namespace の ConfigMap `minato-releases`

それ以外 (Pod 数、rollout 状態など) はすべて Kubernetes API から都度取得する。
server 自身は再起動しても PVC と ConfigMap から状態を復元できる。

### 2.2 minato CLI (`cmd/minato`)

cobra 製の薄い HTTP クライアント。kubeconfig には依存しない (すべて server API 経由)。
接続先は `MINATO_API` 環境変数 (デフォルト `http://minato.localtest.me`)。
`-a` フラグ省略時はカレントリポジトリの git remote `minato` の URL からアプリ名を推測する。

## 3. API 設計

### REST API

| Method / Path | 説明 |
|---|---|
| `POST /v1/apps` | アプリ作成。Namespace・ベアリポジトリ・リリース ConfigMap を初期化 |
| `GET /v1/apps` | アプリ一覧 |
| `GET /v1/apps/{app}` | 状態 (現行リリース、Pod、URL、replicas) |
| `DELETE /v1/apps/{app}` | Namespace ごと削除 + ベアリポジトリ削除 |
| `POST /v1/apps/{app}/scale` | `{"web": 3}` — replicas 変更、Ready まで待つ |
| `GET /v1/apps/{app}/releases` | リリース履歴 |
| `POST /v1/apps/{app}/rollback` | ひとつ前のリリースを新リリースとして再デプロイ |
| `GET /v1/apps/{app}/logs?follow=1` | 全 Pod のログを chunked text でストリーミング |

### Git smart HTTP

| Method / Path | 説明 |
|---|---|
| `GET /git/{app}.git/info/refs?service=git-receive-pack` | ref advertisement |
| `POST /git/{app}.git/git-receive-pack` | push 受信。`git receive-pack` を exec して stdin/stdout を中継 |

push 受信後のパイプライン起動は post-receive フックではなく、receive-pack の
正常終了を server 内で検知して行う (プロセス境界を跨がず、進捗を push 応答の
sideband (`remote: ...` 行) に流し込めるため)。

### ビルドコンテキストの受け渡し

Kaniko は plain HTTP の tar コンテキストを直接扱えないため、次の構成をとる:

1. server が `git archive <sha>` でコンテキスト tar.gz を生成し `/data/contexts/` に保存
2. Kaniko Job に initContainer (busybox) を付け、server の
   `GET /internal/contexts/{app}/{release}.tar.gz` から wget → emptyDir に展開
3. kaniko コンテナは `--context=dir:///workspace` でビルド

## 4. Kubernetes リソース設計

### 命名とラベル

| 対象 | 規約 |
|---|---|
| アプリ Namespace | `minato-app-<name>` |
| Deployment / Service / Ingress | アプリ名と同名 |
| ビルド Job | `build-<app>-v<N>` |
| イメージ | `kind-registry:5000/<app>:v<N>` |
| 共通ラベル | `app.kubernetes.io/managed-by=minato`, `minato.dev/app=<name>` |
| リリースラベル (Pod) | `minato.dev/release=v<N>` |

アプリ名は DNS-1123 label (`[a-z0-9][a-z0-9-]*`, 最大 40 文字) に制限する。

### 生成する Deployment の要点

- `PORT=8080` を環境変数で注入し、containerPort 8080 を公開
- readinessProbe: `GET /` (TCP fallback は使わない — rollout 完了 = HTTP 応答可能 を保証)
- resources: requests/limits を小さめに固定 (ローカル kind 前提)
- `progressDeadlineSeconds: 60` — 起動しないイメージを早期に失敗判定

### RBAC (minato-server の ServiceAccount)

ClusterRole で以下を許可する:

- `namespaces`: create / delete / get / list (アプリ Namespace 管理)
- `deployments`, `services`, `ingresses`, `jobs`, `configmaps`: CRUD
- `pods`: get / list / watch、`pods/log`: get
- `events`: get / list (ビルド・デプロイ失敗の診断用)

対象を `minato-app-*` に絞る仕組みは Kubernetes RBAC には無いため ClusterRole と
するが、server 側のコードで操作対象 Namespace を `minato-app-<app>` に限定する。

## 5. デプロイパイプライン (シーケンス)

```
git push minato main
  → POST /git/<app>.git/git-receive-pack
      receive-pack 完了 (ref 更新確定)
  → release.Allocate()        : vN 採番、ConfigMap に status=building で記録
  → build.PrepareContext()    : git archive → /data/contexts/<app>/vN.tar.gz
  → build.RunJob()            : Kaniko Job 生成 (ns: minato-app-<app>)
      watch Job → 成功: image kind-registry:5000/<app>:vN が registry に存在
                → 失敗: ConfigMap を status=failed に更新、push 応答にログ要約
  → deploy.Apply()            : Deployment/Service/Ingress を server-side apply
      watch rollout → 完了: ConfigMap を status=live に更新、旧リリースを superseded に
  → push 応答 sideband に "-----> http://<app>.localtest.me is live!" を出力
```

- 進捗はすべて push 応答 (`remote:` 行) にリアルタイムで流す。
  `git push` が返ってきた時点でデプロイ完了 (または失敗) が確定している
- ビルド失敗・rollout 失敗時は既存の Deployment に手を加えない (現行バージョン無傷)
- 同一アプリへの並行 push はアプリ単位の mutex で直列化する

## 6. リリースモデル

ConfigMap `minato-releases` (アプリ Namespace 内) に JSON で保存:

```json
{
  "counter": 3,
  "releases": [
    {"version": 1, "image": "kind-registry:5000/hello:v1", "commit": "abc123",
     "createdAt": "...", "status": "superseded", "description": "deploy abc123"},
    {"version": 3, "image": "kind-registry:5000/hello:v1", "commit": "abc123",
     "createdAt": "...", "status": "live", "description": "rollback to v1"}
  ]
}
```

- **rollback は「新しいリリース」** — v2 から v1 に戻すと、v1 と同じ image を指す
  v3 が作られる (Heroku 方式)。履歴が常に前進するため監査が単純になる
- ビルドをやり直さず image タグの付け替えのみなのでロールバックは数秒で完了する

## 7. ログストリーミング

1. CLI が `GET /v1/apps/{app}/logs?follow=1` を開く
2. server はアプリの Pod を list し、各 Pod へ client-go の `GetLogs(follow=true)` を張る
3. 各行に `[pod-name] ` プレフィックスを付けて 1 本の chunked レスポンスに fan-in
4. Pod の増減 (scale 等) は watch で検知し、ストリームを動的に追加・削除

## 8. ローカルインフラ (kind)

kind 公式の [local registry パターン](https://kind.sigs.k8s.io/docs/user/local-registry/) に従う。

| コンテナ | 役割 |
|---|---|
| `minato-control-plane` | kind ノード。hostPort 80 → ingress-nginx |
| `kind-registry` | ビルド成果物レジストリ。ホスト `localhost:5001` ⇔ クラスタ内 `kind-registry:5000` |
| `kind-registry-mirror` | docker.io の pull-through cache。Kaniko の `--registry-mirror` に指定 |

- ノードの containerd に `kind-registry:5000` を insecure (plain HTTP) として設定
- ingress-nginx はバージョン固定の manifest を `deploy/ingress-nginx/` に vendoring
  (ネットワーク断でも `make cluster-up` が再現可能)
- ホストの port 80 が使用中の場合は `MINATO_HTTP_PORT` で変更可能 (URL に `:<port>` が付く)

## 9. 障害モードと対応

| 障害 | 挙動 |
|---|---|
| Dockerfile なし / ビルド失敗 | push 応答にエラー要約 + `minato releases` に failed 記録。現行バージョン無傷 |
| イメージは出来たが起動しない | progressDeadline 超過で rollout 失敗扱い。現行バージョン無傷 |
| server 再起動 | PVC (リポジトリ) + ConfigMap (履歴) から状態復元。進行中ビルドの Job は watch を再確立 |
| 同一アプリへ並行 push | アプリ単位 mutex で直列化 (後着は待たされる) |
