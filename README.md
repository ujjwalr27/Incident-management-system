# Incident Management System (IMS)

A mission-critical, high-throughput incident management system built with Go, React, PostgreSQL (TimescaleDB), MongoDB, and Redis.

---

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          CLIENT LAYER                                    │
│  React 18 + Vite + TailwindCSS (port 3000)                              │
│  Login → Live Feed (SSE) → Incident Detail → RCA Form                   │
└────────────────────────────┬─────────────────────────────────────────────┘
                             │ REST + SSE (token in cookie / Bearer)
                             ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                         API GATEWAY (Go / chi)  port 8080               │
│  JWT Middleware → Role Guards → Rate Limiter (token bucket)             │
│  POST /api/v1/signals    GET /api/v1/incidents    /stream (SSE)         │
└────────────────────────────┬─────────────────────────────────────────────┘
                             │
              ┌──────────────▼──────────────┐
              │  Ingress Channel (cap 50k)  │ ← backpressure boundary
              └──────────────┬──────────────┘
                             │
              ┌──────────────▼──────────────┐
              │  Worker Pool (NumCPU × 4)   │
              │  ┌─ Debouncer (sync.Map)    │
              │  ├─ Mongo writer (async)    │
              │  ├─ Postgres upsert (tx)    │
              │  ├─ Alerter strategy        │
              │  └─ Redis cache + pub/sub   │
              └──────┬──────────────────────┘
                     │
     ┌───────────────┼───────────────────────┐
     ▼               ▼                       ▼
┌─────────┐   ┌─────────────────┐   ┌──────────────┐
│ MongoDB │   │  PostgreSQL +   │   │    Redis     │
│ (Data   │   │  TimescaleDB    │   │ live:active  │
│  Lake)  │   │  (Source of     │   │ live:closed  │
│raw_sig. │   │   Truth + TS)   │   │  SSE pub/sub │
└─────────┘   └─────────────────┘   └──────────────┘
```

---

## Tech Stack Rationale

| Component | Choice | Why |
|---|---|---|
| Backend | **Go 1.22** | Goroutines + buffered channels = textbook "modern concurrency primitives". Native 10k+/sec throughput, single binary. |
| Ingestion API | **HTTP/JSON** (batch up to 1000) | Easy to test; rate-limited by token bucket. gRPC-ready by design. |
| Data Lake | **MongoDB** | Schema-flexible, indexed on `component_id + timestamp`, efficient bulk inserts. |
| Source of Truth | **PostgreSQL** | ACID, serializable isolation for state transitions, CHECK constraints on RCA. |
| Timeseries | **TimescaleDB** (Postgres extension) | Same Postgres instance, hypertable for `signal_counts`, no extra infra. |
| Cache | **Redis** | Sorted sets for severity-ordered live feed. Pub/sub as zero-broker SSE fan-out. |
| Auth | **JWT HS256** (access 15m / refresh 7d) | Stateless, role-encoded in claims, httpOnly cookie for SSE. |
| Frontend | **React 18 + Vite + Tailwind** | SSE-native, fast DX, responsive dark-mode UI. |

---

## Setup Instructions

### Prerequisites
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (includes Docker Compose v2)
- Git

### 1. Clone & start everything

```bash
git clone <your-repo-url>
cd zeotap
docker compose up --build
```

Services start in order (Postgres → Mongo → Redis → backend → frontend).
The backend auto-runs migrations on startup.

| Service | URL |
|---|---|
| Frontend | http://localhost:3002 |
| Backend API | http://localhost:8080 |
| Health check | http://localhost:8080/health |
| Ready check | http://localhost:8080/ready |

### 2. Seed demo data (cascading failure simulation)

```bash
# From repo root (Go must be installed locally, or exec into the backend container)
go run backend/scripts/simulate_outage.go --addr http://localhost:8080
```

Open http://localhost:3002, log in as `responder@ims.local / password123`, and watch incidents populate in real-time via SSE.

### 3. Run unit tests

```bash
cd backend
go test ./test/... -v -race
```

### 4. Run load test (requires [k6](https://k6.io/docs/get-started/installation/))

```bash
k6 run backend/scripts/load.js
```

---

## Demo Credentials

| Role | Email | Password |
|---|---|---|
| Admin | admin@ims.local | password123 |
| Responder (dashboard) | responder@ims.local | password123 |
| Producer (ingest) | producer@ims.local | password123 |

---

## How Backpressure Is Handled

The ingest path uses a **bounded in-process channel** (`cap=50,000`) as the single backpressure boundary between HTTP ingestion and the worker pool.

```
Producer → [rate limiter] → [chan *Signal cap=50k] → [worker pool N]
                                      ↑
                               backpressure point
```

**What happens when the channel is full:**
1. The HTTP handler's `select` hits the `default` branch immediately (non-blocking).
2. If **all** signals in the batch are dropped, the server returns **`503 Service Unavailable`** with a `Retry-After: 5` header.
3. If only some are dropped, the server returns **`202 Accepted`** with `{ "accepted": N, "dropped": M }` — the producer can re-submit the dropped subset.

**Why not a blocking put?**
Blocking puts would tie up HTTP goroutines, causing timeouts to cascade into the HTTP server itself — the classic thundering-herd failure mode. Shedding at the boundary protects the server from OOM while giving producers actionable feedback.

**Rate limiting:**
A per-user token bucket (`golang.org/x/time/rate`) caps sustained throughput at `RATE_LIMIT_RPS` (default 20,000/s, burst 5,000) and returns **`429 Too Many Requests`** before signals even reach the channel.

**Worker isolation:**
Each worker fan-outs to separate goroutines for Mongo writes and timeseries upserts so a slow datastore doesn't block the main processing path.

---

## Design Patterns Used

### State Pattern (Workflow Engine)

Work items follow a strict lifecycle enforced at compile time:

```
OPEN → INVESTIGATING → RESOLVED → CLOSED
              ↑____________↓        (re-investigate allowed)
```

Each state implements `CanTransition(to) error`. The `CLOSED` state's guard calls `ValidateClose(rca)` — the single authoritative location that enforces mandatory RCA. The transition is executed inside a **serializable Postgres transaction** to prevent race conditions between concurrent status updates.

### Strategy Pattern (Alerting)

`Alerter` interface → `P0PagerDutyAlerter` | `P1SlackAlerter` | `P2EmailAlerter` | `P3LogAlerter`.
`alerting.Factory(componentType)` selects at runtime:

| Component Type | Severity | Channel |
|---|---|---|
| RDBMS | P0 | PagerDuty |
| MCP_HOST, API | P1 | Slack |
| CACHE, ASYNC_QUEUE | P2 | Email |
| NOSQL, other | P3 | Log |

### Producer-Consumer (Ingestion Pipeline)

`chan *Signal` decouples HTTP ingestion from persistence. Worker pool size = `NumCPU × 4` (configurable via `WORKER_COUNT`).

### Debounce (100-signal / 10-second window)

`sync.Map[componentID] → *DebounceWindow`. On each signal, if the window is active (< 10s, < 100 signals), the signal is appended to the existing work item. If expired or the threshold is hit, a new work item is created and the window resets. A janitor goroutine sweeps the map every second.

---

## Resilience Features

| Feature | Implementation |
|---|---|
| Retry w/ backoff | `resilience.Retry(ctx, 5, op)` — exponential + 20% jitter, max 5s |
| Circuit breaker | `gobreaker` — opens after 5 consecutive failures, half-opens after 30s |
| Rate limiting | Per-user token bucket — 429 before the channel is touched |
| Backpressure | Bounded channel + 503 shedding |
| Graceful shutdown | 30s drain window on SIGTERM |

---

## Non-functional Extras (Bonus Points)

- **Full JWT auth** with role-based access control (producer / responder / admin)
- **httpOnly cookie** for SSE auth (browser-friendly)
- **TimescaleDB hypertable** for per-minute signal aggregations
- **Throughput metrics** printed every 5s: `signals_in/s`, `signals_processed/s`, `queue_depth`
- **`/ready` endpoint** that checks all three datastores
- **Race-condition tests** (`go test -race`)
- **Docker multi-stage builds** — Go binary ~8MB, React via nginx:alpine

---

## Repository Structure

```
zeotap/
├── backend/
│   ├── cmd/server/main.go          # Entry point, wiring, graceful shutdown
│   ├── internal/
│   │   ├── models/                 # Domain types (WorkItem, Signal, RCA…)
│   │   ├── ingest/                 # HTTP handler, rate limiter, pipeline
│   │   ├── debounce/               # 10s/100-signal debounce window
│   │   ├── workflow/               # State pattern, RCA validation, MTTR
│   │   ├── alerting/               # Strategy pattern (P0-P3 alerters)
│   │   ├── auth/                   # JWT issuer, middleware, role guards
│   │   ├── store/{postgres,mongo,redis,timescale}/
│   │   ├── api/                    # REST handlers, SSE bridge
│   │   ├── resilience/             # Retry, circuit breaker
│   │   └── metrics/                # 5s throughput logger
│   ├── db/migrations/001_init.sql
│   ├── test/                       # Unit + concurrency tests
│   └── scripts/
│       ├── simulate_outage.go      # Cascading failure demo
│       └── load.js                 # k6 load test (10k rps)
├── frontend/
│   └── src/
│       ├── pages/{Login,LiveFeed,IncidentDetail,RcaForm}.tsx
│       ├── hooks/{useAuth,useSse}.ts
│       ├── api/client.ts
│       └── components/{SeverityBadge,StatusBadge,Toast}.tsx
├── samples/outage.json             # Replay-able failure event payload
├── docs/                           # ADRs and design notes
├── docker-compose.yml
└── README.md
```

---

## Prompts & Plans

All planning markdown and prompts used to design this system are checked in under:
- `docs/` — Architecture decision records
- `.claude/plans/` — Implementation plan used with Claude Code
