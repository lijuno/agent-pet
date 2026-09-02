package desktop

import "fmt"

// The window is a transparent rectangle much larger than the character, and it
// takes mouse events across all of it — including the parts that show nothing.
// At the sizes the menu offers that is 108 to 60 points either side and 56
// above, and a click there reaches neither the app behind nor, visibly, the pet:
// a single click on the character is defined to do nothing. See ADR 0009.
//
// The fix is to hand those points back, by telling the window to ignore mouse
// events whenever the cursor is outside the parts that are actually used. This
// file decides which parts those are. It is deliberately the whole of the
// policy: everything else in the feature is native, main-thread and invisible
// to `go test`, so as much of the decision as can be tested lives here and the
// cgo beside it is told rectangles rather than trusted to work them out.
//
// Rectangles rather than the sprite's alpha, though the alpha is right there in
// the PNGs. The sprite animates at 2 to 10 frames a second, so a per-pixel
// region would flicker at the character's edge several times a second and a
// click could land in a hole that was solid in the frame the eye last saw.
// Precision the user cannot predict is not precision.

// hitRegion returns the rectangles, in window coordinates, over which the
// window should take mouse events. Everywhere else the click belongs to
// whatever is behind.
//
// winW and winH are the window's current size, which is the character-only
// size until an overlay grows it. petX is the character's centre measured from
// the window's left edge — normally half the window, but not when the window
// had to slide sideways to keep an overlay on screen. side is the overlay's
// side, which moves the character: the frontend pins it to the bottom of the
// window normally, and to the top when the overlay is drawn below it.
//
// The character's rectangle is the sprite box, not an inset of it. The box has
// transparent corners and insetting would reclaim a few more points, but the
// cost of being wrong is a character that cannot be picked up, and there is no
// number here that is right for six different silhouettes.
func hitRegion(winW, winH, sprite, petX int, side string, overlay rect, hasOverlay bool) []rect {
	if winW <= 0 || winH <= 0 || sprite <= 0 {
		return nil
	}

	pet := rect{X: petX - sprite/2, W: sprite, H: sprite}
	if hasOverlay && side == "below" {
		// body.overlay-below #pet { bottom: auto; top: 56px }
		pet.Y = bubbleRoom
	} else {
		// #pet { bottom: 8px }
		pet.Y = winH - shadow - sprite
	}

	out := []rect{clampToWindow(pet, winW, winH)}
	if hasOverlay && overlay.W > 0 && overlay.H > 0 {
		out = append(out, clampToWindow(overlay, winW, winH))
	}
	return out
}

// clampToWindow trims a rectangle to the window. A region reaching past the
// window is not wrong — the character is pinned to a corner and the arithmetic
// can run over by a point — but a native hit test given coordinates outside the
// window it belongs to is a question with no useful answer.
func clampToWindow(r rect, winW, winH int) rect {
	if r.X < 0 {
		r.W += r.X
		r.X = 0
	}
	if r.Y < 0 {
		r.H += r.Y
		r.Y = 0
	}
	if r.X+r.W > winW {
		r.W = winW - r.X
	}
	if r.Y+r.H > winH {
		r.H = winH - r.Y
	}
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	return r
}

// contains reports whether a window-relative point falls in any rectangle.
// The native side asks this on every mouse-moved event it sees, so it stays
// arithmetic: no allocation, no locking, no calls out.
func containsPoint(rs []rect, x, y int) bool {
	for _, r := range rs {
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return true
		}
	}
	return false
}

// HitRegion is the region as the app currently understands it. Published on
// /diagnostics so `make test-desktop` can drive the character into a corner,
// open a panel, and read back what the app believes — which is the most that
// can be checked without looking at the screen. Whether a click actually
// reached the Finder is not knowable from here and never will be.
func (a *App) HitRegion() []rect {
	a.mu.Lock()
	overlay, has := a.overlay, a.hasOverlay
	petX, side := a.petX, a.petSide
	winW, winH := a.curW, a.curH
	a.mu.Unlock()

	if petX <= 0 {
		petX = winW / 2
	}
	return hitRegion(winW, winH, spriteSize(a.eng.Config().Pet.Scale), petX, side, overlay, has)
}

// publishHitRegion hands the current region to the native side. Called wherever
// the geometry moves — a panel opening or closing, a size change — and never
// from a mouse callback: the native side keeps its own copy precisely so that
// the thing running on every mouse-moved event does no work but arithmetic.
func (a *App) publishHitRegion() {
	if !a.clickThrough {
		return
	}
	setHitRegion(a.HitRegion())
}

// hitRegionReport is what /diagnostics shows, so `make test-desktop` can put the
// character in a corner, open a panel and read back what the app believes its
// clickable area is. It is not proof that a click reached the Finder — nothing
// available here can be — but it is the difference between a wrong rectangle
// and a mystery.
func (a *App) hitRegionReport() string {
	if !a.clickThrough {
		return "off — the window takes clicks across its whole rectangle"
	}
	where := "cursor is over it"
	if isIgnoringMouse() {
		where = "cursor is outside it, clicks passing through"
	}
	rs := a.HitRegion()
	if len(rs) == 0 {
		return "none"
	}
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += " + "
		}
		out += fmt.Sprintf("%dx%d at %d,%d", r.W, r.H, r.X, r.Y)
	}
	return out + " — " + where
}
