---
description: Update the Agent Pet macOS app to the newest published version, or report that it is already current. Use when the user asks to update the pet, check for a new version, or says the pet is out of date.
disable-model-invocation: true
---

# Update the Agent Pet app

The app updates itself. Your job is to ask whether there is a newer version,
tell the user what you found, and run one command if they want it.

**Do not do this by hand.** Do not download a release, unzip it over
`/Applications`, or move a bundle yourself. `petctl update` already handles the
parts that are easy to get wrong and expensive to get wrong: quitting the old
daemon and waiting for it to actually let go of the event port, verifying that
what arrived is signed by the same developer and is the same application, and
putting the old bundle back if the swap fails halfway.

The pet is cosmetic. Nothing here is worth taking a risk for — if a step fails,
stop and say so rather than working around it.

## Hard rules

**Never run `xattr -d com.apple.quarantine`, `spctl --master-disable`, or
`codesign --force`.** Releases are signed and notarized precisely so nobody
needs them. A refusal from macOS is a finding to report, not an obstacle to
route around.

**Never pass `--allow-dev`, edit a manifest, or point the updater somewhere
else.** There is no flag that makes a failed check pass, and inventing one is
not the answer to a check that failed.

## Steps

### 1. Find out what is installed and what is available

```bash
petctl doctor
```

The **Apps** section lists both applications — `Agent Pet` and
`Agent Pet (dev)` — with the version of each and whether it is running. Note
which ones this machine actually has.

If it says the app is not installed at all, this is not the skill you want:
use `/agent-pet:install`.

```bash
petctl update --check --json
```

That prints the answer as JSON: `available`, `current`, `channel`, and `latest`
— which is **absent** when nothing has been published on that channel, rather
than empty. It reaches `raw.githubusercontent.com` for a small manifest and
nothing else.

### 2. Tell the user, and wait

Say the version they have and the version on offer. **Then stop and ask.**

Updating **quits the running pet**, so it is not a silent operation, and there
is no reason to spend somebody's afternoon on a cartoon cat without being asked
to. Do not run step 3 on a `"available": true` alone.

If `available` is `false`, say which of these it is rather than "up to date":

- **Already current** — `latest` equals `current`. Nothing to do.
- **Nothing published on this channel yet** — the check says so in words.
- **Ahead of the channel** — the user is on a newer build than the channel
  offers. Normal for somebody running a prerelease. The updater will not move
  them backwards, and should not.
- **This is a dev build** — `current` is `"dev"`, meaning they built it
  themselves. It is never replaced over the air. Say so and stop.

### 3. Run the update

```bash
petctl update
```

It prints what it is doing at each step: downloading, verifying the signature,
quitting the running pet, installing, starting the new version. Let it finish —
the last step waits for the new app to answer on the event port **with the
version that was just installed**, which is the only thing that distinguishes a
real update from one where the old daemon never let go.

Expect it to take a minute or so. Most of that is the download.

### 4. Prove it worked

```bash
petctl doctor
```

Check the version actually changed. Then make the pet visibly do something, so
the user confirms with their eyes rather than trusting your summary:

```bash
petctl test celebrate --for 5s
```

Ask whether they saw it. If they did not, go to `/agent-pet:troubleshoot`.

Hooks are unaffected by an update — they come from this plugin, not from the
app, so nothing needs reinstalling and no session needs restarting.

## Updating the dev app

If `petctl doctor` lists **both** applications, plain `petctl update` acts on
the release one: `petctl` resolves through the release bundle first, and each
app only ever updates itself. To update the other one, name it:

```bash
AGENT_PET_APP=/Applications/agent-pet-dev.app petctl update
```

Neither app can update into the other. They are separate applications with
separate settings, and an update carrying the wrong bundle identifier is
refused — so if the user wants to move between them, that is an install, not an
update.

## When it refuses

Report what it said and stop. Quote the command's own output rather than
summarising it: these messages name the specific check that failed.

| What it says | What it means |
|---|---|
| `this is a dev build; refusing to replace it` | They built it from source. Nothing to do. |
| `cannot read the signature of the installed app` | Also a build from source, installed by hand. It has no signature to compare against, so reinstall from a release first with `/agent-pet:install`. |
| `the update is signed by team X, the installed app by Y` | The download is not from the same developer. **Do not install it.** Report it and stop. |
| `the download does not match the manifest` | Usually a truncated download. One clean retry is reasonable. |
| `macOS will not accept the downloaded app` | Gatekeeper refused it. Report the output verbatim. |
| `the pet is still holding 127.0.0.1:9876` | The old app did not quit. Ask the user to quit it from the menu bar, then retry. Nothing was changed. |
| `cannot stage an update next to …` | No write access to `/Applications`. The user needs to install it themselves, or move the app somewhere they own. |
| `the pet answering on the event port is a different copy` | The bundle was replaced, but another build — often one left in a project's `build/bin` — holds the port, so the new app exited on the single-instance lock. The message names its path. Quit that one and open the installed app. Nothing is wrong with the download. |

A signature or team failure means the file is not what the author published, or
the download was corrupted, and neither you nor the user can tell those apart
from the output. Leave it uninstalled and let them decide.
