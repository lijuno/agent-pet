# ADR 0009 — Click-through by rectangle union

Status: accepted

The window is a frameless transparent rectangle, and every pixel of it takes
mouse events — including the pixels that show nothing. At the default scale the
window is 300x184 around a character 120x120, so there are **90 points of
invisible dead zone to either side of the character and 56 above it**. A click
there does not reach the window behind, and it does not visibly reach the pet
either: a single click on the character is defined to do nothing, so the dead
zone gives no feedback of any kind. It reads as the desktop ignoring you.

The margins themselves are wanted. `bubbleRoom` keeps a two-line bubble on
screen when the character is near the top of a display, and the 300-point floor
on the width exists because the bubble and the panels are up to 240 wide while
the character is 120. Nothing about them is a mistake. What is missing is any
mechanism to say that a transparent margin should not swallow a click.

The dead zone is **worst at the smallest size**, which is the opposite of the
guess. The width is `max(300, sprite + 80)`, and the menu offers 0.7, 1.0 and
1.5 — so the `sprite + 80` arm never runs, the window stays 300 wide at every
size a user can pick, and shrinking the character only widens the margin around
it:

| Size   | scale | sprite | window  | each side | above |
|--------|-------|--------|---------|-----------|-------|
| Small  | 0.7   | 84     | 300x148 | 108       | 56    |
| Medium | 1.0   | 120    | 300x184 | 90        | 56    |
| Large  | 1.5   | 180    | 300x244 | 60        | 56    |

The floor is not wrong — the bubble and the panels need it whatever size the
character is. It is only the input that should not have followed the layout.
(`config.yaml` accepts a scale up to 6 by hand, and past about 1.84 the other
arm does take over and the margin settles at 40. Nothing in the menu goes
there.)

Four constraints shape everything below.

1. **macOS has no shaped-window API.** There is no `SetWindowRgn`. Overriding
   `hitTest:` only redirects an event inside our own process; it cannot hand a
   click to the application behind.
2. **`NSWindow.ignoresMouseEvents` is the only lever, and it is
   all-or-nothing.** A window either takes mouse events or it does not. There is
   no per-region form of it.
3. **Wails v2 offers neither.** It exposes no mouse-ignoring option and no
   second window — both checked in the vendored source, not assumed. A design
   that needs either has to go around Wails.
4. **This repository refuses accessibility access.** `make test-desktop` exists
   because nothing here may read a macOS menu bar. Any design that needs that
   permission is not available.

## The decisions

### The hit region is a union of rectangles, not a pixel mask

The window is interactive over the character's rectangle, plus the overlay's
rectangle while a panel or menu is open, and click-through everywhere else.

The tempting alternative is the sprite's own alpha — per-pixel, exact, and the
data is already to hand, since `sprite_rules_test.go` reads sprite pixels
today. It is the wrong shape for this problem. The sprite animates at 2 to 10
frames a second, so the silhouette moves continuously: a wagging tail or an arm
raised to celebrate would make the edge of the character flicker between
clickable and not, several times a second. Worse, a click could land in a hole
that was solid in the frame the eye last registered. Precision the user cannot
predict is not precision.

A rectangle around a 120-point character captures effectively all of the
benefit and behaves the same way in every frame.

It is the sprite box exactly, not an inset of it. The box has transparent
corners and an inset would reclaim a few more points, but the cost of being
wrong in that direction is a character that cannot be picked up, and there is no
single number that is right for a cat, a bicycle and a pickaxe.

### The geometry is computed in Go, from state that already exists

The region is derivable now, with no new state to thread anywhere:

- `Placement.PetX` — the character's centre measured from the window's left
  edge, already maintained even when the window is pushed sideways to keep an
  overlay on screen.
- The sprite size, from the same `40 * 3 * scale` the window sizing uses.
- The `bottom: 8px` pin, and its flip to `top: 56px` when the overlay opens
  below instead of above.
- `a.overlay` and `a.hasOverlay`, which already carry the overlay's rectangle.

So this is one pure function — window and placement in, a slice of rectangles
out — and no I/O.

That split is the point of the design rather than a tidiness preference.
Everything else in this feature is native, main-thread and untestable by the Go
suite; CLAUDE.md is explicit that a crash in that territory has shipped before
and that `go test` will not catch it. So as much of the decision as possible
belongs in the part that a test can reach, and the cgo should be as close to
dumb as it can be made: it is told a set of rectangles and a cursor position,
and it sets a flag.

### The toggle is cgo, beside the status item, and knows nothing

A small `_darwin` file calls `setIgnoresMouseEvents:` on the Wails window. It
holds no policy. Three things it does have to get right:

- **Both monitors.** `addGlobalMonitorForEvents` sees mouse-moved events going
  to other applications; `addLocalMonitorForEvents` sees our own. Neither alone
  is enough — while we are ignoring events the local monitor goes quiet, and
  while we are not the global one does.
- **Only on transition.** Flipping the flag on every mouse-moved event is a
  great deal of work to reach the same state it was already in.
- **Never mid-drag.** `#pet` carries `--wails-draggable: drag`, so the window
  moves when a drag starts on the character. If the flag flips while the cursor
  is briefly outside the region during a drag, the user drops the pet.

**The assumption this rested on, now checked.** A mouse-moved global monitor
does not require accessibility access. Keyboard monitors do; mouse ones do not.
This was tested twice: first with a throwaway program, which established that
the monitor is fed by AppKit's own dispatch and needs `[NSApp run]` rather than
a hand-spun `NSRunLoop` — two earlier attempts saw nothing and it was the run
loop, not a permission — and then in the app itself, which uses no accessibility
API, has never prompted for one, and is not in the Accessibility list. Moving
the cursor onto the character and back into the margin flips the window between
taking clicks and passing them through. The design stands.

### Everything native hops to the main queue, because startup is not on it

Wails runs `OnStartup` on a goroutine. The first working version called
`addGlobalMonitorForEventsMatchingMask:` straight from it and trapped
immediately — `signal arrived during cgo execution`, in `startClickThrough`,
under `frontend.Run.func1`. AppKit is main-thread-only and NSEvent monitors are
no exception.

So every entry point in the cgo file is a `dispatch_async` onto the main queue
and nothing else. The region is copied before that hop rather than inside it:
the caller owns that memory, and Go is free to reuse it the moment the call
returns, which is long before a queued block runs.

This is the second entry in this repository's list of ways the main thread will
kill the app, after "never call the Wails runtime from a native menu callback".
It is worth reading them as one rule: anything native either runs on the main
thread or arranges to.

### It ships behind a setting, off no earlier than it has been driven by hand

The failure mode is not a wrong pixel, it is a pet that cannot be picked up. A
plain key in `config.yaml`, read at load. It does not belong in
`config.SaveOwned` — that is for the five settings the menu changes, and this is
not one of them.

### The region is published on `/diagnostics`

`make test-desktop` can then assert what the app believes its hit region is,
against a character it has just placed. That is worth having and it is not the
same as knowing a click reached the Finder, which nothing here can check. **One
manual verification remains, permanently.** Saying so in this document is
cheaper than rediscovering it during a release.

## Consequences

**Window size stops governing input.** This is the real prize. The window can
keep whatever generous rectangle the bubble and the panels need, and the
interactive area is decided separately. The idea of shrinking the window to the
character when nothing is showing becomes unnecessary rather than complementary.

**The bubble stops blocking clicks even while it is showing.** It is already
`pointer-events: none` inside the page, so its rectangle can stay outside the
hit region — an improvement shrinking could never deliver, since the window has
to be large exactly when a bubble is up.

**A new failure mode replaces an old one.** If the monitors die or the region is
computed wrongly, the symptom is either the dead zone returning or a pet that
cannot be dragged. The setting is the escape hatch, and the reason there is one.

**A closed overlay has to leave the region.** `CloseOverlay` did not clear the
overlay rectangle — nothing had needed it to, because the field existed only for
diagnostics. Left in, the window went on taking clicks over a panel that was no
longer there: the dead zone this feature removes, reintroduced in the shape of
the last menu the user opened. Found by the desktop check below rather than by
reading, which is the argument for having written it.

**A mouse-moved monitor fires often.** The work per event must stay at a few
rectangle comparisons. Nothing about this may allocate or lock.

**`internal/desktop` gains a second cgo file.** CLAUDE.md's rule follows it: a
test touching anything declared there belongs in `app_darwin_test.go`, or
`go test ./...` stops building for anybody working on Linux, with an error that
explains nothing.

**README.md needs a correction either way.** Its troubleshooting section
currently says the window is "deliberately transparent and click-through in
places, so 'I cannot click it' is expected". No such behaviour exists. The
sentence describes this ADR before it was written, and today it tells anybody
hitting the dead zone that the app is working as intended.

## What was considered and not done

**A CSS mask.** `clip-path`, `mask-image` and `pointer-events` govern routing
inside the page. The webview is a layer-backed `NSView` that hit-tests its full
bounds whatever alpha it renders, and the window's mouse region is not derived
from pixels. This is exactly why 100%-transparent margins swallow clicks today.
Named here so it does not get proposed again: it is not that a CSS mask is a
worse mask, it is that it cannot affect another application at all.

**Per-pixel alpha.** Covered above. The objection is not cost, it is that a
moving silhouette makes the clickable edge unpredictable.

**Shrinking the window to the character.** Cheaper, needs no cgo, and was the
first idea. It only helps while nothing is showing — the moment a bubble or a
panel opens, the window must grow again and the dead zone comes back, which is
also the moment the user is most likely to be clicking near the pet. It keeps
input coupled to layout, so every future overlay inherits the problem. Worth
keeping in mind as the fallback if the accessibility assumption above turns out
to be wrong.

**Two windows — one sized to the character, one for overlays.** No mask needed
at all, and each window is exactly its content. Wails v2 has no secondary-window
API, so this means leaving Wails' window management for raw AppKit, in the one
package that is already the hardest to test. Not for a dead zone.

**Documenting it instead of fixing it.** This is the current state, and the
documentation is wrong, which is a fair indication of how well it works.
