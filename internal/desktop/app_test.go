package desktop

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

// The usable area, in the coordinates Wails reports window positions in.
//
// Those are relative to the screen's visible frame, so the origin is already
// past the menu bar and past a Dock on the left: the area starts at 0,0 and is
// only as big as what is left. This is a 1440x900 display with a 47pt Dock on
// the left and a 30pt menu bar — the machine that reported the bug.
func usable() rect { return rect{W: 1393, H: 870} }

// The display is bigger than the usable area, and using its size as the limit
// is what let a menu hang exactly one Dock-width off the edge.
const displayW = 1440

// The character must not move, whatever the window has to do to fit the panel
// on screen. This is the property the whole placement exists to preserve.
//
// The one exception is a character already outside the usable area, which is
// pulled to the edge rather than left where no window can reach it —
// TestCharacterStaysInsideItsWindow covers that.
func TestCharacterDoesNotMoveWhereverThePanelGoes(t *testing.T) {
	cases := []struct {
		name string
		b    rect
	}{
		{"middle of the screen", base()},
		{"hard against the top", rect{X: 600, Y: 28, W: 300, H: 184}},
		{"top-left corner", rect{X: 0, Y: 28, W: 300, H: 184}},
		{"top-right corner", rect{X: 1093, Y: 0, W: 300, H: 184}},
		{"hanging off the left", rect{X: -120, Y: 400, W: 300, H: 184}},
		{"hanging off the right", rect{X: 1150, Y: 400, W: 300, H: 184}},
		{"bottom-right corner", rect{X: 1093, Y: 686, W: 300, H: 184}},
	}
	for _, tc := range cases {
		want := tc.b.X + tc.b.W/2 // the character's centre on screen, before
		out, p := placeOverlay(tc.b, 258, 238, usable())
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
		{X: 1260, Y: 400, W: 300, H: 184},
		{X: 0, Y: 10, W: 300, H: 184},
	} {
		out, _ := placeOverlay(b, 258, 238, usable())
		if out.X < 0 {
			t.Fatalf("window starts off the left of the screen at x=%d", out.X)
		}
		if out.X+out.W > usable().W {
			t.Fatalf("window ends off the right at x=%d w=%d", out.X, out.W)
		}
	}
}

func TestWindowIsWideEnoughForThePanel(t *testing.T) {
	out, _ := placeOverlay(base(), 258, 238, usable())
	if out.W < 258 {
		t.Fatalf("window is %d wide for a 258 panel", out.W)
	}
	// A panel narrower than the character's window must not shrink it.
	out, _ = placeOverlay(base(), 100, 60, usable())
	if out.W < base().W {
		t.Fatalf("window shrank to %d for a narrow panel", out.W)
	}
}

func TestPanelOpensUpwardWhenThereIsRoom(t *testing.T) {
	out, p := placeOverlay(base(), 258, 238, usable())
	if p.Side != "above" {
		t.Fatalf("plenty of room above, got %s", p.Side)
	}
	// Growing upward means the bottom edge — and the pet on it — stay put.
	if got, want := out.Y+out.H, base().Y+base().H; got != want {
		t.Fatalf("the window's bottom moved from %d to %d", want, got)
	}
}

func TestPanelOpensDownwardAgainstTheTopOfTheScreen(t *testing.T) {
	b := rect{X: 600, Y: 0, W: 300, H: 184}
	out, p := placeOverlay(b, 258, 238, usable())
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
	if _, p := placeOverlay(b, 258, 238, rect{W: 1440, H: 175}); p.Side != "above" {
		t.Fatalf("want above when nothing fits, got %s", p.Side)
	}
}

// A Dock on the left or right shifts the usable area's origin away from zero.
// Clamping to zero instead put menus underneath it, which is what "the menu
// goes off the screen" turned out to mean: 1393x870 at 47,30 on the machine
// that reported it.
func TestPanelStaysClearOfASideDock(t *testing.T) {
	v := usable()
	// Hard against the right of the usable area, and beyond it: the window may
	// not be placed anywhere the display would show but the Dock covers.
	for _, b := range []rect{
		{X: v.W - 300, Y: 400, W: 300, H: 184},
		{X: v.W - 100, Y: 400, W: 300, H: 184},
		{X: displayW - 300, Y: 400, W: 300, H: 184}, // right edge of the display
	} {
		out, _ := placeOverlay(b, 258, 238, v)
		if out.X < 0 {
			t.Fatalf("window starts at %d, off the usable area", out.X)
		}
		if right := out.X + out.W; right > v.W {
			t.Fatalf("window ends at %d, %dpt past the usable %d", right, right-v.W, v.W)
		}
	}
}

// The Dock is not screen the pet can use. Placing against the full display is
// what put a menu behind it, which looks exactly like falling off the bottom.
func TestPanelStaysOutOfTheDock(t *testing.T) {
	// A character parked at the very bottom of the usable area.
	v := usable()
	b := rect{X: 600, Y: v.H - 184, W: 300, H: 184}
	out, p := placeOverlay(b, 258, 238, v)
	if p.Side != "above" {
		t.Fatalf("at the bottom the panel must open upward, got %s", p.Side)
	}
	if bottom := out.Y + out.H; bottom > v.H {
		t.Fatalf("window reaches %d, past the usable area's %d", bottom, v.H)
	}
}

// The character can be dragged mostly off the side of the screen. When the
// window is pulled back on, its centre can end up outside the window entirely —
// and a character placed outside its own window is invisible.
func TestCharacterStaysInsideItsWindow(t *testing.T) {
	for _, b := range []rect{
		{X: -280, Y: 400, W: 300, H: 184},
		{X: 1420, Y: 400, W: 300, H: 184},
	} {
		out, p := placeOverlay(b, 258, 238, usable())
		if p.PetX < 0 || p.PetX > out.W {
			t.Fatalf("character placed at %d, outside a window %d wide", p.PetX, out.W)
		}
	}
}

// "Show Pet" has to find a pet that was dragged off the edge, which is exactly
// when somebody reaches for it.
func TestShowPetRecoversAWindowFromOffScreen(t *testing.T) {
	v := usable()
	cases := []struct {
		name               string
		x, y, wantX, wantY int
	}{
		{"off the right", v.W - 20, 300, v.W - 300, 300},
		{"off the bottom", 300, v.H - 20, 300, v.H - 184},
		{"off the left", -250, 300, 0, 300},
		{"above the top", 300, -80, 300, 0},
		{"already inside", 300, 300, 300, 300},
	}
	for _, tc := range cases {
		x, y := clampToArea(tc.x, tc.y, 300, 184, v)
		if x != tc.wantX || y != tc.wantY {
			t.Fatalf("%s: want %d,%d got %d,%d", tc.name, tc.wantX, tc.wantY, x, y)
		}
	}
}

// Nothing may assume a particular display. These are the sizes a laptop, an
// external monitor and a very small window all produce.
func TestPlacementHoldsAtAnyResolution(t *testing.T) {
	for _, v := range []rect{
		{W: 1393, H: 870},  // the reporting machine
		{W: 1280, H: 770},  // a smaller laptop
		{W: 3840, H: 2130}, // a 4K monitor
		{W: 1024, H: 698},  // an old projector
		{W: 800, H: 500},   // absurdly small, but must not panic or invert
	} {
		for _, x := range []int{0, v.W / 2, v.W - 300, v.W + 200, -200} {
			for _, y := range []int{0, v.H / 2, v.H - 184} {
				base := rect{X: x, Y: y, W: 300, H: 184}
				out, p := placeOverlay(base, 258, 238, v)
				if out.W > v.W {
					continue // a panel wider than the screen has no good answer
				}
				if out.X < 0 || out.X+out.W > v.W {
					t.Fatalf("%dx%d at %d,%d: window %d..%d escapes the screen",
						v.W, v.H, x, y, out.X, out.X+out.W)
				}
				if p.PetX < 0 || p.PetX > out.W {
					t.Fatalf("%dx%d at %d,%d: character at %d, outside a %d window",
						v.W, v.H, x, y, p.PetX, out.W)
				}
			}
		}
	}
}

// With no idea how big the screen is, leave the window where the user put it.
// Moving it by a guess is worse than not moving it.
func TestUnknownScreenMovesNothing(t *testing.T) {
	b := rect{X: 1200, Y: 400, W: 300, H: 184}
	out, p := placeOverlay(b, 258, 238, rect{})
	if out.X != b.X+b.W/2-out.W/2 {
		t.Fatalf("window was clamped against an unknown screen: x=%d", out.X)
	}
	if p.Side != "above" {
		t.Fatalf("unknown screen should keep the panel above, got %s", p.Side)
	}
	if x, y := clampToArea(9999, 9999, 300, 184, rect{}); x != 9999 || y != 9999 {
		t.Fatalf("clamped to an unknown area: %d,%d", x, y)
	}
}

// A window wider than the screen has nowhere to be pulled back to; it must not
// end up at a negative x.
func TestPanelWiderThanTheScreen(t *testing.T) {
	out, p := placeOverlay(base(), 2000, 238, rect{W: 800, H: 800})
	if out.X != 0 {
		t.Fatalf("want x=0 for an oversized window, got %d", out.X)
	}
	if p.PetX < 0 {
		t.Fatalf("character placed off the window at %d", p.PetX)
	}
}
