# Adapters

An adapter translates one external system's notion of "something happened" into
the internal event vocabulary (`docs/events.md`) and POSTs it to
`127.0.0.1:9876/event`. That is the whole contract.

Adapters do not import `internal/` and do not link against the engine. If
`petctl` can drive the pet, so can a shell script — which is exactly how the
Claude Code adapter will work, since hooks are shell commands.

## Planned

| Directory | Milestone | Shape |
|---|---|---|
| `claude/` | 2 | A hook payload → event translator, plus a `settings.json` patcher for `petctl install claude`. Must inspect the existing configuration, preserve existing hooks, add only its own, and support clean removal (§28). |
| `codex/` | 3 | Codex exposes fewer lifecycle events. The adapter infers state conservatively and reports what it genuinely cannot observe rather than faking it (§29). |
| `git/` | secondary | A `post-commit` hook emitting `git_commit`. |

## Writing one now

Nothing stops you. Any process that can make an HTTP request can drive the pet:

```bash
# a git post-commit hook
#!/bin/sh
curl -sS -m 1 -X POST http://127.0.0.1:9876/event \
  -H 'content-type: application/json' \
  -d '{"source":"git","event":"git_commit"}' >/dev/null 2>&1 || true
```

Two rules worth copying from that snippet: a short timeout, and never failing
the host command if the pet is not running. An adapter that blocks a commit
because a cartoon cat is offline is a bad adapter.

## Degrading honestly

If a source cannot report something, do not invent it. A Codex adapter that
guesses at `permission_requested` because a prompt *might* be showing produces a
pet that cries wolf, and the user stops trusting the one signal that matters
most. `petctl doctor` is expected to say `permission events unavailable` — that
is a correct answer, not a failure.
