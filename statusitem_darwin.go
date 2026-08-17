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
	"sync"
	"unsafe"

	"github.com/lijunix/agent-digital-pet/internal/state"
)

//go:embed build/trayicon.png
var trayIcon []byte

// statusApp is how the C callback finds its way back. There is exactly one
// status item and one App for the life of the process, and the alternative —
// handing a Go pointer to C — is not allowed.
var (
	statusMu  sync.Mutex
	statusApp *App
)

var startTrayOnce sync.Once

// startTray adds the status-bar item and keeps it in step with the pet.
func (a *App) startTray(ctx context.Context) {
	startTrayOnce.Do(func() {
		statusMu.Lock()
		statusApp = a
		statusMu.Unlock()

		onTop, muted := 0, 0
		if a.alwaysOnTop {
			onTop = 1
		}
		if a.muted {
			muted = 1
		}
		C.petStatusInstall(unsafe.Pointer(&trayIcon[0]), C.int(len(trayIcon)),
			C.int(onTop), C.int(muted))

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

// visibleFrame is the area a window can actually occupy, from AppKit rather
// than from arithmetic on the display size.
func (a *App) visibleFrame() rect {
	var x, y, w, h C.int
	C.petVisibleFrame(&x, &y, &w, &h)
	if w <= 0 || h <= 0 {
		// AppKit had no answer. Fall back to the whole display less a guess at
		// the menu bar, which is what this code did before it could ask.
		sw, sh := a.screenSize()
		return rect{X: 0, Y: menuBarInset, W: sw, H: sh - menuBarInset}
	}
	return rect{X: int(x), Y: int(y), W: int(w), H: int(h)}
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
	switch tag {
	case C.PET_SHOW:
		a.showWindow()
	case C.PET_STATUS:
		a.emitPanel("status")
	case C.PET_STATS:
		a.emitPanel("stats")
	case C.PET_CHANGE:
		a.emitPanel("pets")
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
