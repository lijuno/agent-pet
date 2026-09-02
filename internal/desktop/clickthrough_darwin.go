//go:build darwin

package desktop

/*
// -fobjc-arc for the same reason statusitem_darwin.go needs it: cgo compiles
// Objective-C without ARC, and the event monitors are objects held in statics
// for the life of the app. Under manual retain/release they would be
// autoreleased out from under those statics and the next removeMonitor: would
// be sent to freed memory.
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "clickthrough_darwin.h"
*/
import "C"

import "unsafe"

// startClickThrough begins handing the transparent margin back to whatever is
// behind the window. See ADR 0009.
func startClickThrough() { C.petClickThroughStart() }

// stopClickThrough gives the window its whole rectangle back.
func stopClickThrough() { C.petClickThroughStop() }

// setHitRegion tells the native side which parts of the window are in use.
//
// An empty region would make the whole window click-through, including the
// character, so it is refused here rather than trusted not to happen: the one
// caller computes it from a window that may not exist yet, and the cost of
// being wrong is a pet that cannot be picked up.
func setHitRegion(rs []rect) {
	if len(rs) == 0 {
		return
	}
	cr := make([]C.PetRect, len(rs))
	for i, r := range rs {
		cr[i] = C.PetRect{
			x: C.int(r.X), y: C.int(r.Y),
			w: C.int(r.W), h: C.int(r.H),
		}
	}
	C.petSetHitRegion((*C.PetRect)(unsafe.Pointer(&cr[0])), C.int(len(cr)))
}

// isIgnoringMouse is whether the window is currently letting clicks past.
func isIgnoringMouse() bool { return C.petIsIgnoringMouse() == 1 }
