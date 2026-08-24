# The update manifests

Two files decide what every installed pet is offered:

| File | Which app reads it |
|---|---|
| `release.json` | `Agent Pet` — `agent-pet.app` |
| `dev.json` | `Agent Pet (dev)` — `agent-pet-dev.app`, a separate install |

The two are separate applications rather than one app with a channel setting, so
a build reads one of these files and can never read the other. Which one it
reads is decided when it is built (`internal/flavor`), and a manifest naming the
wrong asset is refused on the way in: an update must carry the same bundle
identifier as the app it replaces.

`petctl update --check` fetches one of them over HTTPS from
`raw.githubusercontent.com` and compares its `version` against the running
build. Nothing else in the program looks at GitHub — the daemon has no HTTP
client in it at all.

## Why these are files and not the releases API

Because a release existing and a release being offered should be two different
events.

`GET /releases/latest` would make them the same one: attach an asset and every
machine is offered it on its next check. With a manifest, `make release` builds,
notarizes and uploads — and then stops, leaving the file below modified and
uncommitted. You can install it yourself, live with it for a few days, and only
then commit. **The commit is the release.**

It also means an offer has a diff, a commit message saying why, and `git revert`
as the undo. See [ADR 0008](../docs/adr/0008-over-the-air-updates.md).

## The shape

```json
{
  "channel": "release",
  "version": "0.2.0",
  "url": "https://github.com/lijuno/agent-pet/releases/download/v0.2.0/agent-pet-0.2.0-universal.zip",
  "sha256": "…64 hex characters…",
  "size": 31457280,
  "min_macos": "12.0",
  "published": "2026-08-24",
  "notes_url": "https://github.com/lijuno/agent-pet/releases/tag/v0.2.0"
}
```

Write it with `make release`, never by hand: the hash and size come from the
artifact that was just stapled, and a hash typed from a terminal is a hash that
can be typed wrong. Every field is validated on the way in — see
`internal/update` — and `.github/workflows/updates.yml` re-checks the whole file
against the real asset whenever one of them changes.

Neither file exists yet. Until one does, `petctl update --check` reports that
nothing has been published, which is true.

`make release` writes the one matching the app it just built, so publishing to
the wrong channel would mean building the wrong app — which `scripts/release.sh`
refuses by asking the binary what it is.

## Undoing one

Reverting the commit stops the offer for everyone who has not taken it yet. It
does **not** pull it back from anyone who already updated: the updater compares
versions and will not move somebody backwards. If a release turns out to be
bad, the fix is the next release.
