# ADR 0004 — The state machine is a pure function of (events, clock)

Status: accepted (Milestone 1)

## Context

§7 requires deterministic transitions. §22 requires multiple concurrent agent
sessions to be tracked independently and reduced to one visible pet state.

## Decision

Two layers.

**1. Per-session state.** Every session (keyed by `source` + `session_id`) has
its own small automaton: `idle → thinking → working → …`. Events only ever touch
their own session. A session also carries a *transient* state with an expiry —
`happy` after `task_completed`, `celebrate` after `tests_passed`, `confused`
after an error. Transients decay back to the session's base state.

**2. Reduction.** The visible pet state is the highest-priority state across all
live sessions, using the §22 order:

```
attention > worried > confused > celebrate > happy > heart > working > thinking > idle > sleeping
```

Sleep is applied last, as two rules with no exception inside either: no live
sessions means `sleeping`, and so does a reduction of `idle` that has stayed
idle for `sleeping_after`. A pending request reduces to `attention` rather than
`idle`, so it falls outside the second rule instead of being carved out of it.

**Determinism mechanics.** `Machine.Apply(now, event)` and `Machine.Tick(now)`
both take the current time as an argument. The machine holds no clock, no
goroutines and no randomness. Every test drives it with a synthetic clock, and
the same event sequence always produces the same state sequence.

`Machine` is not internally synchronised. The engine owns it and serialises all
access through a single goroutine reading from a channel.

## Consequences

- Timers are not `time.AfterFunc` callbacks; they are deadlines recomputed on
  each tick. The engine ticks once a second, which is also what decays
  transients and triggers the sleep transition.
- Adding a new event type means adding one case to `applyToSession`. The
  reduction and sleep layers do not change.
- Unknown event types are recorded (they refresh the activity clock) but do not
  transition state, satisfying §6's "tolerate unknown event types".
