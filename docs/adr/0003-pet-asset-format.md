# ADR 0003 — Pet assets are PNG sprite strips described by a manifest

Status: accepted (Milestone 1)

## Context

§11 suggests one animated file per state (animated WebP or GIF). §12 requires
that the runtime only ever touch local assets — no image API at animation time.

## Decision

Each state is one **horizontal PNG strip**: N frames of equal size laid out
left to right, with alpha. A `manifest.json` describes frame size, frame count,
frame rate and looping per state.

Why strips instead of animated WebP:

- The renderer animates them with a single CSS `steps()` keyframe on
  `background-position`. No decoder, no JS timer per frame, no `requestAnimationFrame`
  loop. The compositor does the work, so idle CPU stays near zero.
- Frame-accurate control: we can pause, slow down, or freeze on a frame when the
  window is hidden or the pet is asleep. Animated WebP gives none of that.
- Trivially generated and diffed. `tools/genpets` produces them deterministically.
- Universal support, transparency, small files at these sizes (a 4-frame 48×48
  strip is under 2 KB).

The manifest accepts either form for each animation, so hand-authored packs that
follow the shape in §11 still load:

```json
"idle": "idle.png"
"idle": { "file": "idle.png", "frames": 4, "fps": 4, "loop": true }
```

A bare string means one frame at 1 fps unless the image dimensions imply more.

## Asset locations

- **Built-in pets** are embedded in the binary under `ui/dist/pets/<id>/` and
  served by the Wails asset server. Nothing to install, nothing to lose.
- **User pets** live on disk at `~/.local/share/digital-pet/pets/<id>/` and are
  served by a custom asset handler. This is where the Milestone 5 character
  pipeline writes its output, and where a user can drop a hand-made pack.

Disk pets shadow built-ins with the same id, so a built-in can be overridden.

## Consequences

- A pet pack is a directory of PNGs plus one JSON file. No build tooling needed
  to make one.
- Missing states fall back through a documented chain (see `internal/petassets`),
  so an incomplete pack still runs.
