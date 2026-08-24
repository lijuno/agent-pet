# The `agent-pet` Claude Code plugin

The front door. Installing this plugin is how a user gets the pet.

```
/plugin marketplace add lijuno/agent-pet
/plugin install agent-pet@lijuno
/agent-pet:install
```

Later, when there is a newer version:

```
/agent-pet:update
```

Both are user-invoked only. Neither will run because an agent decided the
moment was right: installing and updating both replace an application and the
second one quits the pet while somebody is working.

## What is in here, and why

| Path | Purpose |
|---|---|
| `hooks/hooks.json` | The ten Claude Code hooks, each running `petctl hook claude` |
| `bin/petctl` | A shim resolving `petctl` inside the installed app bundle |
| `skills/install/` | `/agent-pet:install` — downloads, **verifies**, installs the app |
| `skills/update/` | `/agent-pet:update` — moves an installed app to the newest published version |
| `skills/troubleshoot/` | `/agent-pet:troubleshoot` — diagnoses a pet that is not reacting |

The plugin is small and travels over git. The app is a signed download from
GitHub releases, because an `.app` bundle has no business in a repository.
That split is the whole design.

**The hooks ship here rather than being written into the user's
`settings.json`.** `petctl install claude` still exists for people not using
the plugin, but this path never edits anyone's settings file and never bakes
an absolute path into it — which means it also never goes stale when the app
moves. If a user has both, they will get two events per hook; `petctl doctor`
reports the hooks it finds in `settings.json` so the duplicate is visible.

**`bin/petctl` is a shim, not the binary.** A universal build is several
megabytes, and `bin/` is distributed over git — committing it would add
another copy to history on every release. Resolving through the bundle also
guarantees `petctl` and the `petd` it talks to are the same build.

It deliberately does not search `$PATH`: this directory is on `$PATH` while
the plugin is enabled, so a lookup would find the shim and re-exec itself
forever.

## Regenerating the hooks

`hooks/hooks.json` is generated from `adapters/claude/translate.go`, which is
the source of truth for which hooks the adapter understands:

```bash
make plugin-hooks
```

Run it after changing `Hooks` there, or the plugin and the adapter drift.

## Testing it without installing

```bash
claude --plugin-dir ./plugin
```

Then `/agent-pet:install`, or check the hooks fired with `petctl watch`.
