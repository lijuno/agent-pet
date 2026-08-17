# The event vocabulary

Every adapter translates its source's notion of "something happened" into one of
these. The engine knows nothing about Claude Code, Codex or git — only about
this list.

## Shape

```json
{
  "source": "claude",
  "event": "tool_started",
  "session_id": "abc123",
  "timestamp": "2026-08-16T12:00:00+08:00",
  "metadata": { "tool": "bash" }
}
```

| Field | Required | Notes |
|---|---|---|
| `source` | yes | `claude`, `codex`, `git`, `tests`, anything. Defaults to `unknown`. |
| `event` | yes | One of the names below, or anything else — see *Unknown events*. |
| `session_id` | no | Sessions are tracked independently. Defaults to `default`, so an adapter that cannot report one still works. |
| `timestamp` | no | RFC3339. Defaults to now; implausible values are clamped to now. |
| `metadata` | no | Flat string map. Numbers and booleans are stringified; nested objects are dropped. |

Metadata is capped at 16 keys, 32-byte keys and 256-byte values, and stripped of
control characters. It is never executed, never interpolated into a shell
command, and never rendered as HTML.

## Events and what they do

| Event | Effect on the session |
|---|---|
| `session_started` | Opens the session, state `idle`. |
| `session_ended` | Closes it. Any reaction in flight still plays out first. |
| `thinking_started` | `thinking` |
| `working` | `working` |
| `idle` | `idle`, clears the running-tool count |
| `tool_started` | `working`; increments running tools. `metadata.tool` is shown in the status panel. |
| `tool_finished` | Decrements. At zero, back to `thinking` — the agent is reasoning about the result. |
| `tool_failed` | Decrements, and a `confused` reaction. |
| `permission_requested` | `attention` — the highest-priority state. |
| `user_input_requested` | `attention` |
| `task_completed` | `happy` for a few seconds, then `idle`. |
| `task_failed` | `confused` |
| `tests_started` | `working` |
| `tests_passed` | `celebrate` |
| `tests_failed` | `confused`, escalating to `worried` on a streak. |
| `git_commit` | `happy` |
| `error` | `confused`, escalating to `worried`. |
| `heartbeat` | Refreshes the activity clock only. Deliberately no transition: "still alive" is not "still working". |
| `pet_interaction` | `heart`. Sent by the UI, not by an agent. |

Three consecutive failures in one session (configurable via
`thresholds.worried_after`) turn `confused` into `worried`. Any success resets
the streak.

## Unknown events

An unrecognised event name is **accepted, not rejected**. It counts as activity —
something is clearly happening — but it drives no transition, because its
meaning is unknown. The response says so:

```json
{ "accepted": true, "known": false, "state": "working" }
```

This is what lets a newer adapter talk to an older `petd` without breaking the
pet.

## Multiple sessions

Each `source` + `session_id` pair gets its own automaton. The visible state is
the highest-priority one across all of them:

```
attention > worried > confused > celebrate > happy > heart > working > thinking > idle > sleeping
```

So if Claude session A is working and session B asks for permission, the pet
asks for attention. When B is answered, the pet goes back to working.

Sessions are forgotten after `thresholds.session_stale` (default 2h) without an
event, so a crashed agent cannot leak one.

## Time-based transitions

| Condition | Result |
|---|---|
| A `working`/`thinking` session goes quiet for `idle_after` (30s) | that session reads as `idle` |
| No sessions at all | `sleeping`, at once |
| Every session idle and no events for `sleeping_after` (60s) | `sleeping` |
| An unanswered `attention` for `attention_timeout` (10m) | gives up, and then sleeps like anything else |

The sleep rule applies to `idle` and nothing else, so a pending `attention` is
never slept through — see [ADR 0007](adr/0007-what-the-states-mean.md).

