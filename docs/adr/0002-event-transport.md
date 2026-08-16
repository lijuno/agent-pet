# ADR 0002 — Event transport is loopback HTTP + JSON

Status: accepted (Milestone 1)

## Context

Adapters (Claude Code hooks, Codex notifications, git hooks, `petctl`) need to
push events into `petd`. §25 suggests `POST http://127.0.0.1:9876/event`.
Alternatives were a Unix domain socket or a named pipe.

## Decision

Loopback HTTP with JSON bodies, listening on `127.0.0.1:9876` only.

Reasons:

- Claude Code hooks are shell commands. `curl` is universally available;
  writing to a Unix socket from a hook is awkward.
- Trivially testable with `curl` and `petctl`.
- Same surface serves the SSE `/stream` endpoint for future UIs.

Hardening applied (§26):

- Listener is bound to `127.0.0.1`, never `0.0.0.0`. The bind address is
  configurable but rejects non-loopback addresses unless
  `security.allow_non_loopback` is explicitly set.
- Request bodies are capped at 16 KiB.
- Only the known fields of the event schema are decoded; unknown JSON fields are
  rejected.
- Metadata is coerced to strings, capped at 16 keys and 256 bytes per value, and
  stripped of control characters.
- Nothing from an event is ever executed, shelled out, or rendered as HTML. The
  frontend writes all agent-supplied text with `textContent`.

## Consequences

- Any local process can post events. This is the same trust boundary as any
  loopback dev server. An optional shared token can be added later via
  `security.token` without changing the transport.
- Unix socket support can be added as a second listener later; the handler layer
  is transport-agnostic.
