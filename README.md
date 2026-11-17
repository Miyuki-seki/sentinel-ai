# Sentinel-AI

<p align="center">
  <strong>Autonomous Multi-Target Synthetic Monitoring & Incident Triage Agent powered by Google Gemini</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go" alt="Go Version">
  <img src="https://img.shields.io/badge/LLM-Gemini%203.8%20Flash-4285F4?style=flat&logo=google" alt="Gemini">
  <img src="https://img.shields.io/badge/Database-SQLite%20(Pure--Go)-003B57?style=flat&logo=sqlite" alt="SQLite">
  <img src="https://img.shields.io/badge/Container-Docker%20Compose-2496ED?style=flat&logo=docker" alt="Docker">
  <img src="https://img.shields.io/badge/Alerts-Discord%20Webhook-5865F2?style=flat&logo=discord" alt="Discord">
</p>

<p align="center">
  English | <a href="docs/README_ja.md">日本語 (Japanese)</a>
</p>

---

## Overview

Traditional synthetic monitoring tools only inform you that an endpoint is down with generic status codes (e.g. `500 Internal Server Error`), leaving Site Reliability Engineers (SREs) to scramble through logs to identify the root cause under high stress.

**Sentinel-AI** bridges the gap between active black-box probing and automated triage. Built with Go and powered by **Gemini 3.8 Flash**, it continuously monitors multiple endpoints concurrently. When consecutive failures occur, it passes raw responses and transport errors to Gemini, which automatically generates a structured incident report with probable root causes and actionable remediation commands, then instantly notifies your team via **Discord Webhook**.

---

## Key Features

- **Concurrent Multi-Target Probing**:
  Monitors multiple endpoints in parallel using lightweight Go goroutines. Each target runs on its own independent probing cycle (default: every 5 seconds).
- **Autonomous Incident Triage (Gemini 3.8 Flash)**:
  Leverages Gemini Structured Outputs (JSON schema enforcement) to parse error logs, status codes, and latency into an actionable triage report containing `summary`, `root_cause`, `severity` (`CRITICAL` / `WARNING`), and step-by-step `remediation` commands.
- **Non-Blocking Architecture**:
  LLM inference runs completely asynchronously in background workers. While one failing service is being analyzed by Gemini (up to 20s), monitoring of healthy endpoints continues uninterrupted.
- **Rich Discord Webhook Notifications**:
  Instantly posts color-coded embed cards to Discord with severity badges, diagnostic summaries, and remediation instructions.
- **Automatic Incident Resolution (Self-Healing)**:
  When an unhealthy target returns HTTP 200, the system automatically marks open incidents as `RESOLVED` and logs recovery time.
- **Chaos Simulation Targets Included**:
  Comes with two mock target microservices capable of simulating database connection exhaustion (`53300`) and high latency on demand.
- **Pure-Go Embedded Storage & Auto-Cleanup**:
  Uses `modernc.org/sqlite` (no CGO required) configured with WAL mode. An internal cleanup worker automatically purges probe logs (24h) and resolved incidents (30d) to prevent disk bloat.

---

## Architecture

```
+------------------------------------------------------------------------------------+
|                                    Sentinel-AI                                     |
|                                                                                    |
|  +--------------------+       Health Probe (5s)       +--------------------------+ |
|  |                    | ----------------------------> | mock-target-1 (:8081)    | |
|  |                    |                               +--------------------------+ |
|  |   Prober Workers   |                                                            |
|  | (Goroutine / URL)  |       Health Probe (5s)       +--------------------------+ |
|  |                    | ----------------------------> | mock-target-2 (:8082)    | |
|  +---------+----------+                               +--------------------------+ |
|            |                                                                       |
|            | (Latency & Metrics)                                                   |
|            v                                                                       |
|  +--------------------+                                                            |
|  | SQLite Store (WAL) |                                                            |
|  +---------+----------+                                                            |
|            ^                                                                       |
|            | (Async Save Incident)                                                 |
|  +---------+----------+        Analyze Failure        +--------------------------+ |
|  |    AI Analyzer     | ----------------------------> | Gemini 3.8 Flash API     | |
|  |   (Non-blocking)   | <---------------------------- +--------------------------+ |
|  +---------+----------+       Structured Triage                                    |
|            |                                                                       |
|            | (Webhook POST / Embed)                                                |
|            v                                                                       |
|  +--------------------+                                                            |
|  |  Discord Notifier  | ----------------------------> Discord Channel              |
|  +--------------------+                                                            |
+------------------------------------------------------------------------------------+
```

---

## Project Structure

```text
.
├── cmd/
│   ├── monitor/          # Core monitoring agent & API server
│   └── mock-target/      # Fault simulation target microservices
├── internal/
│   ├── analyzer/         # Gemini LLM triage client with structured schemas
│   ├── notifier/         # Discord webhook integration (rich embeds)
│   └── store/            # Pure-Go SQLite storage layer (WAL mode & retention)
├── deployments/
│   ├── Dockerfile.monitor
│   ├── Dockerfile.target
│   └── docker-compose.yml
├── docs/
│   └── README_ja.md      # Japanese documentation
├── data/                 # SQLite database storage directory
├── .env.example          # Environment variables template
└── Makefile              # Build & orchestration shortcuts
```

---

## Environment Variables

Copy `.env.example` to create your `.env` file:

```bash
cp .env.example .env
```

| Variable | Description | Default |
| :--- | :--- | :--- |
| `GEMINI_API_KEY` | Google Gemini API key | _(Optional in fallback mode)_ |
| `GEMINI_MODEL` | Gemini model name | `gemini-3.8-flash` |
| `TARGET_URLS` | Comma-separated target URLs | `http://mock-target-1:8081/health,http://mock-target-2:8081/health` |
| `DISCORD_WEBHOOK_URL`| Discord webhook URL for incident alerts | _(Disabled if empty)_ |
| `CHECK_INTERVAL_SEC` | Probe interval in seconds per target | `5` |
| `PORT` | Monitor HTTP API port | `8080` |
| `DB_PATH` | Path to the SQLite database file | `/app/data/sentinel.db` |
| `PROBE_LOG_RETENTION_HOURS` | Retention period for probe logs | `24` (1 day) |
| `INCIDENT_RETENTION_DAYS` | Retention period for resolved incidents | `30` (30 days) |
| `CLEANUP_INTERVAL_MIN` | Interval for automatic log cleanup | `60` (1 hour) |

> **Note**: If `GEMINI_API_KEY` is not provided, Sentinel-AI automatically switches to `FallbackAnalyzer`, allowing local offline testing without an API key.

---

## Quickstart

### 1. Start with Docker Compose

```bash
# Build and launch in background
make up

# Tail live logs
make logs
```

### 2. Stop Containers

```bash
make down
```

---

## Interactive Fault Simulation Demo

Verify multi-target independent probing and automated AI triage in 4 simple steps:

### Step 1: Verify Initial Health Status

Check that the monitor and both mock targets are running:

```bash
# Sentinel-AI Monitor
curl http://localhost:8080/
# => Sentinel-AI Monitor is running. Check GET /api/incidents for JSON reports.

# Target 1 (Port 8081)
curl http://localhost:8081/health
# => {"status":"healthy"}

# Target 2 (Port 8082)
curl http://localhost:8082/health
# => {"status":"healthy"}
```

### Step 2: Inject Fault into Target 1

Trigger an intentional PostgreSQL connection pool exhaustion failure on Target 1:

```bash
curl -X POST "http://localhost:8081/simulate?mode=error"
```

### Step 3: Observe Real-Time AI Triage

Watch the container logs (`make logs`):
1. Target 1 detects 2 consecutive failures.
2. Background AI triage is initiated asynchronously.
3. **Target 2 continues probing normally every 5s (`probe ok`) without interruption.**
4. Gemini returns structured root-cause analysis and remediation steps.
5. An alert card is dispatched to Discord.

### Step 4: Check Incident API & Auto-Recovery

Inspect the generated incident report:

```bash
curl -s http://localhost:8080/api/incidents | jq .
```

**Sample Incident Report:**

```json
[
  {
    "id": 1,
    "target_url": "http://mock-target-1:8081/health",
    "status": "OPEN",
    "status_code": 500,
    "error_sample": "{\"error\":\"FATAL: remaining connection slots are reserved for superuser (SQLSTATE 53300)\",\"status\":\"unhealthy\"}",
    "summary": "Target service failed health checks due to database connection exhaustion",
    "root_cause": "PostgreSQL connection pool exhausted (SQLSTATE 53300) preventing application from establishing new sessions",
    "severity": "CRITICAL",
    "remediation": [
      "Query active DB connections: SELECT count(*) FROM pg_stat_activity;",
      "Inspect container logs: docker logs sentinel-ai-mock-target-1-1",
      "Scale up connection pool limits or restart the backend service"
    ],
    "created_at": "2026-11-17T04:30:00Z"
  }
]
```

Recover Target 1:

```bash
curl -X POST "http://localhost:8081/simulate?mode=ok"
```

The next probe cycle automatically detects recovery and updates the incident status to `RESOLVED`.

---

## API Reference

### Monitor Service (`:8080`)

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/` | Liveness check |
| `GET` | `/api/incidents` | Returns the 20 most recent incidents with Gemini triage reports |

### Mock Target Services (`:8081`, `:8082`)

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Target health check endpoint |
| `POST` | `/simulate?mode={ok\|error\|slow}` | Switches target operating mode for chaos testing |

---

## Technical Highlights

- **Concurrency & Resilience**: Goroutines per URL with thread-safe atomic flags (`atomic.Bool`) ensure non-blocking LLM triage and zero cross-target interference.
- **Type-Safe LLM Integration**: Uses the official Google GenAI SDK with JSON schema enforcement for zero-parsing-error structured outputs.
- **Zero-Dependency SQLite**: Employs pure-Go `modernc.org/sqlite` in WAL mode with connection busy timeouts to achieve robust concurrent read/write operations without CGO.
