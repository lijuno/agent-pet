---
description: Diagnose a Agent Pet that is not reacting, not visible, or stuck on an old version. Use when the user says the pet is not responding, not showing up, or did not update.
---

# The pet is not doing what it should

Work down this list in order. Each check is cheap, and the causes are ordered
by how often they are the real one.

Start with:

```bash
petctl doctor
```

## The pet does not react to anything

**Is the app running?** `petctl doctor` says so. If it is not installed at
all, use `/agent-pet:install`.

**Were the hooks loaded?** Hooks are read when a session starts. A session
that was already open when the plugin was installed does not have them.
`/reload-plugins` picks them up; otherwise a new session will.

**Is the pipe intact?** This bypasses the hooks and pokes the app directly:

```bash
petctl event claude permission_requested
```

If the pet reacts to that but not to real activity, the app is fine and the
hooks are the problem — go back to the previous check. If it reacts to
neither, the app is not receiving events at all.

**Watch live.** `petctl watch` prints events as they arrive, which settles
whether the hooks are firing.

## The pet reacts, but is invisible

It may be off-screen, hidden, or behind something. The menu-bar item can show
it again. Note that the window is deliberately transparent and click-through
in places, so "I cannot click it" is expected, not a fault.

## An update appeared to do nothing

This is almost always the same cause: **the old app was still running.**

The app holds a local event port. A second copy that starts while the first
still holds it exits immediately instead of reporting a conflict, so the
update looks like it succeeded while the old version keeps running. `petctl`
then talks to the old one and reports the old version.

Quit the app completely, confirm nothing is left, then start the new one:

```bash
pkill -f 'MacOS/petd'; sleep 1; open "/Applications/agent-pet.app"
```

Then confirm the version actually changed:

```bash
petctl doctor
```

If it still reports the old version, either the new bundle did not land in
`/Applications` — re-run `/agent-pet:install` — or the pre-rename
`digital-pet.app` is still installed and winning the race for the port. Remove
it; the config and pet packs it stored are moved across on first run.

## The plugin and the app disagree

They update independently: the plugin comes over git with
`/plugin marketplace update`, the app is a signed download. So a plugin can be
newer than the app it is driving.

`petctl doctor` reports both versions. If the app is older than the plugin
expects, update the app with `/agent-pet:install` — new hooks can send events
an older app does not recognise. Unknown events are ignored rather than
treated as errors, so the failure looks like a pet that is missing some
reactions, not one that is broken.

## What not to do

Do not disable Gatekeeper, strip quarantine attributes, or re-sign the bundle
to make something work. If the app will not launch because macOS rejects it,
that is the finding — report it to the user rather than working around it.
