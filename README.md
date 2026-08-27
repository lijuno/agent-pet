# Agent Pet

[![CI](https://github.com/lijuno/agent-pet/actions/workflows/ci.yml/badge.svg)](https://github.com/lijuno/agent-pet/actions/workflows/ci.yml)

A small desktop companion that reacts to what your coding agent is doing.
Claude Code starts, the pet wakes up. A tool runs, it gets to work. A permission
prompt appears, it turns and asks for you. Tests pass, it celebrates.

<p align="center">
  <img src="docs/media/states-sanmao.gif"
       alt="The pet cycling through its ten states: idle, thinking, working, attention, confused, worried, happy, celebrate, heart and sleeping. Each is a four- to six-frame pixel-art animation of a tortoiseshell cat, labelled with its name and frame rate."
       width="288">
</p>

<p align="center">
  <sub>Every state the pet can be in, rendered from the sprites it actually
  ships — <code>make states-gif</code>.</sub>
</p>

```
   agent events  ──▶  local state machine  ──▶  expressive character
```

Nothing about your work leaves your machine. The app has no network client at
all — it listens on loopback and calls nothing out. The one thing that does go
out is an update check: a small JSON manifest, fetched by `petctl`, at most once
a day, and off with one line of config. See [Privacy](#privacy).

---

## Install

You need macOS 12 or newer. The release is a universal build, so Apple Silicon
and Intel both work. Nothing else — no Go, no Xcode, no toolchain.

### With the Claude Code plugin (recommended)

The plugin carries the hooks; the app is a separate signed download. In Claude
Code:

```
/plugin marketplace add lijuno/agent-pet
/plugin install agent-pet@lijuno
/agent-pet:install
```

`/agent-pet:install` reads the published manifest, downloads what it names, and
checks the SHA-256 and the signature — `codesign` and `spctl` both — **before**
anything goes near `/Applications`. Then it makes the pet visibly do something,
so you confirm it works with your eyes rather than trusting a summary.

This path never touches your `settings.json` and never bakes an absolute path
into it, so it cannot go stale when the app moves. There is also
`/agent-pet:troubleshoot` for when the pet stops reacting.

### By hand

1. Download `agent-pet-<version>-universal.zip` from
   [Releases](https://github.com/lijuno/agent-pet/releases) and unzip it.
2. Move `agent-pet.app` to `/Applications` and open it. Releases are signed and
   notarized, so it opens normally.
3. Put `petctl` on your `PATH` — it ships inside the bundle, so the CLI and the
   daemon it talks to can never be different builds:

   ```bash
   export PATH="$PATH:/Applications/agent-pet.app/Contents/MacOS"
   ```

4. Install the Claude Code hooks:

   ```bash
   petctl install claude       # this project;  --global for every project
   ```

Then start a **new** Claude Code session — hooks are read at startup, so the
session you are in now will not pick them up.

> Use one path or the other. With both the plugin and `petctl install claude`,
> every hook fires twice. `petctl doctor` reports the hooks it finds in
> `settings.json`, so the duplicate is visible.

### First run

Agent Pet is a menu-bar app: nothing appears in the Dock. Before an agent
connects, the pet is grey and asleep — that is correct, not a fault. Colour and
motion mean something is there to watch.

`petctl doctor` says more than you expect: whether the app is running, which
flavour it is, where its config lives, and whether the hooks are installed.

## Using it

| Action | Result |
|---|---|
| Drag | Move the pet. Its position is remembered. |
| Double-click | Pet it. |
| Right-click | Menu: status, change pet, size, always on top, drop shadow, report a bug, about, quit. |
| Menu bar | The same commands under the `Pet` menu while the app is frontmost. |

The pet also lives in the macOS menu bar. Its icon shows the current state and
carries the same commands. **Show Pet** is a checkbox there: it puts the pet
away and brings it back, and bringing it back also retrieves one that has been
dragged off the edge of the screen. **Change Pet** is a submenu listing the
characters, so switching happens in the menu bar rather than in a panel beside
the pet. **Report a Bug** opens a window saying where a report goes and what to
put in it, with a button that copies the version, the paths and the log
location onto the clipboard — the pet has no crash reporter and sends nothing
anywhere, so the details have to travel by hand. See
[ADR 0005](docs/adr/0005-macos-menu-and-tray.md).

The character comes in three sizes — small, medium and large — from the
right-click menu or the `pet.scale` setting.

Two rules decide whether she is awake, and neither has a special case:

- **No agent, no pet.** Claude Code exiting puts her to sleep at once, and so
  does never having started.
- **Idle and quiet for a minute** and she dozes off, waking on the next event.

A pending request is `attention`, not idle, so it falls outside the second rule
rather than being excused from it — nobody answers a prompt they were never
shown.

### Trying every state

To see the whole character without waiting for an agent to do something:

```bash
for s in idle thinking working attention confused worried happy celebrate sleeping heart; do
  petctl test $s --for 2s; sleep 2
done
```

A state forced with `petctl test` keeps its colour, since the point of forcing
one is to look at it. Or drive a realistic session by hand:

```bash
petctl event claude session_started --session demo
petctl event claude thinking_started --session demo
petctl event claude tool_started --session demo --meta tool=bash
petctl event claude permission_requested --session demo
petctl event claude tool_started --session demo --meta tool=edit
petctl event claude task_completed --session demo
petctl event claude tests_passed --session demo
```

`petctl watch` prints events as they arrive, which settles whether the hooks are
firing at all.

## Configuration

`~/.config/agent-pet/config.yaml` is written on first run.

```yaml
pet:
  active: sanmao        # sanmao, peach, juanmao, maomao, damao, amiao, or your own pack
  disabled: []          # ids to keep out of the Change Pet menu
  always_on_top: true
  scale: 1.0
  drop_shadow: true     # off if it reads as a halo on a light wallpaper

behavior:
  dialogue: true        # speech bubbles

personality:
  name: Sanmao (三毛)
  personality: gentle   # gentle cheerful calm mischievous sarcastic energetic

thresholds:
  idle_after: 30s       # a quiet agent stops looking busy
  sleeping_after: 60s   # ... and shortly after that, dozes off
  tool_patience: 30m    # but a tool still running keeps her at work this long

server:
  addr: 127.0.0.1:9876

update:
  check: true           # automatic update checks are on; set to false to disable
  interval: 24h
```

Anything you leave out keeps its default. A malformed file logs a warning and
falls back to defaults rather than refusing to start.

The file is rewritten from memory when the app quits, so edit it with the app
closed or the edit is lost. There is no channel setting: which channel a build
follows is decided when it is built — see [Updating](#updating).

## Updating

Ask Claude Code:

```
/agent-pet:update
```

It checks, tells you what it found, waits for you to say yes, and then runs the
command below — which is the same thing you can run yourself:

```bash
petctl update --check     # is there a newer one?
petctl update             # install it
```

`petctl update` quits the pet, downloads the release, and refuses to install it
unless the hash matches the manifest, `codesign` and `spctl` both pass, and the
download carries **the same Apple team identifier and bundle identifier as the
app it is replacing**. Notarization says somebody legitimate signed it; those
last two say it was us, and that it is this app rather than the other one. Then
it swaps the bundle — old one moved aside, not deleted, until the new one has
landed — waits for the event port to actually come free, and starts the new
version. A build you made yourself is never replaced.

The menu-bar menu always carries an update item, and it says what the last check
found: the version on offer, `Up to date`, `Nothing published yet`, `Ahead of
the channel`, `Update check failed`, or `No update check yet` before anything has
looked. Those are six different answers and none is reported as another. When
there is a version it opens the release notes; otherwise there is no page behind
it and it is not pressable. It never installs: the app holds no updater
and opens no connections of its own. `petctl` does the work, which is why
[SECURITY.md](SECURITY.md) can still say the daemon never dials out.

The pet asks for a check when it starts, so the answer is right the first time
you look rather than after the next Claude Code session. It asks by running
`petctl update --if-due` — the same throttle, so opening the app repeatedly is
not repeated checking, and nothing at all when `update.check: false`. That one
subprocess is the only one the daemon starts, and it is why SECURITY.md says
"exactly one" rather than "none".

A check runs on its own at most once a day, when a Claude Code session starts;
`update.check: false` in the config file stops it. Upgrading never turns it on
for you: the file is rewritten in full on every quit, so an installation that
predates this default already has its own answer written down.

#### If you replace the app by hand

Downloading a release and dragging it into `/Applications` works — the build is
signed and notarized, and nothing here objects to it. It just skips everything
above, so two things are yours to do:

**Quit the pet first.** The event port is held by whichever copy is running, and
a second one cannot have it. Replace the bundle underneath a running pet and the
new app opens, finds the port taken, and stops — you are still looking at the
old one, from wherever it happens to live. It says so in a dialog now rather
than exiting in silence, but quitting first avoids the question.

**Expect "No update check yet" until something checks.** The daemon is *told*
what the latest version is; it never looks. `petctl update` tells it as part of
finishing, which is the step a manual replacement does not run. The next Claude
Code session will check and the answer sticks from then on, or
`petctl update --check` does it immediately.

`petctl doctor` names the bundle that is actually answering, which is the fast
way to tell whether the copy you installed is the copy you are looking at.

### Two apps, not two channels

There is a second application:

| | Bundle | Listens on | Updates from |
|---|---|---|---|
| **Agent Pet** | `agent-pet.app` | `127.0.0.1:9876` | `updates/release.json` |
| **Agent Pet (dev)** | `agent-pet-dev.app` | `127.0.0.1:9877` | `updates/dev.json` |

They install side by side, the way VS Code ships Stable and Insiders. There is
no channel setting and nothing to switch: to follow prereleases, install the dev
app; to stop, quit it. Each has its own config file, data directory and menu-bar
icon, and neither can update itself into the other.

The point of that is what happens when a prerelease is broken — which is what
prereleases are for. Your working pet is a different application and is still
running. You can watch both react to the same session at once: `petctl` delivers
every event to every pet that is listening, so no configuration is needed to
compare them.

`petctl doctor` says which of them you have and which are running. The dev app
carries a badge on both its icons and names itself in its menu, because both are
menu-bar-only apps and would otherwise be indistinguishable.

## When it stops reacting

`/agent-pet:troubleshoot` walks this list if you have the plugin. By hand, in
the order the causes actually occur:

- **`petctl doctor` first.** It answers most of what follows in one line.
- **Hooks are read when a session starts.** A session that was already open when
  you installed them does not have them. `/reload-plugins`, or a new session.
- **Is the pipe intact?** `petctl event claude permission_requested` pokes the
  app directly, bypassing the hooks. If the pet reacts to that but not to real
  work, the hooks are the problem, not the app.
- **An update appeared to do nothing?** Almost always the old app was still
  running. It holds the event port, and the new copy exits at startup rather
  than reporting a conflict:

  ```bash
  pkill -f 'MacOS/petd'; sleep 1; open /Applications/agent-pet.app
  ```

- **Invisible, but reacting?** It may be off-screen or hidden; **Show Pet** in
  the menu bar retrieves it. The window is deliberately transparent and
  click-through in places, so "I cannot click it" is expected.

Do not disable Gatekeeper, strip quarantine attributes, or re-sign the bundle to
make something work. If macOS rejects a release, that is a finding worth
reporting, not an obstacle to route around.

## Privacy

- No prompts, no source code, no command arguments are recorded. Default logs
  contain event categories only; `logging.verbose: true` adds tool names.
- The pet itself has no network client of any kind: it listens on loopback and
  calls nothing out. Every byte that goes out is fetched by `petctl`, a separate
  program the daemon cannot import.
- The only thing fetched is a small JSON manifest naming the newest release, and
  the release itself if you choose to install it. Nothing about you is sent:
  `petctl` asks `raw.githubusercontent.com` for a file, and that request carries
  no identifier the project put there.
- That check runs at most once a day — when a Claude Code session starts, and
  once when the pet is launched. `update.check: false` stops it entirely, and
  upgrading never turns it back on for you.
- Character packs are built from local images. Nothing is uploaded, and there is
  no image-generation service to opt into.
- No accessibility, screen-recording, camera, microphone or keyboard-monitoring
  permission is requested, and no elevated privileges are needed.

The event API is unauthenticated, so anything running as you — including
whatever you ask your agent to run — can drive the pet. What that is and is not
worth worrying about is set out in [SECURITY.md](SECURITY.md), along with how
to report something privately.

---

## Building from source

### Requirements

- macOS (the architecture allows Windows and Linux; only macOS is exercised)
- Xcode Command Line Tools — `xcode-select --install`. The menu-bar item is
  Objective-C compiled through cgo, so a build needs clang; `codesign`,
  `notarytool` and `stapler` come from here too. Full Xcode is never needed.
- Go 1.23+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) **v2.10.2 or
  newer** — matching the version in `go.mod`. An older CLI fails to build on a
  recent Go toolchain with `internal error: package "strings" without types`,
  which does not look like a version problem:

  ```bash
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
  ```

  That installs into `$(go env GOPATH)/bin`, which is not on `PATH` by default
  on macOS — so `wails` may be on disk and still not be a command you can run.
  The Makefile looks there as well as on `PATH`, so `make build` works either
  way; add it to your shell if you want to run `wails` yourself:

  ```bash
  export PATH="$PATH:$(go env GOPATH)/bin"
  ```
- Python 3 with Pillow, only if you want to regenerate the sprite art

### Build and run

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

`make build` produces the bundle; `make build CHANNEL=dev` produces the dev app
instead. `make help` lists the rest.

A bundle you build yourself is neither signed nor notarized, and it runs without
complaint, because Gatekeeper only quarantines what arrives from elsewhere. One
you *downloaded* is quarantined, and macOS refuses it with *"Agent Pet" is
damaged and can't be opened*, which is not what has happened: the app is intact
and unsigned. To run such a bundle anyway:

```bash
xattr -d com.apple.quarantine /Applications/agent-pet.app
```

Do that only if you trust where the bundle came from, and never on a release —
a signed one needs no such thing, and an agent asked to install this app will
run that command without hesitating if the README implies it is routine.

### Tests

```bash
make test          # Go: engine, state machine, event API, adapter, placement
make test-ui       # the window, in a browser
make test-desktop  # the menu bar and corner placement, against a running app
```

CI runs the first of those on every push, against the Go version `go.mod`
declares and against current stable, and builds the app bundle. It cannot run
the other two: one needs a browser and the other needs the app running and
drives its menu bar. A green tick is not evidence that a menu handler works.

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

## Cutting a release

```bash
git tag v0.2.0     # the tag is the version; it reaches Info.plist via brand.sh
make release       # builds, notarizes, uploads, writes the manifest
```

`make release CHANNEL=dev` cuts the dev app instead, from a prerelease tag. The
two are built, signed and published separately; a release is one app or the
other, never both.

That builds the bundle, signs and staples it, attaches
`agent-pet-<version>-universal.zip` to the GitHub release — and then stops,
leaving `updates/release.json` modified and **uncommitted**. Apple usually
answers within a few minutes.

Nobody is offered the new version until you commit that file:

```bash
git diff updates/release.json
git commit -am "Offer 0.2.0 on the release channel" && git push
```

**Attach the assets to the GitHub release before pushing that commit** — both
of them, if you are cutting both apps. The manifest names a URL, and pushing it
first points every installed pet at a file that is not there yet.

This has already happened once: the manifest commit landed 41 seconds before
the dev release was created, and
[`.github/workflows/updates.yml`](.github/workflows/updates.yml) failed exactly
as it is meant to — it downloads each asset and checks its size and SHA-256
against the file you just committed. Re-run it once the assets are up. It is
the only thing standing between a typo and a broken update for everybody, now
that releases are cut by hand rather than by a runner.

Publishing an asset and offering it over the air are deliberately two acts. You
can build a release, install it yourself, live with it for a week, and only then
decide — and the decision has a diff, a commit message, and `git revert` as the
undo. See [`updates/README.md`](updates/README.md) and
[ADR 0008](docs/adr/0008-over-the-air-updates.md).

The individual steps are still there when you want them: `make build`,
`make notarize`.

Skip the tag and `make release` refuses: it takes the version from the tag on
HEAD, choosing the plain one for the release app and the prerelease for the dev
app, so both can be cut from the same commit. An untagged `make build` falls
back to `wails.json`, which is the only thing that file is still for.

`make build` alone needs nothing beyond the requirements above. `make notarize`
needs Apple credentials, and those do not travel with a clone — on a machine
that has never signed:

1. **The certificate.** Import your `.p12` backup (double-click it, give the
   export password). Do not create a second one: Apple caps how many Developer
   ID certificates an account may hold, and a certificate whose private key is
   on another Mac cannot sign anything. Confirm with

   ```bash
   security find-identity -v -p codesigning
   ```

   You want one `Developer ID Application: Your Name (TEAMID)`.

2. **The notary credentials**, from the App Store Connect API key. The `.p8`
   downloads exactly once and can never be re-fetched, so it is in your
   password manager or it is gone:

   ```bash
   xcrun notarytool store-credentials agent-pet --key AuthKey_XXXXX.p8 --key-id YOUR_KEY_ID
   ```

3. **The profile name**, so `make notarize` takes no arguments. The file is
   git-ignored, because it names a keychain item that exists only here:

   ```bash
   echo agent-pet > .notary-profile
   ```

Releases are cut by hand for the same reason step 1 is fiddly: signing on a
runner would mean a copy of that private key in the repository's secrets.
[`docs/signing.md`](docs/signing.md) covers the first-time Apple setup behind
all three — enrolment, creating the certificate, and creating the API key —
and what to do when a step fails.

---

## How it works

Hooks fire, `petctl hook claude` POSTs an event to a loopback port, a
deterministic state machine reduces the stream to one visible state, and a
transparent Wails window draws the character. Every decision that shaped that is
written down in [`docs/adr/`](docs/adr/) — start with
[ADR 0001](docs/adr/0001-single-process-petd.md) and
[ADR 0007](docs/adr/0007-what-the-states-mean.md), which says what each of the
ten states is allowed to mean.

The pet never guesses. If Claude Code does not report something, the pet does
not infer it — see the note on degrading honestly in
[`adapters/README.md`](adapters/README.md), which also carries the
hook-to-event table.

### The event API

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
| `POST /window` | Move the window or open a panel — see below |
| `GET /pets`, `POST /pet` | List and switch characters |
| `GET /update`, `POST /update` | What the last update check found. `petctl` posts here; the pet never looks for itself |

`POST /window` parks the window, opens its overlays and performs menu-bar items,
doing nothing you cannot do with a mouse. It exists because a test has no mouse:
the only way to know whether a menu is clipped in the corner of a screen is to
open one in the corner of a screen and measure it, and nothing on macOS may read
a menu bar without accessibility access. `make test-desktop` drives it; by hand:

```bash
curl -sS -X POST http://127.0.0.1:9876/window \
  -H 'content-type: application/json' -d '{"x":0,"y":400}'
curl -sS -X POST http://127.0.0.1:9876/window \
  -H 'content-type: application/json' -d '{"panel":"menu"}'
petctl doctor      # ... overlay  176x224 at 62,168 — fully on screen
```

The listener binds `127.0.0.1` and refuses anything else unless you explicitly
set `server.allow_non_loopback`. See [`docs/events.md`](docs/events.md) for the
full event vocabulary and [ADR 0002](docs/adr/0002-event-transport.md) for the
hardening applied to untrusted input.

### Characters

Six built-in packs ship in the binary:

<p align="center">
  <img src="docs/media/states-sanmao.gif"
       alt="Sanmao, a tortoiseshell tabby, cycling through the ten states as pixel-art animations labelled with their names and frame rates."
       width="240">
  <img src="docs/media/states-peach.gif"
       alt="Peach, a girl with long dark hair and a white dress, cycling through the same ten states: at the laptop for working, arms up for celebrate, asleep with Z's for sleeping."
       width="240">
  <img src="docs/media/states-juanmao.gif"
       alt="Juanmao, a man with curly hair and rectangular glasses, cycling through the same ten states: out on his bicycle for working, brows angled for worried, arms up for celebrate."
       width="240">
  <img src="docs/media/states-maomao.gif"
       alt="Maomao, a small boy in round clear glasses, cycling through the same ten states: writing at a desk for working, eyes wide for attention, arms up for celebrate."
       width="240">
  <img src="docs/media/states-damao.gif"
       alt="Damao, a teenager with a great pile of hair, cycling through the same ten states: out running for working, brows angled for worried, arms up for celebrate."
       width="240">
  <img src="docs/media/states-amiao.gif"
       alt="Amiao, a woman with a dark bob and a red blouse, cycling through the same ten states: swinging a pickaxe at a gold seam for working, eyes closed for sleeping, arms up for celebrate."
       width="240">
</p>

<p align="center">
  <sub>Every character, every state, rendered from the sprites they ship —
  <code>make states-gif</code>.</sub>
</p>

| id | | |
|---|---|---|
| `amiao` | Amiao (阿喵), a woman in a red blouse | a dark bob cut level, Peach's gold necklace, mining gold |
| `damao` | Damao (大毛), a boy of fourteen | a great pile of hair, an olive windbreaker, out running |
| `juanmao` | Juanmao (卷毛), a man in a charcoal tee | curly hair, rectangular glasses, a bicycle to work on |
| `maomao` | Maomao (毛毛), a boy of seven | a short crop, round clear glasses, a pencil to work with |
| `peach` | Peach (桃桃), a girl in a light blue skirt | long dark hair over one shoulder, peach bow, gold necklace |
| `sanmao` | Sanmao (三毛), a tortoiseshell tabby | white bib, white socks, hazel eyes |

Pick one from the menu-bar item's **Change Pet** submenu, or with
`petctl pet peach`. `tools/genpets` carries a seventh species, `byte`, as a
worked example of a species that is none of these; shipping it is one line.

All six provide all ten states as 40×40 pixel-art sprite strips. Regenerate
them with:

```bash
make pets     # python3 tools/genpets/genpets.py
```

**Reload Config** in either menu re-reads this file without a restart, which is
what makes editing it by hand practical: the app rewrites the file from memory
when it quits, so an edit made while it runs is otherwise lost. Everything here
takes effect except `server.addr`, which is bound once at startup and says so.

Characters you never use can be kept out of the **Change Pet** submenu and the
panel beside it with `pet.disabled` in the config, which takes a list of ids. It
hides rather than unloads, so one named in `pet.active` is still drawn — and it
will not hide the character on screen or the last one left, because a menu that
disagrees with the window is worse than a menu with an entry too many.

Your own packs go in `~/.local/share/agent-pet/pets/<id>/` and are picked up
at startup. A pack with the same id as a built-in replaces it. The format is one
PNG strip per state plus a `manifest.json`; see [`docs/pets.md`](docs/pets.md).

### Layout

```
main.go                the entry point: flags, config, logging, wiring
internal/desktop/      the Wails shell — the only package that imports Wails
internal/events/       the internal event vocabulary and its sanitiser
internal/state/        the deterministic state machine
internal/engine/       event intake, timers, subscribers
internal/server/       the loopback event API
internal/petassets/    pet pack loading
internal/bubble/       template speech
internal/config/       config.yaml
internal/flavor/       what makes Agent Pet and Agent Pet (dev) two apps: the
                       bundle id, port, paths and icons that must never be shared
internal/update/       the update manifest and version comparison — no network,
                       no subprocess, so petd stays free of both (ADR 0008)
cmd/petctl/            the CLI (no shared code with the engine), and the whole
                       of the updater: the fetch, the checks, the bundle swap
plugin/                the Claude Code plugin: the hooks, and the install and
                       troubleshoot skills
updates/               the channel manifests — committing one is what publishes
                       a release to everybody
adapters/claude/       the Claude Code hook adapter
adapters/              Codex and git adapters — Milestone 3 and later
tools/genpets/         the sprite generator
ui/dist/               the frontend, embedded in the binary
ui/test/               its tests — open in a browser, no build step
docs/adr/              architectural decisions
```

### Where it stands

**Milestones 1 and 2 are in** — the pet engine, the local event API, `petctl`,
the desktop window, a pixel-art character, the Claude Code adapter and plugin,
and over-the-air updates. The Codex adapter is Milestone 3, and statistics are
collected but not shown — they are in memory rather than a durable store, and
`petctl status` is where to read them;
[ADR 0006](docs/adr/0006-milestone-1-boundaries.md) records which seam each
deferred piece attaches to.

If you want to work on it, [CLAUDE.md](CLAUDE.md) is the map — including a list
of the things here that cost an hour each to discover.

## License

MIT — see [LICENSE](LICENSE).

The pixel art is generated by `tools/genpets/genpets.py` in this repository and
is covered by the same licence. A release binary also contains
[Wails](https://github.com/wailsapp/wails) (MIT) and
[yaml.v3](https://github.com/go-yaml/yaml) (MIT and Apache 2.0); if you
redistribute built binaries rather than source, include their notices too.
