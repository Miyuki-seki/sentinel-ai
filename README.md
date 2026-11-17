# Sentinel-AI

Sentinel-AI は、Web サービスや API エンドポイントのヘルスチェック（外形監視）を行い、障害検知時に **Gemini API** を活用して自動トリアージおよび根本原因分析・復旧手順の提案を行う AI 搭載監視エージェントです。

---

## 主な機能

1. **自動プロービング & メトリクス記録**
   - 設定されたインターバル（デフォルト 5 秒）で監視対象エンドポイントのヘルスチェックを実行。
   - レイテンシ（ms）、ステータスコード、死活状態を SQLite に自動記録。
2. **AI によるインシデント自動トリアージ (Gemini 2.5 Flash)**
   - 連続障害（2回連続失敗）を検知すると、HTTP ステータスやレスポンスボディ、エラーログを Gemini API へ連携。
   - 障害の要約 (`summary`)、推定原因 (`root_cause`)、重大度 (`severity`)、具体的な復旧コマンド (`remediation`) を構造化 JSON で自動生成。
3. **インシデントの自動クローズ（自己回復検知）**
   - 監視対象が正常ステータスに復帰すると、未解決（`OPEN`）のインシデントを自動的に `RESOLVED` へ遷移。
4. **障害シミュレーション用モックターゲット同梱**
   - DB コネクション枯渇エラーやレイテンシ遅延を再現可能なモックサーバーが付属し、すぐにトリアージ動作を検証可能。
5. **CGO 不要のピュア Go 構成**
   - `modernc.org/sqlite` を採用し、CGO なしで SQLite を利用可能。

---

## システム構成・ワークフロー

```
+-------------------------------------------------------------+
|                        Sentinel-AI                          |
|                                                             |
|  +----------------+     Health Probe     +---------------+  |
|  |                | -------------------> |  Target App   |  |
|  |     Prober     |                      | (mock-target) |  |
|  |                | <------------------- +---------------+  |
|  +-------+--------+     Status/Latency                      |
|          |                                                  |
|          | (Log & Metrics)                                  |
|          v                                                  |
|  +----------------+                                         |
|  |  SQLite Store  |                                         |
|  +-------+--------+                                         |
|          ^                                                  |
|          | (Save Incident)                                  |
|  +-------+--------+       Analyze Failure    +------------+ |
|  |   AI Analyzer  | -----------------------> | Gemini API | |
|  |                | <----------------------- +------------+ |
|  +----------------+    Structured Triage                    |
+-------------------------------------------------------------+
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

---

## 環境変数

`.env.example` をコピーして `.env` を作成します。

```bash
cp .env.example .env
```

| 変数名 | 説明 | デフォルト値 |
| :--- | :--- | :--- |
| `GEMINI_API_KEY` | Gemini API のアクセスキー | - |
| `TARGET_URL` | 監視対象のヘルスチェック URL | `http://mock-target:8081/health` |
| `CHECK_INTERVAL_SEC` | ヘルスチェックの実行間隔（秒） | `5` |
| `PORT` | 監視サーバーの公開ポート | `8080` |
| `DB_PATH` | SQLite データベースファイルの保存先 | `/app/data/sentinel.db` |

> **Note**: `GEMINI_API_KEY` を設定しない場合やサンプルのまま起動した場合は、内蔵の `FallbackAnalyzer` が適用され、ローカル完結で動作確認できます。

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

## 動作確認・障害シミュレーション手順

### 1. 正常稼働の確認

監視サーバーおよびモックサーバーの稼働を確認します。

```bash
# 監視サーバー
curl http://localhost:8080/
# => Sentinel-AI Monitor is running. Check GET /api/incidents for JSON reports.

# モックターゲット
curl http://localhost:8081/health
# => {"status":"healthy"}
```

### 2. 障害のシミュレーション (DB 接続枯渇)

モックサーバーに対して `mode=error` を送信し、障害状態を発生させます。

```bash
curl -X POST "http://localhost:8081/simulate?mode=error"
```

監視ログで 2 回連続の失敗検知後、Gemini による自動トリアージが実行されます。

### 3. トリアージ結果 (インシデント) の確認

監視サーバーの API エンドポイントから生成されたインシデントレポートを取得できます。

```bash
curl -s http://localhost:8080/api/incidents | jq .
```

**インシデント JSON の出力例:**

```json
[
  {
    "id": 1,
    "target_url": "http://mock-target:8081/health",
    "status": "OPEN",
    "status_code": 500,
    "error_sample": "{\"error\":\"FATAL: remaining connection slots are reserved for superuser (SQLSTATE 53300)\",\"status\":\"unhealthy\"}\n",
    "summary": "Health check failed with 500 Internal Server Error due to PostgreSQL connection exhaustion",
    "root_cause": "PostgreSQL connection pool exhausted (SQLSTATE 53300) preventing new database sessions.",
    "severity": "CRITICAL",
    "remediation": [
      "Check active DB connections: SELECT count(*) FROM pg_stat_activity;",
      "Inspect container logs: docker logs sentinel-ai-mock-target-1",
      "Restart target service or increase max_connections / connection pool limits"
    ],
    "created_at": "2026-11-17T02:30:00Z"
  }
]
```

### 4. サービスの復旧

モックサーバーを通常状態（`mode=ok`）に戻します。

```bash
curl -X POST "http://localhost:8081/simulate?mode=ok"
```

次回のプローブ成功時に、対象インシデントの `status` が自動的に `RESOLVED` へ更新されます。

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
