//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -Wno-deprecated-declarations
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
