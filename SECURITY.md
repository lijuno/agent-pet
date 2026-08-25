# Security

## Reporting a vulnerability

Report privately through GitHub: **Security → Advisories → Report a
vulnerability** on this repository. That opens a private thread visible only to
you and the maintainer. Please do not open a public issue for something
exploitable.

This is a spare-time project, so a fix is not on a clock. Expect an
acknowledgement within about a week. If you would like credit in the advisory,
say so and how you would like to be named.

## What this program is

A desktop pet that watches a coding agent. It runs entirely on one machine:
the daemon opens no outbound connections of any kind, contains no HTTP client,
and the embedded frontend loads no remote script, font or image.

It starts exactly one subprocess, and only one: `petctl update --if-due`, from
inside its own bundle, once when it launches. That is the update check, and the
next section is about it. It requests no accessibility, screen-recording, camera,
microphone or keyboard-monitoring permission, and needs no elevated privileges.

It does listen: `127.0.0.1:9876` carries the event API that adapters and
`petctl` use.

## Updates do reach the network, from `petctl` and never from the daemon

The app can update itself (ADR 0008), which is the sort of feature that usually
ends the paragraph above. It does not, because the code that fetches, downloads
and installs lives in `petctl` — a separate program `petd` cannot import. The
daemon holds a version number it was *told* over the loopback API and shows it
in a menu; it has no way to find one out.

`petd` starts that check itself, at launch — one known binary, beside its own
executable, with fixed arguments. It reads nothing back: `petctl` reports what
it found over the loopback API like any other caller, and whether a check runs
at all is `petctl`'s decision, throttled to once a day and off entirely when
`update.check: false`.

The daemon still opens no connection. Which program dials out did not change;
what changed is that the daemon may now ask it to.

That is the only process it starts. The menu-bar item still opens a release page
through `NSWorkspace` rather than the Wails runtime's `BrowserOpenURL`, which
would run `/usr/bin/open` — one deliberate subprocess with a fixed path is a
thing you can audit, and a general habit of shelling out is not.

What `petctl` does reach:

- `raw.githubusercontent.com`, for a small JSON manifest — when you run
  `petctl update --check`, and once a day from the Claude Code `SessionStart`
  hook. Nothing is sent but the request.
- `github.com` and its asset storage, to download a release you chose to
  install.

The manifest check runs on its own, at most once a day. It is the only thing
this program does unasked, and all it does is read a version number: nothing is
downloaded and nothing is installed without you asking for it. `update.check:
false` in the config file stops it, and an installation that predates this
default keeps whatever it already had — the file is rewritten in full on every
quit, so its answer is already on disk.

Before an update replaces the app, the download must match the manifest's
SHA-256, pass `codesign --verify --strict` and `spctl`, and carry **the same
Apple team identifier and bundle identifier as the app being replaced** —
Apple's notarization says somebody legitimate signed it; the team check is what
says it was us, and the bundle check is what says it is this app rather than
`Agent Pet (dev)`, which is a separate application with its own port and data
directory.

## The listener has no authentication, on purpose

Any process running as your user can post events, force a state, switch
characters, move the window, open its panels, and read `/diagnostics`. There is
no token and no permission prompt.

This is a deliberate trade and worth being explicit about, because a coding
agent runs commands on your behalf — so "a local process" includes anything the
agent is told to run. What that gets an attacker is bounded by what the API can
express: the pet can be made to lie about what your agent is doing, put itself
somewhere inconvenient on screen, or be told to quit. `/diagnostics` returns the
config path and data directory, which contain your account name.

It cannot read your files, run a command, or reach the network. No endpoint
takes a path or a command as input, and the one that takes a URL — `/update`,
where `petctl` reports what a check found — accepts nothing but an HTTPS
`github.com` URL inside this project's repository, and only ever hands it to
the browser when somebody clicks the menu item. Nothing there is fetched.

A local attacker with that much access has better targets than the pet. The
boundary being defended here is the machine, not the process: the concern is a
*remote* page or a *non-loopback* peer reaching the API, not a local one.

### What defends that boundary

- The listener binds `127.0.0.1` and refuses any other address unless
  `server.allow_non_loopback` is set explicitly in the config file.
- Every request is dropped if its peer address is not a loopback IP, even if
  the listener somehow ended up bound more widely.
- A request carrying an `Origin` is dropped unless that origin's host is
  exactly `localhost`, a loopback IP, or the webview's own `wails:` scheme.
  The host is parsed and compared, not prefix-matched — `localhost.evil.com`
  resolves to 127.0.0.1 for anyone who registers it, and is refused.

Setting `server.allow_non_loopback` puts an unauthenticated API on your
network. Do not set it on a machine you do not control.

## Handling of untrusted input

Everything an adapter sends is untrusted, including the agent's own output:

- Request bodies are capped at 16 KiB and decoded strictly — an unknown field
  is an error rather than a silent ignore.
- Session ids, tool names and metadata are truncated (16 metadata keys, 32-byte
  keys, 256-byte values) and stripped of control characters, so a terminal
  escape sequence in a tool name cannot corrupt the log or the speech bubble.
- No event field is ever executed, interpolated into a command, used as a file
  path, or turned into markup. The UI test suite asserts the markup property
  directly.
- An event that cannot be understood becomes an unknown event that refreshes
  the activity clock and changes nothing else. Malformed input is never fatal.
- Character packs are ordinary local files. Pack ids and the filenames inside a
  manifest are validated: a name containing a separator or `..` is rejected, so
  a manifest cannot reach outside its own directory.

## Privacy

No prompt, no source code, and no command argument is recorded anywhere. The
adapter reads four fields out of the Claude Code hook payload and drops the
rest, including `transcript_path` and `cwd`. Default logs carry event
categories only; `logging.verbose: true` adds tool names. Nothing is uploaded,
because there is nowhere to upload it to.

## Supported versions

Pre-1.0. Only the tip of the default branch is supported; there are no
backports.

## Out of scope

- A local process driving the pet through the API. That is the design, above.
- Anything reachable only after setting `server.allow_non_loopback`, which
  is documented as putting an unauthenticated API on the network.
- Denial of service by an adapter posting events as fast as it can. The pet
  stops being useful; nothing else happens.
- Release binaries being unsigned and unnotarized. Known and stated in the
  README — build from source if that matters to you.
