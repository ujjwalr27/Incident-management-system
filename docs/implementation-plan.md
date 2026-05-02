# Bug-Fix Plan — IMS Code Review Follow-up

## Context

The system is built and demoable end-to-end (login, ingest, debounce, state machine,
RCA, SSE, MTTR display). A code review against the rubric surfaced **2 critical bugs**
and **3 rubric-impacting issues**. This plan applies all five fixes, targeted at the
files listed below — no architecture changes, only surgical edits.

The previous (architecture) plan is preserved in `docs/architecture-plan.md` (move
once approved).

---

## Fix 1 — Signal-count race (CRITICAL, breaks the demo)

**Symptom:** A burst of 100 signals for `MCP_HOST_01` produces a work item with
`signal_count = 1`. Worker B's `UPDATE` runs before Worker A's `INSERT` lands, hits
**0 rows affected**, returns no error → 99 signals silently lost.

**Root cause:** `IncrementSignalCount` (`internal/store/postgres/store.go`) ignores
`RowsAffected`.

**Fix:**

```go
// internal/store/postgres/store.go
func (s *Store) IncrementSignalCount(ctx context.Context, id uuid.UUID, ts time.Time) error {
    return resilience.Retry(ctx, 8, func() error {  // 8 attempts (~2s ceiling)
        result, err := s.db.ExecContext(ctx, `
            UPDATE work_items
            SET signal_count = signal_count + 1, last_signal_at = $2, updated_at = NOW()
            WHERE id = $1`, id, ts)
        if err != nil {
            return err
        }
        rows, _ := result.RowsAffected()
        if rows == 0 {
            return fmt.Errorf("work item %s not yet visible — retrying", id)
        }
        return nil
    })
}
```

The existing `resilience.Retry` (exponential 100ms→5s + jitter) handles the race
window automatically. Worker A's INSERT typically lands well within the first retry.

**Files:** `internal/store/postgres/store.go` (single function).

---

## Fix 2 — MTTR uses wrong field

**Symptom:** Assignment requires *MTTR = RCA submission time − first signal time*.
Current code uses `rca.IncidentEnd` (operator-input field on the RCA form), so MTTR
reflects when the operator *says* the incident ended, not when they actually
documented it.

**Fix:**

```go
// internal/workflow/state.go
func MTTR(wi *models.WorkItem, rca *models.RCA) float64 {
    if rca == nil {
        return 0
    }
    return rca.SubmittedAt.Sub(wi.FirstSignalAt).Seconds()
}
```

Update the existing `TestMTTR` in `test/workflow_test.go` to use `SubmittedAt`.

**Files:** `internal/workflow/state.go`, `test/workflow_test.go`.

---

## Fix 3 — Restore Redis hot-path cache for the dashboard list

**Symptom:** Earlier we made `ListIncidents` always hit Postgres so closed
incidents would appear. That regressed the rubric line *"Cache (Hot-Path):
Real-time Dashboard State to avoid querying the Source of Truth for every UI
refresh"* (Data Handling 20%).

**Fix:** Two zsets in Redis — `live:active` and `live:closed`, both keyed by
last_signal_at as score so newest is on top within each section.

```go
// internal/store/redis/store.go — add a closed-feed key + dual update.
const (
    liveFeedKey   = "live:active"
    closedFeedKey = "live:closed"
    incidentPrefix = "incident:"
    sseChannel    = "sse:events"
)

func (s *Store) UpsertIncident(ctx context.Context, wi *models.WorkItem) error {
    data, _ := json.Marshal(wi)
    pipe := s.client.Pipeline()
    pipe.Set(ctx, incidentPrefix+wi.ID.String(), data, 24*time.Hour)

    if wi.Status == models.StatusClosed {
        pipe.ZRem(ctx, liveFeedKey, wi.ID.String())
        // Score by last_signal_at so newest closures rank first.
        pipe.ZAdd(ctx, closedFeedKey, redis.Z{
            Score:  -float64(wi.LastSignalAt.Unix()),
            Member: wi.ID.String(),
        })
    } else {
        pipe.ZRem(ctx, closedFeedKey, wi.ID.String())
        pipe.ZAdd(ctx, liveFeedKey, redis.Z{
            Score:  severityScore(wi.Severity),
            Member: wi.ID.String(),
        })
    }
    _, err := pipe.Exec(ctx)
    return err
}

// New: return both sets in one call.
func (s *Store) GetAllIncidents(ctx context.Context, limit int) ([]*models.WorkItem, error) {
    activeIDs, _ := s.client.ZRange(ctx, liveFeedKey, 0, int64(limit-1)).Result()
    closedIDs, _ := s.client.ZRange(ctx, closedFeedKey, 0, int64(limit-1)).Result()
    return s.hydrate(ctx, append(activeIDs, closedIDs...))
}
```

```go
// internal/api/rest.go — read-through cache pattern.
func (h *Handler) ListIncidents(w http.ResponseWriter, r *http.Request) {
    limit := queryInt(r, "limit", 100)
    if items, err := h.rds.GetAllIncidents(r.Context(), limit); err == nil && len(items) > 0 {
        jsonOK(w, items)
        return
    }
    // Cold cache → Postgres + reheat.
    items, err := h.pg.ListWorkItems(r.Context(), limit, queryInt(r, "offset", 0))
    if err != nil { jsonError(w, "database error", 500); return }
    for _, wi := range items {
        _ = h.rds.UpsertIncident(r.Context(), wi)
    }
    if items == nil { items = []*models.WorkItem{} }
    jsonOK(w, items)
}
```

**Files:** `internal/store/redis/store.go`, `internal/api/rest.go`.

---

## Fix 4 — `/auth/refresh` endpoint

**Symptom:** Access tokens expire after 15min and the refresh token is unused —
users get logged out mid-session.

**Fix:** Add an endpoint that verifies a refresh token (must have `aud=refresh`)
and issues a fresh token pair.

```go
// internal/auth/jwt.go — verify with audience check.
func (i *Issuer) VerifyRefresh(tokenStr string) (*Claims, error) {
    claims, err := i.Verify(tokenStr)
    if err != nil {
        return nil, err
    }
    for _, a := range claims.Audience {
        if a == "refresh" {
            return claims, nil
        }
    }
    return nil, ErrInvalidToken
}
```

```go
// internal/api/rest.go — new public handler.
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
    var req struct{ RefreshToken string `json:"refresh_token"` }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        jsonError(w, "invalid request body", 400); return
    }
    claims, err := h.issuer.VerifyRefresh(req.RefreshToken)
    if err != nil {
        jsonError(w, "invalid refresh token", 401); return
    }
    uid, _ := uuid.Parse(claims.UserID)
    pair, err := h.issuer.Issue(uid, claims.Role)
    if err != nil {
        jsonError(w, "token generation failed", 500); return
    }
    jsonOK(w, pair)
}
```

```go
// cmd/server/main.go — register route alongside /auth/login.
r.Post("/auth/refresh", h.Refresh)
```

```ts
// frontend/src/api/client.ts — auto-refresh on 401.
// In the existing fetch interceptor, before redirecting to /login,
// try POST /auth/refresh with the stored refresh_token and retry once.
```

**Files:** `internal/auth/jwt.go`, `internal/api/rest.go`, `cmd/server/main.go`,
`frontend/src/api/client.ts`, `frontend/src/App.tsx` (store refresh_token).

---

## Fix 5 — Bounded rate-limiter map

**Symptom:** `internal/ingest/ratelimit.go` keeps a `map[string]*rate.Limiter` with no
eviction. Each unique authenticated user adds an entry forever → memory leak in
long-running production deployments.

**Fix:** Track `lastSeen` per limiter and run a janitor goroutine that purges
entries idle > 30 min.

```go
// internal/ingest/ratelimit.go
type entry struct {
    limiter  *rate.Limiter
    lastSeen time.Time
}

type perUserLimiter struct {
    mu       sync.Mutex
    entries  map[string]*entry
    rps      rate.Limit
    burst    int
}

func newPerUserLimiter(rps float64, burst int) *perUserLimiter {
    p := &perUserLimiter{
        entries: make(map[string]*entry),
        rps:     rate.Limit(rps),
        burst:   burst,
    }
    go p.janitor()
    return p
}

func (p *perUserLimiter) get(key string) *rate.Limiter {
    p.mu.Lock()
    defer p.mu.Unlock()
    if e, ok := p.entries[key]; ok {
        e.lastSeen = time.Now()
        return e.limiter
    }
    e := &entry{limiter: rate.NewLimiter(p.rps, p.burst), lastSeen: time.Now()}
    p.entries[key] = e
    return e.limiter
}

func (p *perUserLimiter) janitor() {
    t := time.NewTicker(5 * time.Minute)
    defer t.Stop()
    for now := range t.C {
        p.mu.Lock()
        for k, e := range p.entries {
            if now.Sub(e.lastSeen) > 30*time.Minute {
                delete(p.entries, k)
            }
        }
        p.mu.Unlock()
    }
}
```

**Files:** `internal/ingest/ratelimit.go` only.

---

## New Tests (proves fix 1 works + fills gaps)

```go
// test/pipeline_race_test.go — NEW
// Spins up the real Pipeline against in-memory mocks of pg/mongo/redis stores,
// fires 200 signals for one component_id concurrently, asserts:
//   - exactly 1 work item created
//   - signal_count == 200 (this is the fix-1 regression test)
```

```go
// test/alerting_test.go — NEW
// Table-driven: each ComponentType → expected Alerter type (Factory mapping).
// Asserts SeverityFor and Alerter.Priority/Channel for all six component types.
```

```go
// test/ratelimit_test.go — NEW
// 1) Asserts 429 when burst exhausted.
// 2) Asserts limiter recovers after refill.
// 3) Asserts janitor purges stale entries (with synthetic clock).
```

Update `test/workflow_test.go::TestMTTR` to assert `SubmittedAt - FirstSignalAt`.

---

## Verification

After all fixes are in:

```bash
# 1. Backend rebuilds clean
cd backend && go build ./...

# 2. All tests pass (including new race test)
go test ./test/... -v

# 3. End-to-end: rebuild containers, replay simulation, eyeball the dashboard
docker compose up --build -d
go run scripts/simulate_outage.go --addr http://localhost:8080
```

Pass criteria on the dashboard:
- `MCP_HOST_01` shows **100 signals**, not 1
- Submitting an RCA with arbitrary `incident_end` doesn't change MTTR — only
  submission time matters
- Closed incidents appear in the "Closed" section, served from Redis (verify by
  flushing Postgres mid-flight: `docker exec ims_postgres psql -U ims -c
  "DELETE FROM work_items"` — feed should remain populated until cache TTL)
- Tokens auto-refresh: leave the tab open >15min, no forced re-login
- `pprof` heap profile: rate-limiter map size flat over time

---

## Files Modified Summary

| # | File | Change |
|---|---|---|
| 1 | `internal/store/postgres/store.go` | `IncrementSignalCount` checks `RowsAffected` |
| 2 | `internal/workflow/state.go` | MTTR uses `SubmittedAt` |
| 2 | `test/workflow_test.go` | Update `TestMTTR` expectation |
| 3 | `internal/store/redis/store.go` | Add `closedFeedKey`, `GetAllIncidents` |
| 3 | `internal/api/rest.go` | `ListIncidents` is read-through cache |
| 4 | `internal/auth/jwt.go` | Add `VerifyRefresh` |
| 4 | `internal/api/rest.go` | Add `Refresh` handler |
| 4 | `cmd/server/main.go` | Register `/auth/refresh` |
| 4 | `frontend/src/api/client.ts` | Auto-refresh on 401 |
| 4 | `frontend/src/App.tsx` | Store + use refresh_token |
| 5 | `internal/ingest/ratelimit.go` | Bounded map + janitor |
| Tests | `test/pipeline_race_test.go` | NEW |
| Tests | `test/alerting_test.go` | NEW |
| Tests | `test/ratelimit_test.go` | NEW |

13 file edits across 5 surgical fixes, no architecture changes.
