---
description: Install or update the Agent Pet macOS app that this plugin drives. Use when the user asks to install, set up, or update the pet app.
disable-model-invocation: true
---

# Install the Agent Pet app

This plugin ships the Claude Code hooks. The app they drive is a separate
download, because a macOS `.app` bundle cannot sensibly live in a git
repository. Your job is to install that app and prove it works.

The pet is cosmetic. Nothing here is worth taking a risk for — if a step below
fails, stop and say so rather than working around it.

## Hard rules

**Never run `xattr -d com.apple.quarantine`, `spctl --master-disable`, or
`codesign --force` on this or any other app.** Those commands turn off the
check that tells the user whether what they just downloaded is what the author
published. Releases are signed and notarized precisely so that nobody needs
them. If macOS refuses to open the app, that is a finding to report, not an
obstacle to route around — go to *When verification fails*.

**Do not fall back to `curl … | sh`,** and do not install from anywhere other
than the GitHub releases of `lijuno/agent-pet`.

## Steps

### 1. Check what is already there

```bash
petctl doctor
```

`petctl` is on your `PATH` while this plugin is enabled.

**If it reports a running app, the rest of this skill is not what you want.**
The app updates itself, and doing it by hand risks getting it wrong in ways the
built-in path already handles — quitting the old daemon before replacing the
bundle it is running from, and checking that what arrived is signed by the same
developer as what is being replaced.

```bash
petctl update --check --json
```

That prints `{"available": true, "latest": "..."}` when there is something
newer. Tell the user the version, and install it only if they say yes:

```bash
petctl update
```

It quits the pet, verifies the download, swaps the bundle and starts it again,
and it refuses rather than guesses at every step. Then go to *Prove it works*.
If it says this is a dev build, the user built it themselves — say so and stop.

If `petctl doctor` reports the app is not installed, continue below.

### 2. Ask the manifest what to install

The same file `petctl update` reads, so an install and an update always agree
about what the current version is:

```bash
curl -fsSL https://raw.githubusercontent.com/lijuno/agent-pet/master/updates/release.json
```

It gives you `version`, `url`, `sha256`, `size` and `min_macos`. Use those four —
do **not** go looking for an asset by name, and do not use the GitHub releases
API instead; the manifest is the thing the project publishes deliberately, and a
release can exist for days before it is offered to anyone.

**If it 404s, stop** and tell the user nothing has been published yet, pointing
them at `README.md` for building from source. Do not improvise another download.

Check `min_macos` against `sw_vers -productVersion` before downloading anything.
An app that will not launch on their machine is not worth 19 MB of their
bandwidth.

### 3. Download to a temporary directory

Never unpack straight into `/Applications`:

```bash
tmp=$(mktemp -d) && curl -fsSL -o "$tmp/pet.zip" <URL_FROM_MANIFEST> && ditto -x -k "$tmp/pet.zip" "$tmp"
```

Use `ditto`, not `unzip` — `unzip` does not preserve the symlinks and metadata
inside a bundle.

### 4. Verify it — before it goes anywhere

This is the step the whole procedure exists for. Run all three.

```bash
shasum -a 256 "$tmp/pet.zip"
```

Compare against `sha256` from the manifest. This one is worth doing first
because it is the cheapest and its failure is the least alarming: a mismatch is
usually a truncated download.

```bash
codesign --verify --strict --deep --verbose=2 "$tmp/agent-pet.app"
```

```bash
spctl -a -t exec -vv "$tmp/agent-pet.app"
```

The first says the bundle is intact and unmodified since signing. The second
says Gatekeeper accepts it — expect `source=Notarized Developer ID` and
`origin=Developer ID Application: Lijun Li (YH77U4A52H)`.

**All three must pass.** If any fails, delete `$tmp` and go to *When
verification fails*. Do not install it anyway, and do not ask the user whether
they would like you to install it anyway.

### 5. Install and launch

```bash
rm -rf "/Applications/agent-pet.app" && mv "$tmp/agent-pet.app" /Applications/ && open "/Applications/agent-pet.app"
```

If the app was already running, quit it first — a still-running copy holds the
event port, and a second instance exits immediately on startup rather than
complaining, which looks exactly like an update that did nothing.

### 6. Prove it works

```bash
petctl doctor
```

Then make the pet visibly do something, so the user confirms with their eyes
rather than trusting your summary:

```bash
petctl test celebrate --for 5s
```

Ask whether they saw it. If they did not, go to `/agent-pet:troubleshoot`.

### 7. Mention the dev app, do not install it

There is a second application, `Agent Pet (dev)`, that installs alongside this
one and follows prereleases. Its manifest is `updates/dev.json`, and installing
it is the same procedure with that file — but do not, unless they ask. It is not an alternative to what you just
installed and it is not a setting — it is a separate app with its own icon,
its own port and its own settings, and both can run at once.

Say it exists and leave it there. Someone who wants prereleases will ask; the
release app is the right thing for everyone else, and installing both without
being asked leaves an extra menu-bar-only app on their machine.

Also mention that automatic update checks are **off** — the app contacts no
server at all until they turn them on. `petctl update --check` is the manual
version, and `update.check: true` in the config file that `petctl doctor` names
turns on a once-a-day check when a Claude Code session starts.

### 8. Tell them what happens next

The hooks come from this plugin and are already active — there is **no**
`petctl install claude` step, and nothing was written to their
`settings.json`. If the plugin was installed in this same session, hooks may
need `/reload-plugins`, or a new session, before the pet starts reacting.

## When verification fails

Report it plainly and stop. Say which command failed and quote its output.

A checksum failure on its own is almost always a truncated or corrupted
download — retry once, cleanly.

A **signature** failure is different. It means the download was corrupted, or
the file is not what the author published, and neither you nor the user can
tell those apart from the output. One clean retry is reasonable. If it fails
again, leave it uninstalled and let the user decide.

Either way: say which command failed and quote its output. Do not summarise it
as "the install did not work".
