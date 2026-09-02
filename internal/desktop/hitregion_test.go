package desktop

import (
	"testing"
)

// The numbers in this file are the ones ADR 0009 was written from: at the three
// sizes the menu offers, the window stays 300 wide, so the dead zone is widest
// around the smallest character.
func TestHitRegionLeavesTheMarginToWhateverIsBehind(t *testing.T) {
	for _, c := range []struct {
		name       string
		scale      float64
		wantSide   int // transparent points either side of the character
		wantAbove  int // transparent points above it
		wantSprite int
	}{
		{"Small", 0.7, 108, 56, 84},
		{"Medium", 1.0, 90, 56, 120},
		{"Large", 1.5, 60, 56, 180},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, h := WindowSize(c.scale)
			sprite := spriteSize(c.scale)
			if sprite != c.wantSprite {
				t.Fatalf("sprite %d, want %d", sprite, c.wantSprite)
			}

			rs := hitRegion(w, h, sprite, w/2, "", rect{}, false)
			if len(rs) != 1 {
				t.Fatalf("with no overlay open the region is the character alone, got %d rects", len(rs))
			}
			pet := rs[0]

			if pet.X != c.wantSide {
				t.Errorf("character starts at x=%d, want %d — that is the margin handed back", pet.X, c.wantSide)
			}
			if right := w - (pet.X + pet.W); right != c.wantSide {
				t.Errorf("margin to the right is %d, want %d", right, c.wantSide)
			}
			if pet.Y != c.wantAbove {
				t.Errorf("character starts at y=%d, want %d", pet.Y, c.wantAbove)
			}
			if bottom := h - (pet.Y + pet.H); bottom != shadow {
				t.Errorf("gap under the character is %d, want %d", bottom, shadow)
			}
		})
	}
}

// A point in the margin belongs to whatever is behind; a point on the character
// does not. This is the whole behaviour, stated once.
func TestHitRegionAnswersForPointsEitherSide(t *testing.T) {
	w, h := WindowSize(1)
	rs := hitRegion(w, h, spriteSize(1), w/2, "", rect{}, false)

	for _, c := range []struct {
		name string
		x, y int
		want bool
	}{
		{"middle of the character", w / 2, h - shadow - 10, true},
		{"just inside its left edge", 90, h - shadow - 10, true},
		{"one point left of it", 89, h - shadow - 10, false},
		{"just inside its right edge", 209, h - shadow - 10, true},
		{"one point right of it", 210, h - shadow - 10, false},
		{"the bubble's room above", w / 2, 10, false},
		{"the shadow gap below", w / 2, h - 1, false},
		{"the top-left corner", 0, 0, false},
	} {
		if got := containsPoint(rs, c.x, c.y); got != c.want {
			t.Errorf("%s (%d,%d): in region = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

// An open panel is as clickable as the character. Without this the menu would
// be unusable the moment the window learned to ignore anything.
func TestHitRegionIncludesAnOpenOverlay(t *testing.T) {
	w, h := WindowSize(1)
	overlay := rect{X: 30, Y: 4, W: 240, H: 120}
	rs := hitRegion(w, h, spriteSize(1), w/2, "above", overlay, true)

	if len(rs) != 2 {
		t.Fatalf("character and overlay make two rects, got %d", len(rs))
	}
	if !containsPoint(rs, 40, 20) {
		t.Error("a point on the open panel must be clickable")
	}
	if containsPoint(rs, 10, 20) {
		t.Error("a point beside the panel, on nothing, must not be")
	}
}

// When the overlay opens below, the frontend moves the character to the top of
// the window. A region still measuring from the bottom would leave the pet
// undraggable and hand clicks on it to the desktop.
func TestHitRegionFollowsTheCharacterWhenTheOverlayOpensBelow(t *testing.T) {
	sprite := spriteSize(1)
	winH := 400 // grown by an overlay
	rs := hitRegion(300, winH, sprite, 150, "below", rect{X: 30, Y: 200, W: 240, H: 120}, true)

	pet := rs[0]
	if pet.Y != bubbleRoom {
		t.Fatalf("character sits at y=%d, want %d — body.overlay-below pins it to the top", pet.Y, bubbleRoom)
	}
	if containsPoint(rs, 150, winH-shadow-10) {
		t.Error("the foot of the window is empty when the overlay is below; it must not be clickable")
	}
}

// The character is pinned to a corner and the arithmetic can run past the edge.
// A native hit test given a rectangle outside its own window is a question with
// no useful answer.
func TestHitRegionStaysInsideTheWindow(t *testing.T) {
	sprite := spriteSize(1)
	rs := hitRegion(300, 184, sprite, 5, "", rect{}, false) // character pushed hard left

	for _, r := range rs {
		if r.X < 0 || r.Y < 0 || r.X+r.W > 300 || r.Y+r.H > 184 {
			t.Errorf("rect %+v escapes the 300x184 window", r)
		}
	}
}

// A window with no size yields no region rather than a rectangle of nonsense.
// The monitor runs before the first frame is placed.
func TestHitRegionIsEmptyBeforeThereIsAWindow(t *testing.T) {
	if rs := hitRegion(0, 0, 0, 0, "", rect{}, false); len(rs) != 0 {
		t.Errorf("want no region before there is a window, got %+v", rs)
	}
	if containsPoint(nil, 10, 10) {
		t.Error("no region contains nothing")
	}
}
