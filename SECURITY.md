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
never executes a subprocess, and the embedded frontend loads no remote script,
font or image. It requests no accessibility, screen-recording, camera,
microphone or keyboard-monitoring permission, and needs no elevated privileges.

It does listen: `127.0.0.1:9876` carries the event API that adapters and
`petctl` use.

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

It cannot read your files, run a command, or reach the network, because no
endpoint takes a path, a command or a URL as input.

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
