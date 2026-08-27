# Adapters

An adapter translates one external system's notion of "something happened" into
the internal event vocabulary (`docs/events.md`) and POSTs it to
`127.0.0.1:9876/event`. That is the whole contract.

Adapters do not import `internal/` and do not link against the engine. If
`petctl` can drive the pet, so can a shell script — which is exactly how the
Claude Code adapter will work, since hooks are shell commands.

## Built

### `claude/` — Milestone 2

```bash
petctl install claude              # .claude/settings.json in the current project
petctl install claude --local      # .claude/settings.local.json, this clone only
petctl install claude --global     # ~/.claude/settings.json, every project
petctl uninstall claude
```

The three differ only in which file they land in — Claude Code merges all of
them. `--local` exists because the hook holds an absolute path to petctl on
*this* machine: a project that tracks its `settings.json` wants that path in the
file git ignores, not in the one everybody clones.

Install patches the settings file in place: it preserves every other setting and
every hook the user wrote, keeps a `.bak` of what it replaced, writes
atomically, and refuses outright to touch a file it could not parse. Removal
prunes the groups it added and leaves a file identical to the one it started
from. It is idempotent, and re-running it after moving the binary repoints the
hooks rather than adding a second set. `petctl doctor` reports how many of the
hooks are present in each scope, so a hand-edited half-install looks wrong.

The hooks run `petctl hook claude`, which reads the payload on stdin and POSTs
one event. It exits 0 no matter what happens — bad JSON, no petd, a hook this
build has never heard of — and writes nothing to stdout, because a non-zero
exit or a stray `{` would interfere with the agent it is attached to.

| Hook | Event |
|---|---|
| `SessionStart` | `session_started` |
| `UserPromptSubmit` | `thinking_started` |
| `PreToolUse` | `tool_started`, with `metadata.tool` |
| `PostToolUse` | `tool_finished` |
| `PostToolUseFailure` | `tool_failed` |
| `PermissionRequest` | `permission_requested` |
| `Notification` | `user_input_requested` |
| `Stop` | `task_completed` |
| `StopFailure` | `error` |
| `SessionEnd` | `session_ended`, but only when its `reason` says Claude Code is leaving — otherwise `idle` |

`SessionEnd` fires for two quite different things. Quitting Claude Code reports
`prompt_input_exit` or `logout`; rewinding, `/clear` and resuming another
session report something else while the agent carries on running. Treating them
alike greyed the pet out with Claude Code still open. An unrecognised reason
counts as "still running", which is the safer way to be wrong: a pet left in
colour for an agent that has quit still falls asleep a minute later, whereas a
pet greyed out for one that is running is simply lying.

Every other row is something Claude Code reports outright. Nothing is inferred: in
particular `PostToolUse` fires only on success and failures arrive as their own
`PostToolUseFailure`, so the pet never has to guess how a tool call went. The
tool *name* is the only payload that travels — never a command line, a prompt,
a path or a diff (§26).

There is deliberately no test detection. Claude Code reports no test result, and
pattern-matching command lines to guess at one is exactly the cry-wolf failure
described below. A failing test run still reaches the pet honestly, as the
`tool_failed` of the command that ran it.

## Planned

| Directory | Milestone | Shape |
|---|---|---|
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
