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
  PET_SLEEP = 7,
  PET_QUIT = 8,
};

// petStatusInstall adds the status item. png is a template icon. Safe to call
// from any goroutine: the work is dispatched to the main thread, which is the
// only place AppKit will accept it.
void petStatusInstall(const void *png, int len, int onTop, int muted);

// petStatusSetState updates the disabled first line, so the pet's state is
// readable without opening anything.
void petStatusSetState(const char *title);

// petStatusSetCheck ticks or clears a checkbox item.
void petStatusSetCheck(int tag, int on);

// petStatusSetSleepTitle switches the sleep item between Sleep and Wake Up.
void petStatusSetSleepTitle(const char *title);

#endif
