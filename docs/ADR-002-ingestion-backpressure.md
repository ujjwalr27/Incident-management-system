# ADR-002: Ingestion Pipeline & Backpressure Strategy

---

## Context

The ingestion path must absorb bursts of 10,000 signals/second without crashing
the HTTP server when the persistence layer (Postgres, Mongo) is temporarily slow.

---

## Decision: Bounded In-Process Channel as Primary Backpressure Boundary

```
HTTP Handler → [rate limiter] → chan *Signal (cap=50,000) → Worker Pool (NumCPU×4)
                                       ↑
                                backpressure point
```

### Why a channel, not a queue (Kafka/RabbitMQ)?

An in-process channel eliminates network round-trips on the hot ingest path
(microseconds vs. milliseconds). At 10k signals/s the channel drains in <5s even
if all workers stall. For a single-node deployment, this is simpler and faster.

If the system grows to multi-node, the channel can be replaced with a Kafka topic
with no changes to the worker logic — the `Pipeline.Submit()` interface stays the same.

### Non-blocking submit

```go
select {
case p.ch <- sig:
    // accepted
default:
    atomic.AddInt64(&p.dropped, 1)
    // dropped — caller gets accepted=0 in this slot
}
```

A `default` branch means HTTP goroutines never block waiting for a slow worker.
When the channel is full the handler returns `503 + Retry-After: 5` — producers
can retry the dropped subset.

### Rate Limiter (outer guard)

`golang.org/x/time/rate` token bucket per user ID (fallback: IP). Set to
`RATE_LIMIT_RPS=20000 / RATE_LIMIT_BURST=5000` by default. Returns `429` before
signals even reach the channel, protecting the buffer from single-source floods.

### Debounce Window (100 signals / 10 seconds)

`sync.Map[componentID] → *Window`. First signal in a window creates a new Work Item
(`IsNew=true`). Subsequent signals within the 10s window are linked to the same
Work Item (`IsNew=false`). A background janitor sweeps expired windows every second
to prevent memory growth. Thread-safe via per-window mutex.

---

## Consequences

- Max in-flight signals in memory: 50,000 × ~200 bytes ≈ 10 MB.
- A 30s SIGTERM drain window lets the channel empty before shutdown.
- Metrics goroutine prints `queue_depth` every 5s — operators can see backpressure
  building before it becomes a problem.
