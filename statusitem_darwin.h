// A macOS status-bar item for the pet.
//
// This exists instead of fyne.io/systray because that library declares its own
// Objective-C class named AppDelegate, and so does Wails' desktop frontend: a
// production build fails to link on a duplicate symbol. See ADR 0005.
//
// Nothing here creates an application, a delegate or a run loop. It hangs an
// NSStatusItem off the NSApplication that Wails already runs, which is why it
// cannot collide with it.

#ifndef PET_STATUSITEM_H
#define PET_STATUSITEM_H

// Menu item identifiers, shared with the Go side.
enum {
  PET_SHOW = 1,
  PET_STATUS = 2,
  PET_STATS = 3,
  PET_CHANGE = 4,
  PET_ONTOP = 5,
  PET_MUTE = 6,
  PET_QUIT = 8,
  // Items in the Change Pet submenu are PET_PICK_BASE + the pet's index in the
  // list the Go side last sent.
  PET_PICK_BASE = 100,
};

// petStatusInstall adds the status item. png is a template icon. Safe to call
// from any goroutine: the work is dispatched to the main thread, which is the
// only place AppKit will accept it.
void petStatusInstall(const void *png, int len, int onTop, int muted, int shown);

// petStatusSetState updates the disabled first line, so the pet's state is
// readable without opening anything.
void petStatusSetState(const char *title);

// petStatusSetCheck ticks or clears a checkbox item.
void petStatusSetCheck(int tag, int on);

// petStatusClearPets empties the Change Pet submenu, and petStatusAddPet
// appends one character to it. The pet list is a Go fact — built-in packs plus
// whatever the user dropped in their pets directory — so the menu is filled
// from there rather than guessed at here.
void petStatusClearPets(void);
void petStatusAddPet(const char *title, int tag, int checked);

// petStatusProbe reports what actually happened, because nothing else can:
// there is no way to look at the menu bar from a test. Bit 0: the item object
// exists. Bit 1: the system says it is visible. Bit 2: it has a button. Bit 3:
// the button has an image. `petctl doctor` prints the answer.
int petStatusProbe(void);

// petVisibleFrame is the part of the screen a window can actually occupy: the
// whole display minus the menu bar and the Dock. Written in top-left
// coordinates to match the ones Wails uses for window positions, rather than
// Cocoa's bottom-left.
//
// Guessing this was the bug. A menu placed against the bottom of the "screen"
// is behind the Dock, and against the top it is under the menu bar; both look
// exactly like being off the edge.
void petVisibleFrame(int *x, int *y, int *w, int *h);

// petActivate brings the application to the front. Showing a window is not the
// same as being able to see it: an unactivated app puts its window behind
// whatever the user is looking at.
void petActivate(void);

// petStatusMenuDump writes the status menu as "tag:title[state]|..." so a test
// can assert what the menu says. Returns the number of bytes written.
int petStatusMenuDump(char *buf, int cap);

// petStatusClickItem performs a menu item exactly as a click on it would,
// target and all, so a test exercises the same path as the user.
void petStatusClickItem(int tag);

#endif
