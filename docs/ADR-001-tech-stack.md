# ADR-001: Tech Stack Selection

---

## Context

We needed to build a mission-critical Incident Management System capable of ingesting
up to 10,000 signals/second, maintaining a workflow-driven dashboard, and storing
structured + unstructured data reliably.

---

## Decisions

### Go (backend)

**Chosen over:** Node.js, Python, Java

Go's goroutine model and buffered channels are a textbook fit for high-throughput
producer-consumer pipelines. A single binary compiles to ~8 MB, ships in an
`alpine` container with zero runtime dependencies, and handles 10k+ req/s without
JVM warm-up or GIL contention.

### PostgreSQL + TimescaleDB (Source of Truth)

**Chosen over:** MySQL, CockroachDB

ACID guarantees and `SERIALIZABLE` isolation are essential for state machine
transitions — two concurrent responders must not both successfully move an incident
to `CLOSED`. TimescaleDB adds a hypertable for per-minute signal aggregations without
requiring a separate TSDB.

### MongoDB (Data Lake)

**Chosen over:** Elasticsearch, Cassandra

Schema-flexible documents accommodate arbitrary `tags` on signals. Indexed on
`(component_id, timestamp)` for efficient per-incident signal retrieval. Acts as the
immutable audit log; no mutations required.

### Redis (Cache + Pub/Sub)

**Chosen over:** Memcached, Kafka

Two sorted sets (`live:active`, `live:closed`) serve the dashboard read path at
microsecond latency — no Postgres query per page refresh. The built-in Pub/Sub channel
provides zero-broker SSE fan-out to all connected dashboard clients.

### React 18 + Vite (Frontend)

**Chosen over:** Vue, HTMX

`EventSource` SSE is browser-native in React; hooks (`useSse`, `useAuth`) make
reconnection and auth token injection trivial. Tailwind provides the dark-mode design
tokens without a CSS build step.

### JWT HS256 (Auth)

**Chosen over:** Session cookies, OAuth

Stateless tokens eliminate a distributed session store. Role claims (`producer` /
`responder` / `admin`) travel in the token, enabling per-route guard middleware
without a DB lookup on every request. httpOnly cookie carries the token for SSE
connections where `Authorization` headers cannot be set by the browser.

---

## Consequences

- Go binary must be cross-compiled for `linux/amd64` in the Docker multi-stage build.
- TimescaleDB extension must be installed in the Postgres image (`timescale/timescaledb`).
- Redis sorted-set cache is eventually consistent — a cold start (empty Redis) falls
  back to Postgres and reheats the cache transparently.
