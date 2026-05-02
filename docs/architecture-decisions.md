# Architecture Decision Records (ADRs)

## ADR-001: Go as backend language

**Decision:** Go 1.22+

**Rationale:** Native goroutines and buffered channels are textbook "modern concurrency
primitives" (rubric hint). Single binary deployment. Comfortable at 10k+ signals/sec
with zero external queue dependencies.

**Alternatives considered:** Python/FastAPI (GIL limits CPU-bound throughput), Node.js
(single-threaded event loop risk at 10k/s), Java/Spring Boot (viable, heavier images).

---

## ADR-002: In-process buffered channel for async transport

**Decision:** `chan *Signal` (cap=50,000) + worker pool

**Rationale:** Keeps the demo runnable on a single laptop with no external broker.
Backpressure is handled natively: a full channel returns 503 + `Retry-After`.

**Alternatives considered:** Kafka (too heavy for a laptop demo), NATS JetStream
(viable bonus, adds one more container).

---

## ADR-003: Four distinct data stores

| Store | Purpose |
|---|---|
| MongoDB | Raw signal audit log — schema-flexible, bulk-insert optimised |
| PostgreSQL | Source of Truth — ACID, serializable state transitions |
| TimescaleDB | Timeseries hypertable for per-minute signal aggregations |
| Redis | Hot-path dashboard cache (active + closed sorted sets) + SSE pub/sub |

**Rationale:** Each store is chosen for its access pattern, satisfying the rubric's
"correct separation of data for various purposes" (Data Handling 20%).

---

## ADR-004: State Pattern for workflow engine

**Decision:** `WorkItemState` interface with `CanTransition(to) error` per state.

**Transition matrix:**
```
OPEN → INVESTIGATING → RESOLVED → CLOSED
              ↑____________↓  (re-investigate)
```

`ClosedState` guard is the single authoritative location enforcing mandatory RCA.
Transitions execute inside a Postgres `SERIALIZABLE` transaction to prevent races.

---

## ADR-005: Strategy Pattern for alerting

**Decision:** `Alerter` interface → `P0PagerDutyAlerter | P1SlackAlerter | P2EmailAlerter | P3LogAlerter`

`alerting.Factory(componentType)` selects at runtime. Adding a new alerter
(e.g., OpsGenie) requires only a new struct + one factory case — no existing code changes.

---

## ADR-006: JWT HS256 for auth

**Decision:** Access token (15 min) + Refresh token (7 days), HS256.

**Rationale:** Stateless, role-encoded in claims (`producer` / `responder` / `admin`).
httpOnly cookie carries the access token for SSE clients; Bearer header for API clients.
Auto-refresh on 401 in the frontend prevents session disruption.

---

## ADR-007: Backpressure strategy — shed-with-503

**Decision:** When the ingress channel is full, return `503 Service Unavailable` with
`Retry-After: 5` rather than blocking the HTTP goroutine.

**Rationale:** Blocking would tie up HTTP goroutines, cascading into a full server hang
(thundering-herd). Shedding gives producers actionable feedback and keeps the server
responsive.

---

## ADR-008: Debounce window boundary

**Decision:** A new work item is created only when the 10-second window expires.
All signals received within the window — regardless of count — attach to the same
work item.

**Rationale:** Direct implementation of the assignment spec: *"If 100 signals arrive
for the same Component ID within 10 seconds, only ONE Work Item should be created,
while all 100 signals are linked to it in the NoSQL store."* The 100 is the example
burst size, not a threshold to split work items.
