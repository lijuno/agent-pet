//go:build darwin

package main

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

	"github.com/lijuno/agent-digital-pet/internal/state"
)

//go:embed build/trayicon.png
var trayIcon []byte

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

		onTop, muted, shown := 0, 0, 0
		if a.alwaysOnTop {
			onTop = 1
		}
		if a.muted {
			muted = 1
		}
		if !a.hidden {
			shown = 1
		}
		C.petStatusInstall(unsafe.Pointer(&trayIcon[0]), C.int(len(trayIcon)),
			C.int(onTop), C.int(muted), C.int(shown))

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
					setStatusTitle(trayLabel(up.Snapshot.State))
					setSleepTitle(up.Snapshot.State)
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

// The one item that changes wording rather than state: a pet already asleep
// should offer to wake up, exactly as the in-window menu does.
func setSleepTitle(s state.State) {
	title := "Sleep"
	if s == state.Sleeping {
		title = "Wake Up"
	}
	c := C.CString(title)
	defer C.free(unsafe.Pointer(c))
	C.petStatusSetSleepTitle(c)
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
	buf := make([]C.char, 1024)
	n := C.petStatusMenuDump(&buf[0], C.int(len(buf)))
	return C.GoStringN(&buf[0], n)
}

// ClickStatusItem performs a status-menu item through its own target and
// action, so a test drives the same path as a click rather than a copy of it.
func (a *App) ClickStatusItem(name string) error {
	tags := map[string]C.int{
		"show": C.PET_SHOW, "status": C.PET_STATUS, "stats": C.PET_STATS,
		"change": C.PET_CHANGE, "ontop": C.PET_ONTOP, "mute": C.PET_MUTE,
		"sleep": C.PET_SLEEP, "quit": C.PET_QUIT,
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

func setStatusCheck(tag C.int, on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.petStatusSetCheck(tag, v)
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
	case C.PET_STATS:
		a.emitPanel("stats")
	case C.PET_CHANGE:
		// Nothing: the submenu is the picker. AppKit opens it on hover, and an
		// item with a submenu does not fire its action anyway.
	case C.PET_ONTOP:
		next := !a.alwaysOnTop
		a.SetAlwaysOnTop(next)
		setStatusCheck(C.PET_ONTOP, next)
	case C.PET_MUTE:
		next := !a.muted
		a.SetMuted(next)
		setStatusCheck(C.PET_MUTE, next)
	case C.PET_SLEEP:
		// One item, like the in-window menu: sleep and wake are never both
		// useful at once.
		if a.eng.Snapshot().State == state.Sleeping {
			a.Wake()
		} else {
			a.Sleep()
		}
	case C.PET_QUIT:
		a.Quit()
	}
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
	case state.Tired:
		return "Long session"
	case state.Sleeping:
		return "Sleeping"
	case state.Heart:
		return "Hello"
	default:
		return "Idle"
	}
}
