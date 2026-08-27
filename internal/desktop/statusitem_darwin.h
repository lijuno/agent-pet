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
  PET_CHANGE = 4,
  PET_ONTOP = 5,
  PET_UPDATE = 7,
  PET_QUIT = 8,
  PET_ABOUT = 9,
  PET_RELOAD = 10,
  PET_REPORT = 11,
  // The two buttons in the Report a Bug window. They carry tags and share the
  // menu's target and action, so a button press arrives in Go by the same path
  // a menu click does — one callback, not two.
  PET_REPORT_COPY = 12,
  PET_REPORT_OPEN = 13,
  // Items in the Change Pet submenu are PET_PICK_BASE + the pet's index in the
  // list the Go side last sent.
  PET_PICK_BASE = 100,
};

// petStatusInstall adds the status item. png is a template icon. Safe to call
// from any goroutine: the work is dispatched to the main thread, which is the
// only place AppKit will accept it.
void petStatusInstall(const void *png, int len, int onTop, int shown);

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

// petStatusSetUpdate retitles the update item and sets whether it can be
// pressed. The item is always visible: it reports the last check as well as an
// available update. Disabled means there is no release page to open.
void petStatusSetUpdate(const char *title, int enabled);

// petOpenURL opens a URL in the user's browser through NSWorkspace.
//
// Deliberately not the Wails runtime's BrowserOpenURL, which runs /usr/bin/open
// as a subprocess: petd spawns no processes and SECURITY.md says so. NSWorkspace
// asks Launch Services directly. Non-https URLs are refused here as well as by
// the caller — this is the last point before a URL reaches the system.
void petOpenURL(const char *url);

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

// petHideFromDock drops the app out of the Dock and the app switcher.
//
// LSUIElement in Info.plist is not enough on its own: Wails' own AppDelegate
// calls setActivationPolicy:NSApplicationActivationPolicyRegular in
// applicationWillFinishLaunching, which overrides it. This sets the policy
// back afterwards. Dispatched to the main thread, which both guarantees AppKit
// accepts it and puts it after any launch work still on the queue.
void petHideFromDock(void);

// petActivationPolicy reports the policy actually in force: 0 regular (Dock
// icon), 1 accessory (menu bar only), 2 prohibited. Wails setting it behind our
// back is exactly the kind of thing that cannot be seen from a test, so the app
// is asked and `petctl doctor` prints the answer.
int petActivationPolicy(void);

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

// petAlert shows a modal alert and waits for it to be dismissed. For the one
// message that has to arrive before there is a window to put it in.
void petAlert(const char *title, const char *body);

// petAboutShow opens the About window, centred on screen, creating it the first
// time. It is a real NSWindow with a title bar and a close button rather than
// an overlay in the pet: Wails v2 has exactly one window and it is the
// frameless transparent pet, so a second one has to be built here.
void petAboutShow(const char *title, const char *body);

// petAboutClose hides it again. For tests, and for Quit.
void petAboutClose(void);

// petReportShow opens the Report a Bug window: the same kind of window as
// About, plus two buttons. It is where the answer to "how do I report this?"
// lives, because the menu item that opens it is the only part of the program
// somebody with a bug will think to look at.
void petReportShow(const char *title, const char *body);

// petReportClose hides it again.
void petReportClose(void);

// petReportCopied flips the copy button's title for a moment. A button that
// puts something on the clipboard changes nothing anybody can see, and there
// is no badge in a native window to say it happened.
void petReportCopied(void);

// petReportReport describes the window for the diagnostics endpoint: where it
// is, what its buttons say, and whether its text fits. A test cannot see a
// native window — the button titles are where the copy feedback shows up, and
// a path cut off by the edge of the field shows up nowhere else at all.
int petReportReport(char *buf, int cap);

// petCopyToPasteboard puts text on the general pasteboard.
void petCopyToPasteboard(const char *text);

// petOSVersion is the macOS version, for the bug report. Asked of the system
// rather than shelled out to `sw_vers`: petd spawns no processes.
int petOSVersion(char *buf, int cap);

// petAboutReport describes the window — whether it is on screen, its size and
// where it sits relative to the centre of the display it is on. A test cannot
// see a native window any other way: reading one needs accessibility access,
// which this project does not have.
int petAboutReport(char *buf, int cap);

#endif
