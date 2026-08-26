# Working on this project

A macOS desktop pet that reacts to what a coding agent is doing. Claude Code
fires hooks → `petctl hook claude` POSTs an event → a state machine reduces
events to one visible state → a transparent Wails window draws a pixel-art
character.

Read [README.md](README.md) first for what it does. This file is about
working on it.

## Where it stands

Milestones 1 and 2 are in: the engine, the loopback event API, `petctl`, the
window, the menu-bar item, and the Claude Code adapter. Six characters ship —
Sanmao (`sanmao`), Peach (`peach`), Juanmao (`juanmao`), Maomao (`maomao`),
Damao (`damao`) and Amiao (`amiao`) — and a seventh stays defined but unshipped
in `tools/genpets` as the worked example; see the README's Characters
section.

Over-the-air updates are in too. Three things about them are load-bearing and
none is obvious from the code: the updater lives in `petctl` rather than `petd`
so the daemon keeps no HTTP client, `petd` nonetheless starts one subprocess —
`petctl update --if-due`, at launch, and only that — and the "dev channel" is a
**second application** (`Agent Pet (dev)`) rather than a setting. Read
[ADR 0008](docs/adr/0008-over-the-air-updates.md) before touching any of it —
a channel picker was built first and deliberately deleted.

Next, per [ADR 0006](docs/adr/0006-milestone-1-boundaries.md): the Codex
adapter (Milestone 3), a git `post-commit` adapter, and a durable SQLite store
for statistics (Milestone 4, currently in-memory only).

The counters are still collected and still reach `/state` and `petctl status`;
what they no longer have is a menu item. The Statistics panel was deleted
because nobody opened it twice, not because the numbers stopped mattering — a
use worth the screen space may yet turn up, and the store is still planned. Do
not restore the menu entry as a fix for the missing panel.

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
make test          # Go. Engine, state machine, event API, adapter, placement, updater.
make test-ui       # Opens a browser. Runs against the real ui/dist/index.html.
make test-desktop  # Needs the app running. Menu bar, window placement, updates.
```

Run all three before committing anything that touches the window or the menu
bar. The Go suite alone will not catch a crash in a menu handler — one shipped
that way.

`internal/petassets/sprite_rules_test.go` is the one that guards the art. Every
character so far has arrived with a fault only a pair of eyes caught, and two
of them shipped: a blush drawn with an alpha punched translucent holes in every
face for months, and a running character's hands were never drawn at all. The
rules there are the faults that can be stated as a property of the pixels —
no holes in the sprite, every strip animates, every frame has a character in
it, and nothing of the character is cut off by the top of the window.

That last one carries an allowance table of what each state loses today. None
of it is deliberate; it is hair sheared flat by the frame when a bob lifts the
character, and it was found by writing the rule rather than by looking. The
table exists to stop the list growing — a new character starts at zero. Do not
raise an entry to make a test pass; lower the character or shorten the hair.

One rule cannot live there and is checked in `tools/genpets` instead, by
`check_rules` before anything is drawn: a sleeve has to stop short of the hand.
It is invisible in the finished sprite because the arm and the hand are the
same colour — "sleeve against forearm" and "sleeve against knuckles" are the
same pixels — so `make pets` stops rather than writing art nothing downstream
can tell is wrong. Anything else about the drawing rather than the drawn
belongs beside it.

`make test-ui` serves the repo and opens `ui/test/index.html`, which loads the
real UI into an iframe per test. `make test-desktop` drives a running app
through `POST /window` — the only way to check a menu is not clipped in the
corner of a screen, since nothing may read a macOS menu bar without
accessibility access, which is refused here.

## Layout

```
main.go                the entry point: flags, config, logging, wiring
internal/desktop/      the Wails shell — the only package importing Wails
                       (statusitem_darwin.* is the menu-bar item, in cgo)
internal/state/        the state machine: pure, no clock, no goroutines
internal/engine/       event intake, timers, subscribers
internal/server/       the loopback event API
internal/{events,petassets,bubble,config}/
internal/flavor/       what makes the two apps two apps: bundle id, port, config
                       and data paths, icons. Shared values collide silently, so
                       there is a test
internal/update/       manifest, channels, semver. No net/http, no os/exec —
                       that is the boundary that keeps petd offline
adapters/claude/       hook payload → event, and the settings.json patcher
cmd/petctl/            the CLI. Shares no code with the engine, on purpose.
                       update.go and apply.go are the whole updater.
updates/               the channel manifests. Committing one publishes a release
tools/genpets/         the sprite generator. All art is generated.
ui/dist/index.html     the whole frontend, one file
ui/test/               its tests
scripts/desktop-test.sh
```

Releasing a signed build — the Apple setup, what the signing script does and
why each step is load-bearing — is [docs/signing.md](docs/signing.md).

Decisions live in `docs/adr/`. The commit messages carry the reasoning for
most of what is here and are worth reading before changing something that
looks arbitrary — much of it is load-bearing.

## Things that will waste your time

Each of these cost an hour or more to find.

- **A stale `petd` holds the port.** A new instance cannot bind and stops, so
  your rebuild appears to do nothing. It says so in a dialog now rather than
  only on stderr, which nothing launched from the Finder can show.
  `pkill -9 -f 'MacOS/petd'` before relaunching.
- **A copy in `build/bin` will answer for the installed app.** The lock is by
  bundle identifier, so whichever started first holds the port and the other
  exits — and with both at the same version nothing on screen tells them apart.
  A release build took over this machine's dev port twice, and `petctl doctor`
  pointed confidently at `/Applications` both times. It names the answering
  binary now when the two differ. `make release` leaves such a copy behind.
- **A failed `make build` leaves the previous `.app` in place.** A green test
  run after a failed build tested the old binary and means nothing. Check the
  build said `Built '...'` before believing a result.
- **`config.yaml` is rewritten from memory on shutdown.** Editing it while the
  app runs loses the edit. Close the app first.
- **Changing a default in `config.go` does not touch an existing
  `config.yaml`.** Your machine keeps the old value; test on a fresh config or
  edit both.
- **The UI test runner and its iframes are cache-busted deliberately.** Without
  it the browser serves a stale `index.html` and the suite passes against a
  file no longer on disk. It did exactly that, twice.
- **`make test-desktop` is stateful.** Sections set the character and
  visibility they expect first, because asserting on what the last run left
  behind passes in sequence and fails alone.
- **cgo compiles Objective-C without ARC.** `-fobjc-arc` in
  `internal/desktop/statusitem_darwin.go` is load-bearing: without it the status
  item is autoreleased out from under its static and the next message crashes
  the app.
- **Wails names the frontend's backend namespace after the Go package.** `App`
  lives in `internal/desktop`, so the UI calls `window.go.desktop.App`. Move
  that package and every backend call silently returns null — `backend()` in
  `ui/dist/index.html` degrades instead of throwing. `make test-desktop` is what
  catches it; the UI suite stubs the namespace and cannot.
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
- **Signing a release does not offer it to anybody.** `make release` leaves
  `updates/<channel>.json` uncommitted on purpose; the commit is the release.
  Expect `petctl update --check` to report nothing published until then, and do
  not "fix" it by pointing the updater at the releases API.
- **`petctl update` re-execs a copy of itself out of the bundle** before
  touching it, passing `os.Args` through. A new flag works automatically; a
  flag read from somewhere other than `os.Args` will silently not survive.
- **Both channels have been published to, and neither is where you would
  guess.** Release is on 0.2.1 and dev is on 0.2.0-dev.6 — dev is *behind*
  release, because 0.2.0-dev.6 is a prerelease of 0.2.0. A build from `master`
  is 0.2.1, so it reads as up to date against release and ahead of the channel
  against dev. Both are correct and neither is a fault. Check
  `updates/*.json` rather than this line: it has been wrong once already, and
  it goes stale every time a manifest is committed.
  The 404-means-nothing-published path still exists and still has a test; it is
  just no longer what you will see.
- **The Wails single-instance lock is keyed by bundle identifier.** With a
  shared key the second app to start binds its port, logs `petd started`, and
  then exits with **status 0 and no error at all** — Wails checks the lock
  inside `Run`. It looks exactly like an app that declined to launch for no
  reason. Anything new that both apps touch belongs in `internal/flavor` with a
  line in `TestFlavoursDoNotCollide`.
- **`make build` builds one app, and `-clean` deletes the other.** To run both,
  build one, move it aside, build the other, move it back. `build/bin` holds
  whichever flavor you built last.
- **`go build` with no ldflags is the release flavor.** A `bin/petctl` built by
  `make petctl` talks to 9876 and reports on `agent-pet.app`, whatever you were
  working on. The one inside the dev bundle is the dev one.

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
