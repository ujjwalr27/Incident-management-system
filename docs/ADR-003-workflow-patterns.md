# ADR-003: Workflow Engine — State & Strategy Patterns

**Date:** 2026-05-01  
**Status:** Accepted

---

## Context

Incidents follow a strict lifecycle and require different alerting behaviour
depending on the component type that failed. Both concerns must be extensible
without modifying core pipeline logic.

---

## Decision 1: State Pattern for Incident Lifecycle

The allowed transitions are encoded in a compile-time map:

```
OPEN → INVESTIGATING → RESOLVED → CLOSED
             ↑__________↓         (re-investigate allowed)
       ↑_____↓                    (back to OPEN allowed)
```

```go
var allowedMap = map[Status][]Status{
    StatusOpen:          {StatusInvestigating},
    StatusInvestigating: {StatusResolved, StatusOpen},
    StatusResolved:      {StatusClosed, StatusInvestigating},
    StatusClosed:        {},
}
```

`CanTransition(from, to)` is the single authoritative guard. Every state transition
is executed inside a **serializable Postgres transaction** (`FOR UPDATE` row lock)
to prevent two concurrent responders from racing to the same state.

`ValidateClose(rca)` enforces 5 mandatory RCA fields before allowing `CLOSED`:
category, fix_applied, prevention_steps, incident_start, incident_end. A missing
or incomplete RCA returns `422 Unprocessable Entity`.

### MTTR Calculation

`MTTR = rca.SubmittedAt − wi.FirstSignalAt` (seconds).

`SubmittedAt` is the timestamp when the RCA was filed (server-side `NOW()`), not the
operator-input `incident_end` field. This ensures MTTR reflects actual response time.

---

## Decision 2: Strategy Pattern for Alerting

```go
type Alerter interface {
    Priority() string
    Channel() string
    Send(ctx, wi) error
}
```

`alerting.Factory(componentType)` selects at runtime:

| Component | Severity | Alerter | Channel |
|---|---|---|---|
| RDBMS | P0 | P0Alerter | PagerDuty |
| MCP_HOST, API | P1 | P1Alerter | Slack |
| CACHE, ASYNC_QUEUE | P2 | P2Alerter | Email |
| NOSQL, other | P3 | P3Alerter | Log |

Adding a new component type requires only: (1) a new constant in `models.go`,
(2) a new `case` in `Factory()`. No changes to the pipeline.

---

## Consequences

- Serializable isolation adds ~1ms overhead per transition — acceptable since
  transitions are human-triggered, not high-frequency.
- The Strategy pattern means alerting is currently simulated (log lines). Replacing
  with real PagerDuty/Slack API calls requires only implementing `Send()` — the
  pipeline code is unchanged.
- RCA upsert (`ON CONFLICT DO UPDATE`) allows re-submission after a
  `RESOLVED → INVESTIGATING → RESOLVED` back-transition without a unique constraint
  violation.
