# Digital Pet

A small desktop companion that reacts to what your coding agent is doing.
Claude Code starts, the pet wakes up. A tool runs, it gets to work. A permission
prompt appears, it turns and asks for you. Tests pass, it celebrates.

**This repository contains Milestones 1 and 2** — the pet engine, the local
event API, `petctl`, the desktop window, three built-in pixel-art characters,
and the Claude Code adapter. The Codex adapter is Milestone 3;
[`docs/adr/0006-milestone-1-boundaries.md`](docs/adr/0006-milestone-1-boundaries.md)
records which seam each deferred piece attaches to.

```
   agent events  ──▶  local state machine  ──▶  expressive character
```

## Requirements

- macOS (the architecture allows Windows and Linux; only macOS is exercised)
- Go 1.23+
- [Wails CLI v2](https://wails.io/docs/gettingstarted/installation)
- Python 3 with Pillow, only if you want to regenerate the sprite art

## Quick start

```bash
go mod tidy          # resolves Wails and yaml; needed once
wails dev            # runs the pet with hot reload
```

In another terminal:

```bash
go build -o bin/petctl ./cmd/petctl

bin/petctl doctor
bin/petctl test celebrate --for 8s
bin/petctl event claude permission_requested
bin/petctl watch
```

A release build:

```bash
make build           # produces build/bin/digital-pet.app
```

## Connecting Claude Code

```bash
petctl install claude       # this project;  --global for every project
```

Then start a **new** Claude Code session — hooks are read at startup, so the
session you are in now will not pick them up. `petctl watch` shows the events
arriving. `petctl doctor` says whether the hooks are installed, and
`petctl uninstall claude` removes them again.

See [`adapters/README.md`](adapters/README.md) for the hook-to-event table and
what the adapter deliberately does not try to detect.

## Trying every state

To see the whole character without waiting for an agent to do something:

```bash
for s in idle thinking working attention confused worried happy celebrate sleeping tired heart; do
  petctl test $s --for 2s; sleep 2
done
```

Or drive a realistic session by hand:

```bash
petctl event claude session_started --session demo
petctl event claude thinking_started --session demo
petctl event claude tool_started --session demo --meta tool=bash
petctl event claude permission_requested --session demo
petctl event claude tool_started --session demo --meta tool=edit
petctl event claude task_completed --session demo
petctl event claude tests_passed --session demo
```

## Interacting with the pet

| Action | Result |
|---|---|
| Drag | Move the pet. Its position is remembered. |
| Click | Status panel: what each session is doing. |
| Double-click | Pet it. |
| Right-click | Menu: status, statistics, change pet, always on top, mute, sleep, quit. |
| Menu bar | The same commands under the `Pet` menu while the app is frontmost. |

The pet also lives in the macOS menu bar. Its icon shows the current state and
carries the same commands, so the pet can be reached even when it is behind
something. See [ADR 0005](docs/adr/0005-macos-menu-and-tray.md).

The character comes in three sizes — small, medium and large — from the
right-click menu or the `pet.scale` setting.

## Configuration

`~/.config/digital-pet/config.yaml` is written on first run.

```yaml
pet:
  active: momo          # momo | byte | pip, or your own pack
  always_on_top: true
  scale: 1.0

behavior:
  dialogue: true        # speech bubbles

personality:
  name: SanMao
  personality: gentle   # gentle cheerful calm mischievous sarcastic energetic

thresholds:
  idle_after: 30s       # a quiet agent stops looking busy
  sleeping_after: 30m
  tired_after: 2h

server:
  addr: 127.0.0.1:9876
```

Anything you leave out keeps its default. A malformed file logs a warning and
falls back to defaults rather than refusing to start.

## The event API

Any local process can drive the pet:

```bash
curl -sS -X POST http://127.0.0.1:9876/event \
  -H 'content-type: application/json' \
  -d '{"source":"claude","event":"tests_passed","session_id":"abc"}'
```

| Endpoint | Purpose |
|---|---|
| `POST /event` | Submit an event |
| `GET /state` | Current snapshot |
| `GET /stream` | Server-sent events, one per state change |
| `POST /test` | Force a state (`petctl test`) |
| `GET /diagnostics` | Everything `petctl doctor` prints |
| `GET /pets`, `POST /pet` | List and switch characters |

The listener binds `127.0.0.1` and refuses anything else unless you explicitly
set `server.allow_non_loopback`. See [`docs/events.md`](docs/events.md) for the
full event vocabulary and [ADR 0002](docs/adr/0002-event-transport.md) for the
hardening applied to untrusted input.

## Characters

Three built-in packs ship in the binary:

| id | | |
|---|---|---|
| `momo` | SanMao, a tortoiseshell tabby | white bib, white socks, green eyes — the default |
| `byte` | a terminal robot | screen face, blinking antenna |
| `pip` | a slime | very squishy |

Each provides all eleven states as 40×40 pixel-art sprite strips. Regenerate
them with:

```bash
make pets     # python3 tools/genpets/genpets.py
```

Your own packs go in `~/.local/share/digital-pet/pets/<id>/` and are picked up
at startup. A pack with the same id as a built-in replaces it. The format is one
PNG strip per state plus a `manifest.json`; see [`docs/pets.md`](docs/pets.md).

## Privacy

- No prompts, no source code, no command arguments are recorded. Default logs
  contain event categories only; `logging.verbose: true` adds tool names.
- No network client of any kind. The process listens on loopback and calls
  nothing out.
- Character packs are built from local images. Nothing is uploaded, and there is
  no image-generation service to opt into.
- No accessibility, screen-recording, camera, microphone or keyboard-monitoring
  permission is requested, and no elevated privileges are needed.

## Layout

```
main.go, app.go        Wails shell — the only files that import Wails
internal/events/       the internal event vocabulary and its sanitiser
internal/state/        the deterministic state machine
internal/engine/       event intake, timers, subscribers
internal/server/       the loopback event API
internal/petassets/    pet pack loading
internal/bubble/       template speech
internal/config/       config.yaml
cmd/petctl/            the CLI (no shared code with the engine)
adapters/claude/       the Claude Code hook adapter
adapters/              Codex and git adapters — Milestone 3 and later
tools/genpets/         the sprite generator
ui/dist/               the frontend, embedded in the binary
ui/test/               its tests — open in a browser, no build step
docs/adr/              architectural decisions
```

## Tests

```bash
make test      # Go: engine, state machine, event API, adapter
make test-ui   # the window, in a browser
```

The state machine is a pure function of (events, clock), so its tests drive a
synthetic clock and never sleep. See
[ADR 0004](docs/adr/0004-deterministic-state-machine.md).

`make test-ui` serves the repository and opens
[`ui/test/index.html`](ui/test/index.html), which loads the real
`ui/dist/index.html` into a fresh iframe for each test and reports pass/fail on
the page. There is no runner to install and no build step, because the UI is one
static file and its tests should cost the same to run. They cover sprite
rendering and timing, the panels and menu, click versus double-click, the
layout invariant that an open panel never covers the pet, and — most
importantly — that nothing an agent controls can become markup (§26).
