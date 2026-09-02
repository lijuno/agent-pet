// Click-through for the transparent parts of the pet's window. See ADR 0009.
//
// The window is a rectangle far larger than the character — 108 to 60 points of
// transparent margin either side, 56 above — and by default it takes mouse
// events across all of it, so a click near the pet reaches neither the app
// behind nor, visibly, the pet.
//
// macOS has no shaped-window API. NSWindow.ignoresMouseEvents is the only lever
// that hands a click to the application behind, and it is all-or-nothing for
// the whole window. So the window is switched between taking and ignoring
// events as the cursor crosses the edge of the region it actually uses.
//
// Nothing here decides what that region is. Go computes it and pushes it in;
// this file stores the rectangles and does the one thing that cannot be done
// anywhere else — watch the cursor, and set the flag.

#ifndef PET_CLICKTHROUGH_H
#define PET_CLICKTHROUGH_H

// A rectangle in window coordinates, with the origin at the window's top left
// as the frontend and the Go geometry both use. AppKit's own origin is at the
// bottom left; the conversion happens here so only one file has to know.
typedef struct {
  int x, y, w, h;
} PetRect;

// petClickThroughStart begins watching the cursor. Safe to call more than once;
// only the first call installs anything.
void petClickThroughStart(void);

// petClickThroughStop stops watching and gives the window back its whole
// rectangle, so a failure or a disabled setting cannot leave the pet
// unclickable.
void petClickThroughStop(void);

// petSetHitRegion replaces the region. Copies what it is given; the caller
// keeps ownership. Passing zero rectangles means the window takes no mouse
// events anywhere, which is why Go never sends an empty region while the
// feature is on.
void petSetHitRegion(const PetRect *rects, int count);

// petIsIgnoringMouse reports the flag as it stands, so the app can say on
// /diagnostics whether the cursor is currently over a part of the window it
// uses. That is the only way to see this working without a person clicking.
int petIsIgnoringMouse(void);

#endif
