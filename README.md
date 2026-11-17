# Sentinel-AI

Sentinel-AI は、複数の Web サービスや API エンドポイントのヘルスチェック（外形監視）を並行して行い、障害検知時に **Gemini API** を活用して自動トリアージ・根本原因分析・復旧手順の提案を生成し、**Discord Webhook** へ通知する AI 搭載監視エージェントです。

---

## 主な機能

1. **複数ターゲットの完全独立・並行プロービング**
   - カンマ区切りで複数のターゲット URL を指定可能（`TARGET_URLS`）。
   - ターゲットごとに独立した Goroutine で監視を実行し、定期インターバル（デフォルト 5 秒）でヘルスチェックを行います。
2. **AI によるインシデント自動トリアージ (Gemini 3.8 Flash)**
   - 対象エンドポイントで連続 2 回の障害を検知すると、HTTP ステータスやレスポンスボディ、エラーログを Gemini API へ自動連携。
   - 障害の要約 (`summary`)、推定原因 (`root_cause`)、重大度 (`severity`)、具体的な復旧コマンド (`remediation`) を構造化 JSON で自動生成。
3. **ノンブロッキング監視アーキテクチャ**
   - あるターゲットで Gemini トリアージ（LLM 推論）が実行されている最中も、バックグラウンド Goroutine で処理されるため、正常稼働している他のターゲットの 5 秒周期プロービングは一切ブロックされず継続します。
4. **Discord Webhook への自動インシデント通知**
   - インシデント発生時、Gemini が生成したトリアージレポート（重大度カラーリング、推定原因、復旧手順、エラーサンプル）を Discord チャンネルへリッチな Embed 形式で即時投稿。
5. **インシデントの自動クローズ（自己回復検知）**
   - 監視対象が正常ステータスに復帰すると、未解決（`OPEN`）のインシデントを自動的に `RESOLVED` へ遷移。
6. **障害シミュレーション用モックターゲット同梱**
   - DB コネクション枯渇エラーやレイテンシ遅延を再現可能なモックサーバーが付属し、すぐにトリアージ動作や通知を検証可能。
7. **CGO 不要のピュア Go 構成**
   - `modernc.org/sqlite` を採用し、WAL モードによる並行読み書き耐性を備えています。

---

## システム構成・ワークフロー

```
+--------------------------------------------------------------------------------+
|                                  Sentinel-AI                                   |
|                                                                                |
|  +-------------------+       Health Probe (5s)      +------------------------+ |
|  |                   | ---------------------------> | Target 1 (mock-target) | |
|  |                   |                              +------------------------+ |
|  |  Prober Workers   |                                                         |
|  | (Goroutine / URL) |       Health Probe (5s)      +------------------------+ |
|  |                   | ---------------------------> | Target 2 (Service B)   | |
|  +---------+---------+                              +------------------------+ |
|            |                                                                   |
|            | (Log & Latency Metrics)                                           |
|            v                                                                   |
|  +-------------------+                                                         |
|  | SQLite Store(WAL) |                                                         |
|  +---------+---------+                                                         |
|            ^                                                                   |
|            | (Async Save Incident)                                             |
|  +---------+---------+        Analyze Failure       +------------------------+ |
|  |    AI Analyzer    | ---------------------------> |       Gemini API       | |
|  |  (Non-blocking)   | <--------------------------- +------------------------+ |
|  +---------+---------+       Structured Triage                                 |
|            |                                                                   |
|            | (Webhook POST / Embed)                                            |
|            v                                                                   |
|  +-------------------+                                                         |
|  |  Discord Notifier | ---------------------------> Discord Channel            |
|  +-------------------+                                                         |
+--------------------------------------------------------------------------------+
```

---

## ディレクトリ構成

```text
.
├── cmd/
│   ├── monitor/          # 監視エージェント & Web サーバーのエントリポイント
│   └── mock-target/      # 障害シミュレーション用モックサーバー
├── internal/
│   ├── analyzer/         # Gemini 連携およびトリアージ解析ロジック
│   ├── notifier/         # Discord Webhook 等の通知クライアント
│   └── store/            # SQLite データベース操作ロジック
├── deployments/
│   ├── Dockerfile.monitor
│   ├── Dockerfile.target
│   └── docker-compose.yml
├── data/                 # SQLite データベースファイル格納先
├── .env.example          # 環境変数テンプレート
└── Makefile              # 開発・運用用タスクランナー
```

---

## 必要要件

- Docker / Docker Compose
- (ローカル直接実行の場合) Go 1.24 以上
- Google Gemini API キー（未設定時はモックの Fallback トリアージが動作）
- (任意) Discord Webhook URL

---

## 環境変数

`.env.example` をコピーして `.env` を作成します。

```bash
cp .env.example .env
```

| 変数名 | 説明 | デフォルト値 |
| :--- | :--- | :--- |
| `GEMINI_API_KEY` | Gemini API のアクセスキー | - |
| `GEMINI_MODEL` | 使用する Gemini モデル | `gemini-3.8-flash` |
| `TARGET_URLS` | 監視対象 URL（カンマ区切りで複数指定可能） | `http://mock-target-1:8081/health,http://mock-target-2:8081/health` |
| `TARGET_URL` | 後方互換用（`TARGET_URLS` 未設定時に参照） | `http://mock-target:8081/health` |
| `DISCORD_WEBHOOK_URL` | 通知先 Discord Webhook URL（未設定時は通知スキップ） | - |
| `CHECK_INTERVAL_SEC` | 各ターゲットのヘルスチェック間隔（秒） | `5` |
| `PORT` | 監視サーバーの公開ポート | `8080` |
| `DB_PATH` | SQLite データベースファイルの保存先 | `/app/data/sentinel.db` |

---

## クイックスタート

### 1. Docker Compose による起動

```bash
# ビルド & バックグラウンド起動
make up

# ログの確認
make logs
```

### 2. コンテナ停止

```bash
make down
```

---

## 動作確認・複数ターゲット障害シミュレーション手順

### 1. 正常稼働の確認

監視サーバーおよび 2 つのモックターゲットの稼働を確認します。

```bash
# 監視サーバー
curl http://localhost:8080/
# => Sentinel-AI Monitor is running. Check GET /api/incidents for JSON reports.

# モックターゲット 1 (ポート 8081)
curl http://localhost:8081/health
# => {"status":"healthy"}

# モックターゲット 2 (ポート 8082)
curl http://localhost:8082/health
# => {"status":"healthy"}
```

### 2. 片方のターゲットで障害をシミュレーション

Target 1（ポート 8081）にのみ `mode=error` を送信して障害を発生させます。

```bash
curl -X POST "http://localhost:8081/simulate?mode=error"
```

- **ログの確認 (`make logs`)**:
  - Target 1 は連続失敗検知によりバックグラウンドで **Gemini AI トリアージ**（および Discord 通知）がトリガーされます。
  - **重要**: Target 1 のトリアージ中であっても、正常な Target 2（ポート 8082）は一切止まることなく **5 秒ごとの監視プローブ（`probe ok`）が継続**されます。

### 3. トリアージ結果 (インシデント) の確認

監視サーバーの API エンドポイントから、Target 1 に関するインシデントレポートを取得できます。

```bash
curl -s http://localhost:8080/api/incidents | jq .
```

### 4. ターゲット 1 の復旧

Target 1 を通常状態（`mode=ok`）に戻します。

```bash
curl -X POST "http://localhost:8081/simulate?mode=ok"
```

次回のプローブ成功時に、Target 1 の未解決インシデントが自動的に `RESOLVED` に更新されます。

---

## API 仕様

### 監視サーバー (`:8080`)

| メソッド | パス | 説明 |
| :--- | :--- | :--- |
| `GET` | `/` | サーバー死活状態の確認 |
| `GET` | `/api/incidents` | 直近のインシデント一覧（最新20件、Gemini トリアージ結果含む） |

### モックターゲット (`:8081`)

| メソッド | パス | 説明 |
| :--- | :--- | :--- |
| `GET` | `/health` | ヘルスチェックエンドポイント |
| `POST` | `/simulate?mode={ok\|error\|slow}` | 稼働モードの変更（正常・DB障害・遅延） |
