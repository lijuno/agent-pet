package main

import "testing"

// The window must be no taller than the character needs. Anything more is
// transparent dead space above the pet, and because macOS keeps a dragged
// window's top below the menu bar, that space is screen the pet can never
// reach — which is exactly the bug this replaced.
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
	_, h1 := WindowSize(1)
	_, h2 := WindowSize(2)
	if h2 <= h1 {
		t.Fatalf("a bigger pet needs a bigger window: %d at 1x, %d at 2x", h1, h2)
	}
	// A missing or nonsensical scale must not collapse the window.
	if w, h := WindowSize(0); w <= 0 || h <= 0 {
		t.Fatalf("zero scale produced a %dx%d window", w, h)
	}
}

func TestOverlayGrowsUpwardWhenThereIsRoom(t *testing.T) {
	if got := overlaySide(400, 238); got != "above" {
		t.Fatalf("plenty of room above, want above, got %s", got)
	}
}

// Parked at the top of the screen there is nowhere above to grow, and a panel
// opening upward would be off-screen.
func TestOverlayGrowsDownwardAgainstTheTopOfTheScreen(t *testing.T) {
	if got := overlaySide(menuBarInset, 238); got != "below" {
		t.Fatalf("no room above, want below, got %s", got)
	}
	if got := overlaySide(0, 238); got != "below" {
		t.Fatalf("window at the very top, want below, got %s", got)
	}
}

// The menu bar is not usable space, so a window sitting just under it has no
// room even though its y looks large enough by a hair.
func TestOverlayDoesNotCountTheMenuBarAsRoom(t *testing.T) {
	needed := 100
	if got := overlaySide(needed+menuBarInset-1, needed); got != "below" {
		t.Fatalf("one point short, want below, got %s", got)
	}
	if got := overlaySide(needed+menuBarInset, needed); got != "above" {
		t.Fatalf("exactly enough, want above, got %s", got)
	}
}
