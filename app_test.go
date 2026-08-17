package main

import "testing"

// The window must be no taller than the character needs. Anything more is
// transparent dead space above the pet, and because macOS keeps a dragged
// window's top below the menu bar, that space is screen the pet can never
// reach.
func TestWindowIsOnlyAsTallAsTheCharacter(t *testing.T) {
	_, h := WindowSize(1)
	sprite := 40 * 3
	if h > sprite+80 {
		t.Fatalf("window is %d for a %d sprite: too much room above the pet", h, sprite)
	}
	if h < sprite {
		t.Fatalf("window is %d, shorter than the %d sprite", h, sprite)
	}
}

func TestWindowFollowsScale(t *testing.T) {
	_, small := WindowSize(0.7)
	_, medium := WindowSize(1)
	_, large := WindowSize(1.5)
	if !(small < medium && medium < large) {
		t.Fatalf("window should grow with the pet: %d, %d, %d", small, medium, large)
	}
	if w, h := WindowSize(0); w <= 0 || h <= 0 {
		t.Fatalf("zero scale produced a %dx%d window", w, h)
	}
}

func TestSizePresets(t *testing.T) {
	for _, s := range Sizes {
		if got := SizeName(s.Scale); got != s.Name {
			t.Fatalf("scale %v should be %s, got %q", s.Scale, s.Name, got)
		}
	}
	// A float that has been through YAML and JSON must still tick its box.
	if got := SizeName(0.7000000001); got != "Small" {
		t.Fatalf("a rounded scale should still match its preset, got %q", got)
	}
	if got := SizeName(1.234); got != "" {
		t.Fatalf("a hand-edited scale belongs to no preset, got %q", got)
	}
}

// base is a window parked in the middle of a 1440x900 screen.
func base() rect { return rect{X: 600, Y: 400, W: 300, H: 184} }

const (
	screenW = 1440
	screenH = 900
)

// The character must not move, whatever the window has to do to fit the panel
// on screen. This is the property the whole placement exists to preserve.
func TestCharacterDoesNotMoveWhereverThePanelGoes(t *testing.T) {
	cases := []struct {
		name string
		b    rect
	}{
		{"middle of the screen", base()},
		{"hard against the top", rect{X: 600, Y: 28, W: 300, H: 184}},
		{"top-left corner", rect{X: 0, Y: 28, W: 300, H: 184}},
		{"top-right corner", rect{X: screenW - 300, Y: 28, W: 300, H: 184}},
		{"hanging off the left", rect{X: -120, Y: 400, W: 300, H: 184}},
		{"hanging off the right", rect{X: screenW - 180, Y: 400, W: 300, H: 184}},
		{"bottom-right corner", rect{X: screenW - 300, Y: screenH - 184, W: 300, H: 184}},
	}
	for _, tc := range cases {
		want := tc.b.X + tc.b.W/2 // the character's centre on screen, before
		out, p := placeOverlay(tc.b, 258, 238, screenW, screenH)
		if got := out.X + p.PetX; got != want {
			t.Fatalf("%s: the character moved from x=%d to x=%d", tc.name, want, got)
		}
	}
}

// A panel that hangs off the side of the screen is the bug this fixes: in a
// corner, the window has to come back on screen even though the pet does not.
func TestWindowIsPulledBackOntoTheScreen(t *testing.T) {
	for _, b := range []rect{
		{X: -120, Y: 400, W: 300, H: 184},
		{X: screenW - 180, Y: 400, W: 300, H: 184},
		{X: 0, Y: 40, W: 300, H: 184},
	} {
		out, _ := placeOverlay(b, 258, 238, screenW, screenH)
		if out.X < 0 {
			t.Fatalf("window starts off the left of the screen at x=%d", out.X)
		}
		if out.X+out.W > screenW {
			t.Fatalf("window ends off the right at x=%d w=%d, screen %d", out.X, out.W, screenW)
		}
	}
}

func TestWindowIsWideEnoughForThePanel(t *testing.T) {
	out, _ := placeOverlay(base(), 258, 238, screenW, screenH)
	if out.W < 258 {
		t.Fatalf("window is %d wide for a 258 panel", out.W)
	}
	// A panel narrower than the character's window must not shrink it.
	out, _ = placeOverlay(base(), 100, 60, screenW, screenH)
	if out.W < base().W {
		t.Fatalf("window shrank to %d for a narrow panel", out.W)
	}
}

func TestPanelOpensUpwardWhenThereIsRoom(t *testing.T) {
	out, p := placeOverlay(base(), 258, 238, screenW, screenH)
	if p.Side != "above" {
		t.Fatalf("plenty of room above, got %s", p.Side)
	}
	// Growing upward means the bottom edge — and the pet on it — stay put.
	if got, want := out.Y+out.H, base().Y+base().H; got != want {
		t.Fatalf("the window's bottom moved from %d to %d", want, got)
	}
}

func TestPanelOpensDownwardAgainstTheTopOfTheScreen(t *testing.T) {
	b := rect{X: 600, Y: menuBarInset, W: 300, H: 184}
	out, p := placeOverlay(b, 258, 238, screenW, screenH)
	if p.Side != "below" {
		t.Fatalf("no room above, got %s", p.Side)
	}
	// Growing downward means the top edge stays.
	if out.Y != b.Y {
		t.Fatalf("the window's top moved from %d to %d", b.Y, out.Y)
	}
}

// With no room on either side, above is still the better answer: it is where
// the panel has always been, and below would be just as clipped.
func TestPanelPrefersAboveWhenNeitherSideFits(t *testing.T) {
	b := rect{X: 600, Y: 40, W: 300, H: 184}
	if _, p := placeOverlay(b, 258, 238, screenW, 200); p.Side != "above" {
		t.Fatalf("want above when nothing fits, got %s", p.Side)
	}
}

// A window wider than the screen has nowhere to be pulled back to; it must not
// end up at a negative x.
func TestPanelWiderThanTheScreen(t *testing.T) {
	out, p := placeOverlay(base(), 2000, 238, 800, screenH)
	if out.X != 0 {
		t.Fatalf("want x=0 for an oversized window, got %d", out.X)
	}
	if p.PetX < 0 {
		t.Fatalf("character placed off the window at %d", p.PetX)
	}
}
