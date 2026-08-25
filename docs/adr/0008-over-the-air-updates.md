# ADR 0008 — Over-the-air updates

Status: accepted

The app is installed by an agent, from a GitHub release, into `/Applications`.
Until now it stayed at whatever version it was installed at, and the only way to
move it was to run the install skill again and hope somebody thought to.

Three constraints shaped every decision below.

1. **Signing happens on one machine.** [ADR-adjacent, see
   docs/signing.md](../signing.md): there is no signing job in CI because a
   runner can only sign with the certificate's private key in repository
   secrets, and Apple caps how many Developer ID certificates an account may
   hold. Releases are cut by hand.
2. **The daemon opens no outbound connections.** SECURITY.md said so, in those
   words, before this feature existed. An updater is exactly the thing that
   breaks such a claim.
3. **A local process can post anything to the event API.** There is no
   authentication, on purpose. Whatever the updater tells the pet, something
   else can tell it too.

## The decisions

### The network lives in `petctl`, not in `petd`

`petctl` fetches the manifest, downloads the asset, checks the signature and
swaps the bundle. `petd` holds a result it was *told* and displays it.

This is enforced by the package boundary rather than by discipline.
`internal/update` contains no `net/http` and no `os/exec` — it is types and
validation. The code that reaches the network lives in `cmd/petctl`, a `main`
package that `petd` cannot import even by accident.

So SECURITY.md's paragraph needed one sentence added rather than deleting.

### `petd` starts one subprocess: the update check, at launch

**Revised.** This document originally said `petd` spawns no processes at all,
and rejected installing from the menu bar on that basis. The no-subprocess rule
is now one exception wide.

The argument it overturns is worth keeping, because it is still most of the
reason the rest of this holds: a daemon that starts no processes cannot be
talked into starting the wrong one, and the loopback API — which anything
running as this user can post to — could not reach `exec` at all.

What overturned it: the daemon is *told* update results and holds them only in
memory, so a freshly started one knows nothing and its menu says "no update
check yet". That is true and useless, and it is the state somebody is in the
first time they open the app, straight after installing it, and straight after
replacing the bundle by hand. Persisting the last result across restarts fixed
the second and third of those; nothing fixes the first except looking.

So `petd` runs `petctl update --if-due` once at launch: one known binary, beside
its own executable in its own bundle, with fixed arguments, detached and never
waited for. It reads nothing back — `petctl` posts what it finds over loopback
like any other caller — and `petctl` decides whether to check at all, using the
same daily throttle and the same `update.check` setting as the session hook.

What did *not* change, and is the reason this is the cheaper of the two ways to
get here: `petd` still has no HTTP client, and `internal/update` still contains
no `net/http`. The code that reaches the network is still in `cmd/petctl`, a
main package `petd` cannot import even by accident. "The daemon opens no
outbound connections" is still enforced by the compiler rather than by anybody
remembering. Giving `petd` an HTTP client would have been the other way, and it
would have spent that guarantee instead of this one.

Installing from the menu bar stays rejected. One subprocess with a fixed path
and fixed arguments is a thing you can audit; a menu item that installs software
is a different question and is not answered by this.

### The menu bar opens URLs through NSWorkspace, not the Wails runtime

`runtime.BrowserOpenURL` runs `/usr/bin/open` as a subprocess. The status item
calls `[NSWorkspace openURL:]` from the cgo file that already exists — six lines
of Objective-C. Written when `petd` spawned nothing at all; kept now for a
narrower reason, that the one subprocess it does start should stay the only one
and stay easy to find.

### Two applications, not two channels

`Agent Pet` and `Agent Pet (dev)` are separate apps that install side by side,
the way VS Code ships Stable and Insiders and Chrome ships Beta and Canary.
There is no channel setting, and no way to move an install from one channel to
the other: you run the other app.

The first design was a channel picker in the menu bar, and it was built. Every
question it raised was a bad one. Switching from dev back to release is a
*downgrade*, and an older binary rewrites `config.yaml` from the fields it knows
and silently drops the rest — so the switch would eat settings. Switching either
way had to discard a pending result, invalidate the check timer, and explain
that "up to date" meant "ahead of the channel you just chose". Doing the install
on switch would have meant `petd` spawning a subprocess, costing the claim
above.

Everyone who has implemented switch-in-place hits the same wall in the same
direction. ChromeOS requires a Powerwash to move to a more stable channel.
Android's beta programme factory-resets the device on the way out. Apple's
answer for leaving an iOS beta is to restore from a backup. Firefox has a
dedicated *profile downgrade protection* feature whose entire job is to refuse.
Nobody has a clever answer, because there is not one.

Shipping two apps deletes the question rather than answering it. It also buys
something no channel switch can: **a broken dev build cannot take out the
working pet.** For an app whose whole job is to sit there reliably while you
work, that is worth more than the plumbing it costs.

What it costs is that identity becomes a build-time variable. Six things must
differ between the two apps, and every one of them fails silently when shared:

| | Shared consequence |
|---|---|
| Bundle identifier | macOS treats them as one application |
| Bundle name | two apps called the same thing in `/Applications` |
| Event API port | the second app exits at startup — the "rebuild did nothing" bug |
| Config directory | two pets writing one file, last one out wins |
| Data directory | shared pets, logs and update stamp |
| Wails single-instance lock | whichever starts second **exits with status 0 and no error** |

That last one was not on the list until both apps were run at once and the
second one vanished. It is why `internal/flavor` exists as a package with a test
rather than as a handful of constants: `TestFlavoursDoNotCollide` fails on a
shared value, and `scripts/brand.sh` asks the built binary what it became rather
than repeating any of these values in shell.

The menu-bar icon and the Finder icon are badged for the dev app, and the dev
menu's first line names the app and its version. Both apps are `LSUIElement`,
with no Dock icon and no app-switcher entry, so two identical menu-bar icons
would be the only thing distinguishing them — which is to say nothing would.
`petctl doctor` lists both apps, installed and running, for the same reason.

### Events reach both pets

`petctl` delivers an event to every pet that is listening, concurrently, not
just to the one it belongs to. Both apps watch the same agent, so both should
react — and comparing a dev build against the working one while actually using
it is the reason to install the dev build at all. It needs no configuration, and
the app that is not running costs nothing: a refused connection on loopback
returns immediately, and the hook already treats every failure as normal.

`--addr` or `AGENT_PET_ADDR` pins to one. Asking for one pet gets one pet.

### A manifest file in git, not the GitHub releases API

`GET /releases/latest` was the obvious choice and is rejected, because it
collapses two events into one: attaching an asset would offer it to everybody
on their next check.

With `updates/release.json`, `make release` builds, notarizes, uploads — and
stops, leaving the manifest modified and uncommitted. **The commit is the
release.** You can build, install it yourself, live with it for a week, and only
then decide. The offer gets a diff, a commit message explaining why, and `git
revert` as the undo.

It also carries fields the API has nowhere to put: a minimum macOS version, a
size, and the hash.

### Gatekeeper is the trust root, plus a team check

There is no second signing key and no EdDSA appcast signature, because there is
already a Developer ID certificate and a notarization ticket, and a second root
would be a second thing to lose.

`petctl update` checks, in order: the SHA-256 against the manifest, then
`codesign --verify --strict`, then `spctl -a -t exec`, then that the download's
**team identifier equals the installed app's**. That last one is the one people
skip. `spctl` passing means somebody Apple knows notarized this; the team check
is what makes it *us*. Since exactly one machine will ever sign this app, the
team identifier is fixed for the life of the project and the comparison can
never legitimately fail.

Then it checks that the bundle inside the zip claims the version the manifest
offered **and the same bundle identifier as the app being replaced**, because a
manifest naming the wrong asset is the failure mode a hand-cut release actually
has — and with two apps, the wrong asset is the other application. Without that
check a mistake in `dev.json` would turn somebody's dev app into the release
build living at the dev app's path, with the dev app's port and data directory.

### Nothing is trusted because of where it came from

The manifest is a file on a web server. So:

- the asset URL must be an HTTPS GitHub release asset **of this repository** —
  compared after parsing, never by prefix, because `github.com.evil.test` starts
  with the right characters;
- redirects must stay inside GitHub, which is where the real asset redirect
  goes;
- unknown JSON fields are refused outright, so a future field cannot be
  half-understood by an old client;
- the download is capped at the size the manifest declared, enforced while
  reading, so a manifest that lies cannot fill the disk before being caught.

And because any local process can post to `/update`, a version that reaches a
menu title must look like a version, and a URL that reaches a browser must be an
HTTPS github.com URL in this repository. Both are checked at the boundary and
again at the point of use.

### The swap, and the port

CLAUDE.md's first entry under *Things that will waste your time* is a stale
`petd` holding port 9876 while a rebuild appears to do nothing. An updater walks
straight into it, so:

- the running app is quit through its own menu item, and then the **port is
  polled** until it is actually free — not slept on;
- `petctl` copies itself out of the bundle and re-execs before touching it, the
  way Sparkle copies its helper, because a process running from inside a bundle
  cannot safely delete it;
- the old bundle is renamed aside and only removed once the new one has landed,
  so a failure halfway through is recoverable — and is recovered, not left;
- afterwards the new app must answer `/healthz` **with the version that was just
  installed**. Without that second half, an update that silently left the old
  daemon running would report success.

### Automatic checks are on by default

`update.check` is `true` in a fresh config. It was `false` first, on the
reasoning that an app which has never contacted a server should not start
because it was upgraded — but a fix nobody hears about is not a fix, and the
check reads a version number and nothing else. Nothing is downloaded or
installed without the user asking, and `petctl update --check` works either way,
because typing it is consent.

The default only ever reaches an installation that has no answer of its own.
`config.yaml` is written back in full on every quit, so somebody who turned
checks off has that on disk and no upgrade may talk them round.

The check is started from the `SessionStart` hook — at most once a day, in a
detached process, never waited for. The hook has a budget measured in
milliseconds and a network call does not fit in it.

## What was considered and not done

**Sparkle.** The right default for most Mac apps. Here it would mean embedding
`Sparkle.framework` into a Wails bundle with no first-class support for it,
reordering the signing in `notarize.sh` so frameworks and their XPC services
sign first, a cgo bridge to `SPUUpdater`, and an EdDSA keypair as a second trust
root. What it buys is delta updates and a privileged install path for users who
cannot write to `/Applications`. Neither matters yet for a cosmetic app of about
30 MB installed by an admin user. If either starts to, Sparkle goes behind the
same `petctl update` command.

**Installing from the menu bar.** The status item shows a version and opens the
release page; it does not install. The original reason — that it would mean
`petd` spawning a subprocess — no longer stands on its own, now that `petd`
spawns one to check. What stands is the difference between the two acts:
checking reads a small JSON file and changes nothing, while installing replaces
the application, and a menu item that replaces an application on a single click
wants more thought than "the process was already allowed to fork". The audience
for this app is also one whose agent can run `petctl update` for them.

**A channel switch.** Covered above: it was built, and then deleted in favour of
two apps. The code that went is worth naming so it does not come back — a
channel submenu, a setting in `config.yaml`, discard-on-switch logic in the
event API, a check-timer reset, and an "ahead of this channel" state that
existed only to explain a downgrade the updater would refuse to perform anyway.

**A recall.** Reverting a manifest stops the offer for anyone who has not taken
it yet. It cannot pull it back from anyone who has, because the updater will not
move a version backwards. If a release turns out to be bad, the fix is the next
release. A `yanked` list is a thing to add when it is needed, not before.
