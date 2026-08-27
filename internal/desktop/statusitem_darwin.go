//go:build darwin

package desktop

/*
// -fobjc-arc is load-bearing. cgo compiles Objective-C without ARC by default,
// and under manual retain/release the status item comes back autoreleased:
// storing it in a static does not retain it, so it is deallocated at the next
// drain of the pool. The icon appeared for no time at all, and the next message
// sent to that pointer killed the process. With ARC the file-scope statics are
// __strong and hold the objects for the life of the app.
#cgo CFLAGS: -x objective-c -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "statusitem_darwin.h"
*/
import "C"

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/state"
	"github.com/lijuno/agent-pet/internal/update"
)

//go:embed trayicon.png
var trayIcon []byte

// The dev app's icon differs in shape, not colour: this is a template image and
// macOS discards the colour to recolour it for the menu bar. Two identical
// icons would be the only thing distinguishing two menu-bar-only apps, which is
// to say nothing would. See scripts/gen-trayicon-dev.py.
//
//go:embed trayicon-dev.png
var trayIconDev []byte

func trayIconBytes() []byte {
	if flavor.Current().IsDev() {
		return trayIconDev
	}
	return trayIcon
}

// statusApp is how the C callback finds its way back. There is exactly one
// status item and one App for the life of the process, and the alternative —
// handing a Go pointer to C — is not allowed.
var (
	statusMu  sync.Mutex
	statusApp *App
	// statusPetIDs is the pet list in the order the submenu was last built.
	// A menu item carries an index, not a string; this turns it back into a pet.
	statusPetIDs []string
)

var startTrayOnce sync.Once

// startTray adds the status-bar item and keeps it in step with the pet.
func (a *App) startTray(ctx context.Context) {
	startTrayOnce.Do(func() {
		statusMu.Lock()
		statusApp = a
		statusMu.Unlock()

		// The menu bar is the only place the app needs to be reachable from,
		// so drop the Dock icon. Info.plist asks for this with LSUIElement and
		// Wails overrides it at launch — see petHideFromDock.
		C.petHideFromDock()

		onTop, shown := 0, 0
		if a.alwaysOnTop {
			onTop = 1
		}
		if !a.hidden {
			shown = 1
		}
		icon := trayIconBytes()
		C.petStatusInstall(unsafe.Pointer(&icon[0]), C.int(len(icon)),
			C.int(onTop), C.int(shown))

		// The menu is built with a placeholder first line. Set it now rather
		// than waiting for the first event, so the dev app says which app it
		// is from the moment it is installed.
		setStatusTitle(menuStateLine(a.eng.Last().Snapshot.State))
		setUpdateItem(a.GetUpdate())
		a.refreshPetMenu()

		// Keep the disabled first line in step, so the pet's state is readable
		// from the menu bar without opening anything.
		go func() {
			ch, cancel := a.eng.Subscribe()
			defer cancel()
			for {
				select {
				case <-ctx.Done():
					return
				case up, ok := <-ch:
					if !ok {
						return
					}
					setStatusTitle(menuStateLine(up.Snapshot.State))
				}
			}
		}()
	})
}

func setStatusTitle(s string) {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	C.petStatusSetState(c)
}

// usableArea is where a window may sit, in the coordinates Wails uses for
// window positions.
//
// Those are relative to the screen's visible frame, not to the display:
// Application.m computes them as `windowFrame.origin - [screen visibleFrame]
// .origin`. So the origin is already past the menu bar and past a Dock on the
// left, and the usable area is simply (0,0) to the visible frame's size.
//
// Getting that wrong in either direction is a real bug. Treating the origin as
// the display's puts every window one Dock-width off; using the display's
// *size* as the limit — which this did — lets a window hang exactly one
// Dock-width past the edge, which is what "the menu goes off the screen" was.
//
// A zero rect means the size is unknown, and nothing gets clamped: leaving a
// window where the user put it beats moving it by a guess.
func (a *App) usableArea() rect {
	var x, y, w, h C.int
	C.petVisibleFrame(&x, &y, &w, &h)
	if w <= 0 || h <= 0 {
		return rect{}
	}
	return rect{W: int(w), H: int(h)}
}

// displayInset is the menu bar and Dock, for diagnostics only. Nothing
// positions anything with it.
func (a *App) displayInset() (int, int) {
	var x, y, w, h C.int
	C.petVisibleFrame(&x, &y, &w, &h)
	return int(x), int(y)
}

func (a *App) activate() { C.petActivate() }

// StatusMenu is what the menu-bar menu currently says, as
// "tag:title[on]|...". A test can assert on it; nothing else can see a menu
// bar, and accessibility access is refused to anything that tries.
func (a *App) StatusMenu() string {
	// Big enough for the whole menu with every submenu expanded: the pet list
	// grows with whatever the user has installed, and a truncated dump would
	// fail a test for a reason that has nothing to do with the menu.
	buf := make([]C.char, 4096)
	n := C.petStatusMenuDump(&buf[0], C.int(len(buf)))
	return C.GoStringN(&buf[0], n)
}

// ClickStatusItem performs a status-menu item through its own target and
// action, so a test drives the same path as a click rather than a copy of it.
func (a *App) ClickStatusItem(name string) error {
	tags := map[string]C.int{
		"show": C.PET_SHOW, "status": C.PET_STATUS,
		"change": C.PET_CHANGE, "ontop": C.PET_ONTOP,
		"quit": C.PET_QUIT, "update": C.PET_UPDATE, "about": C.PET_ABOUT,
		"reload": C.PET_RELOAD, "bug": C.PET_REPORT,
		// The two buttons in the bug-report window. Reachable by name for the
		// same reason the menu items are: nothing in a test has a mouse.
		"bug-copy": C.PET_REPORT_COPY, "bug-open": C.PET_REPORT_OPEN,
	}
	tag, ok := tags[name]
	if !ok {
		// "pet:<id>" performs an item in the Change Pet submenu.
		if id, found := strings.CutPrefix(name, "pet:"); found {
			statusMu.Lock()
			idx := -1
			for i, p := range statusPetIDs {
				if p == id {
					idx = i
					break
				}
			}
			statusMu.Unlock()
			if idx < 0 {
				return fmt.Errorf("no pet %q in the menu", id)
			}
			tag = C.int(int(C.PET_PICK_BASE) + idx)
		} else {
			return fmt.Errorf("unknown status item %q", name)
		}
	}
	C.petStatusClickItem(tag)
	return nil
}

// statusItemReport is what `petctl doctor` prints about the menu bar. There is
// no way to look at a menu bar from a test, so the item is asked about itself.
func statusItemReport() string {
	bits := int(C.petStatusProbe())
	if bits == 0 {
		return "not installed"
	}
	out := "installed"
	if bits&2 == 0 {
		out = "installed but hidden — the menu bar may be full"
	}
	if bits&4 == 0 {
		out += ", no button"
	} else if bits&8 == 0 {
		out += ", no icon (showing a letter instead)"
	}
	return out
}

// dockReport is what `petctl doctor` prints about the Dock. LSUIElement is
// overridden by Wails at launch and set back afterwards, so the only honest
// answer is the policy actually in force, not the one Info.plist asked for.
func dockReport() string {
	switch int(C.petActivationPolicy()) {
	case 0:
		return "showing a Dock icon — LSUIElement did not take effect"
	case 1:
		return "hidden — menu bar only"
	case 2:
		return "prohibited"
	default:
		return "unknown"
	}
}

// refreshPetMenu rebuilds the Change Pet submenu from the pet library, ticking
// the one in use. Called when the item is installed and whenever the pet
// changes, from either surface — a menu that disagrees with the window about
// which character is on screen is worse than no tick at all.
func (a *App) refreshPetMenu() {
	pets := a.eng.Library().List()
	active := ""
	if p, ok := a.eng.ActivePet(); ok {
		active = p.ID
	}

	ids := make([]string, 0, len(pets))
	C.petStatusClearPets()
	for i, p := range pets {
		name := p.Name
		if name == "" {
			name = p.ID
		}
		c := C.CString(name)
		checked := C.int(0)
		if p.ID == active {
			checked = 1
		}
		C.petStatusAddPet(c, C.int(int(C.PET_PICK_BASE)+i), checked)
		C.free(unsafe.Pointer(c))
		ids = append(ids, p.ID)
	}

	statusMu.Lock()
	statusPetIDs = ids
	statusMu.Unlock()
}

// syncShownCheck keeps the Show Pet tick in step with the window.
func (a *App) syncShownCheck(on bool) { setStatusCheck(C.PET_SHOW, on) }

// setUpdateItem retitles the update item. The version has already been through
// update.Status.Validate, so it cannot put anything but a version number into a
// menu title.
//
// Enabled exactly when there is a page to open. openReleaseNotes does nothing
// without a URL, and an item that silently does nothing when pressed is worse
// than one that is visibly not for pressing.
func setUpdateItem(st update.Status) {
	c := C.CString(updateItemTitle(st))
	defer C.free(unsafe.Pointer(c))
	enabled := C.int(0)
	if st.NotesURL != "" {
		enabled = 1
	}
	C.petStatusSetUpdate(c, enabled)
}

// openURL hands a URL to Launch Services. Validated once more here: this is
// called from a menu action, and what reaches it came in over the event API.
func openURL(raw string) {
	if err := update.ValidateNotesURL(raw); err != nil {
		return
	}
	c := C.CString(raw)
	defer C.free(unsafe.Pointer(c))
	C.petOpenURL(c)
}

// setOnTopCheck ticks the Always on Top box. Named so app.go can keep the
// checkbox in step without importing a C constant it cannot see.
func setOnTopCheck(on bool) { setStatusCheck(C.PET_ONTOP, on) }

func setStatusCheck(tag C.int, on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.petStatusSetCheck(tag, v)
}

// showAbout opens the native About window. The text is built in Go and handed
// over as two strings, so the cgo side owns nothing but the window.
func showAbout(title, body string) {
	t, b := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	C.petAboutShow(t, b)
}

func closeAbout() { C.petAboutClose() }

// showBugReport opens the Report a Bug window. Same two strings as About: what
// it says is decided in Go, and the cgo side owns nothing but the window.
func showBugReport(title, body string) {
	t, b := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	C.petReportShow(t, b)
}

func closeBugReport() { C.petReportClose() }

// copyToClipboard puts the details on the pasteboard and says so on the
// button. The two belong together — a copy nobody can see happen reads as a
// button that does nothing.
func copyToClipboard(s string) {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	C.petCopyToPasteboard(c)
	C.petReportCopied()
}

// osVersion is the macOS version for the report. Empty is a fine answer: the
// report marks a field it does not know rather than guessing at one.
func osVersion() string {
	buf := make([]C.char, 128)
	n := C.petOSVersion(&buf[0], C.int(len(buf)))
	return C.GoStringN(&buf[0], n)
}

// bugReportReport describes the window for the diagnostics endpoint.
func bugReportReport() string {
	buf := make([]C.char, 256)
	n := C.petReportReport(&buf[0], C.int(len(buf)))
	return C.GoStringN(&buf[0], n)
}

// Alert shows a modal message. It exists for failures that happen before the
// window does, where the only other channel is stderr — which nothing launched
// from the Finder has anywhere to show.
func Alert(title, body string) {
	t, b := C.CString(title), C.CString(body)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(b))
	C.petAlert(t, b)
}

// aboutReport describes the About window for the diagnostics endpoint.
func aboutReport() string {
	buf := make([]C.char, 256)
	n := C.petAboutReport(&buf[0], C.int(len(buf)))
	return C.GoStringN(&buf[0], n)
}

//export goStatusClick
func goStatusClick(tag C.int) {
	statusMu.Lock()
	a := statusApp
	statusMu.Unlock()
	if a == nil {
		return
	}
	// Hand the work to a goroutine and return to AppKit at once.
	//
	// This function runs on the main thread, inside the menu item's action.
	// The Wails runtime must not be called from there: emitting an event ends
	// in evaluateJavaScript on the webview, and running that from inside a
	// main-thread callback segfaults the process — which is what "clicking Pet
	// Status quits the program" was. Only the three items that emit an event
	// crashed; the others never touched the webview.
	//
	// Every other call into Wails in this program is made from a goroutine.
	// These are no different, and the menu should not be waiting on them
	// anyway.
	go a.handleStatusClick(tag)
}

func (a *App) handleStatusClick(tag C.int) {
	if tag >= C.PET_PICK_BASE {
		statusMu.Lock()
		i := int(tag - C.PET_PICK_BASE)
		id := ""
		if i >= 0 && i < len(statusPetIDs) {
			id = statusPetIDs[i]
		}
		statusMu.Unlock()
		if id != "" {
			a.SetPet(id)
		}
		return
	}
	switch tag {
	case C.PET_SHOW:
		// A toggle, so the same item both fetches the pet and puts it away.
		// SetShown ticks the box itself.
		a.SetShown(a.hidden)
	case C.PET_STATUS:
		a.emitPanel("status")
	case C.PET_ABOUT:
		a.ShowAbout()
	case C.PET_REPORT:
		a.ShowBugReport()
	case C.PET_REPORT_COPY:
		a.CopyBugReportDetails()
	case C.PET_REPORT_OPEN:
		// The new-issue form with the report already in it. It fills the form
		// and stops: signing in, reading it and pressing the button are the
		// user's, which is why this is not a submission.
		a.OpenBugReportIssue()
	case C.PET_RELOAD:
		// The result goes to the log rather than to a dialog. A reload that
		// worked has nothing to say, and one that did not is a line in the
		// file the user is already editing.
		a.log.Info("reload", "result", a.ReloadConfig())
	case C.PET_CHANGE:
		// Nothing: the submenu is the picker. AppKit opens it on hover, and an
		// item with a submenu does not fire its action anyway.
	case C.PET_ONTOP:
		next := !a.alwaysOnTop
		a.SetAlwaysOnTop(next)
		setStatusCheck(C.PET_ONTOP, next)
	case C.PET_UPDATE:
		// The pet cannot install anything — it holds no updater and opens no
		// connections. It shows the release, and `petctl update` does the work.
		a.openReleaseNotes()
	case C.PET_QUIT:
		a.Quit()
	}
}

// menuStateLine is the disabled first line of the menu-bar menu.
//
// For the release app it is just the state. The dev app appends what it is and
// what version it is, because the two apps run at once and their menus are
// otherwise identical — and clicking the icon is how somebody works out which
// of the two they are looking at.
func menuStateLine(s state.State) string {
	f := flavor.Current()
	if !f.IsDev() {
		return trayLabel(s)
	}
	return trayLabel(s) + " · " + f.Label + " " + Version
}

func trayLabel(s state.State) string {
	switch s {
	case state.Attention:
		return "Needs you"
	case state.Working:
		return "Working"
	case state.Thinking:
		return "Thinking"
	case state.Confused:
		return "Something failed"
	case state.Worried:
		return "Repeated failures"
	case state.Happy:
		return "Task done"
	case state.Celebrate:
		return "Tests passed"
	case state.Sleeping:
		return "Sleeping"
	case state.Heart:
		return "Hello"
	default:
		return "Idle"
	}
}
