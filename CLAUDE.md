# Working on this project

A macOS desktop pet that reacts to what a coding agent is doing. Claude Code
fires hooks → `petctl hook claude` POSTs an event → a state machine reduces
events to one visible state → a transparent Wails window draws a pixel-art cat.

Read [README.md](README.md) first for what it does. This file is about
working on it.

## Where it stands

Milestones 1 and 2 are in: the engine, the loopback event API, `petctl`, the
window, the menu-bar item, and the Claude Code adapter. One character ships
(SanMao, `momo`), though everything for more is still here — see the note in
the README's Characters section.

Next, per [ADR 0006](docs/adr/0006-milestone-1-boundaries.md): the Codex
adapter (Milestone 3), a git `post-commit` adapter, and a durable SQLite store
for statistics (Milestone 4, currently in-memory only).

## Running it

```bash
go mod tidy                       # once
make build && open build/bin/agent-pet.app
go build -o bin/petctl ./cmd/petctl
bin/petctl doctor                 # says more than you expect
```

Requires the Wails CLI **v2.10.2 or newer**; see the README for why.

## Three test suites, and what each needs

```bash
make test          # Go. Engine, state machine, event API, adapter, placement.
make test-ui       # Opens a browser. Runs against the real ui/dist/index.html.
make test-desktop  # Needs the app running. Checks the menu bar and window placement.
```

Run all three before committing anything that touches the window or the menu
bar. The Go suite alone will not catch a crash in a menu handler — one shipped
that way.

`make test-ui` serves the repo and opens `ui/test/index.html`, which loads the
real UI into an iframe per test. `make test-desktop` drives a running app
through `POST /window` — the only way to check a menu is not clipped in the
corner of a screen, since nothing may read a macOS menu bar without
accessibility access, which is refused here.

## Layout

```
main.go, app.go        the Wails shell — the only files importing Wails
statusitem_darwin.*    the menu-bar item, written against AppKit in cgo
internal/state/        the state machine: pure, no clock, no goroutines
internal/engine/       event intake, timers, subscribers
internal/server/       the loopback event API
internal/{events,petassets,bubble,config}/
adapters/claude/       hook payload → event, and the settings.json patcher
cmd/petctl/            the CLI. Shares no code with the engine, on purpose.
tools/genpets/         the sprite generator. All art is generated.
ui/dist/index.html     the whole frontend, one file
ui/test/               its tests
scripts/desktop-test.sh
```

Decisions live in `docs/adr/`. The commit messages carry the reasoning for
most of what is here and are worth reading before changing something that
looks arbitrary — much of it is load-bearing.

## Things that will waste your time

Each of these cost an hour or more to find.

- **A stale `petd` holds port 9876.** A new instance exits on startup and
  `petctl` keeps talking to the old one, so your rebuild appears to do
  nothing. `pkill -9 -f 'MacOS/petd'` before relaunching.
- **A failed `make build` leaves the previous `.app` in place.** A green test
  run after a failed build tested the old binary and means nothing. Check the
  build said `Built '...'` before believing a result.
- **`config.yaml` is rewritten from memory on shutdown.** Editing it while the
  app runs loses the edit. Close the app first.
- **Changing a default in `config.go` does not touch an existing
  `config.yaml`.** Your machine keeps the old value; test on a fresh config or
  edit both.
- **The pre-rename directories only move if the new name is free.**
  `config.MigrateLegacy` renames `digital-pet` to `agent-pet` and never merges,
  so a machine where anything created `~/.local/share/agent-pet` first keeps a
  stale `digital-pet` beside it, packs and all. Check both before concluding a
  pack is not being loaded.
- **The UI test runner and its iframes are cache-busted deliberately.** Without
  it the browser serves a stale `index.html` and the suite passes against a
  file no longer on disk. It did exactly that, twice.
- **`make test-desktop` is stateful.** Sections set the character and
  visibility they expect first, because asserting on what the last run left
  behind passes in sequence and fails alone.
- **cgo compiles Objective-C without ARC.** `-fobjc-arc` in
  `statusitem_darwin.go` is load-bearing: without it the status item is
  autoreleased out from under its static and the next message crashes the app.
- **Never call the Wails runtime from a native menu callback.** Those run on
  the main thread, and emitting an event ends in `evaluateJavaScript`, which
  segfaults from there. `goStatusClick` hands off to a goroutine for this
  reason.
- **Wails window coordinates are relative to the screen's `visibleFrame`,**
  not the display — origin already past the menu bar and a left Dock. Using
  the display size as a bound lets a window hang off by exactly the Dock's
  width.
- **The hooks are installed in this repo's `.claude/settings.json`,** so your
  own Claude Code session drives the pet while you work. A session in
  `petctl status` you did not create is probably you.

## Conventions

- Comments explain **why**, not what. If a line looks odd, the comment should
  say what breaks without it.
- A new test must be shown to fail. Break the thing it covers, watch it go
  red, put it back. Several tests here passed against deliberately broken code
  until that was done.
- The pet never guesses. If Claude Code does not report something, the pet does
  not infer it — see the "degrading honestly" note in
  [adapters/README.md](adapters/README.md).
- Nothing an agent controls becomes markup, a path, or a command. §26 of
  [the requirements](docs/prd.md); there are tests asserting it.
