#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>
#include "clickthrough_darwin.h"

// The region, in window coordinates with a top-left origin. Guarded by nothing
// because everything that touches it runs on the main queue — which is not the
// same as saying it is called from there.
//
// Wails runs OnStartup on a goroutine, so the Go side of this file is called
// from a thread that is not the main one, and the first version trapped on the
// spot: AppKit is main-thread-only and NSEvent monitors are no exception. Every
// entry point below therefore hops to the main queue and does nothing else.
static PetRect *gRects = NULL;
static int gCount = 0;

static id gGlobalMonitor = nil;
static id gLocalMonitor = nil;
static BOOL gStarted = NO;

// gIgnoring mirrors the window's flag so it is only set when it changes. A
// mouse-moved event arrives for every point the cursor crosses; setting the
// same value a hundred times a second is a great deal of work to stay still.
static BOOL gIgnoring = NO;

// petWindow finds the character's window by class rather than by position in
// [NSApp windows]. An NSStatusItem creates a window of its own, so "the first
// one" is a coin toss that would leave the status item click-through and the
// pet untouched.
static NSWindow *petWindow(void) {
  Class wails = NSClassFromString(@"WailsWindow");
  if (wails == nil) {
    return nil;
  }
  for (NSWindow *w in [NSApp windows]) {
    if ([w isKindOfClass:wails]) {
      return w;
    }
  }
  return nil;
}

// insideRegion answers for a point in screen coordinates.
//
// The flip is the fiddly part and the reason this is not in Go: AppKit measures
// from the bottom left of the window and everything else in this project — the
// frontend's CSS, the overlay the frontend reports, the Go geometry — measures
// from the top left.
static BOOL insideRegion(NSWindow *win, NSPoint screenPoint) {
  if (win == nil || gCount == 0) {
    return NO;
  }
  NSRect f = [win frame];
  if (!NSPointInRect(screenPoint, f)) {
    return NO;
  }
  CGFloat lx = screenPoint.x - NSMinX(f);
  CGFloat ly = NSMaxY(f) - screenPoint.y; // top-left origin

  for (int i = 0; i < gCount; i++) {
    PetRect r = gRects[i];
    if (lx >= r.x && lx < r.x + r.w && ly >= r.y && ly < r.y + r.h) {
      return YES;
    }
  }
  return NO;
}

static void applyForCursor(void) {
  NSWindow *win = petWindow();
  if (win == nil) {
    return;
  }
  // While a drag is in progress the window is being moved by the character
  // under the cursor. Flipping the flag then drops the pet mid-drag: the window
  // stops receiving the drag and stays wherever the last event left it.
  if ([NSEvent pressedMouseButtons] != 0) {
    return;
  }

  BOOL shouldIgnore = !insideRegion(win, [NSEvent mouseLocation]);
  if (shouldIgnore == gIgnoring) {
    return;
  }
  gIgnoring = shouldIgnore;
  [win setIgnoresMouseEvents:shouldIgnore];
}

void petSetHitRegion(const PetRect *rects, int count) {
  // Copied here, before the hop: the caller owns that memory and Go may reuse
  // or collect it the moment this returns, long before a queued block runs.
  PetRect *copy = NULL;
  int n = 0;
  if (rects != NULL && count > 0) {
    copy = malloc(sizeof(PetRect) * (size_t)count);
    if (copy != NULL) {
      memcpy(copy, rects, sizeof(PetRect) * (size_t)count);
      n = count;
    }
  }

  dispatch_async(dispatch_get_main_queue(), ^{
    free(gRects);
    gRects = copy;
    gCount = n;
    // The region may have moved out from under the cursor — a panel closing is
    // exactly that — so decide again rather than waiting for the next twitch.
    if (gStarted) {
      applyForCursor();
    }
  });
}

void petClickThroughStart(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
  if (gStarted) {
    return;
  }
  gStarted = YES;

  // Both monitors, because neither sees the whole picture: the global one
  // reports events going to other applications, and stops mattering once the
  // cursor is over our own window; the local one sees ours, and goes quiet the
  // moment we start ignoring events.
  NSEventMask mask = NSEventMaskMouseMoved | NSEventMaskLeftMouseDragged |
                     NSEventMaskRightMouseDragged | NSEventMaskLeftMouseUp;

  gGlobalMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:mask
                                                          handler:^(NSEvent *e) {
                                                            applyForCursor();
                                                          }];
  gLocalMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask
                                                        handler:^NSEvent *(NSEvent *e) {
                                                          applyForCursor();
                                                          return e;
                                                        }];
  applyForCursor();
  });
}

void petClickThroughStop(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
  if (gGlobalMonitor != nil) {
    [NSEvent removeMonitor:gGlobalMonitor];
    gGlobalMonitor = nil;
  }
  if (gLocalMonitor != nil) {
    [NSEvent removeMonitor:gLocalMonitor];
    gLocalMonitor = nil;
  }
  gStarted = NO;

  // Whatever happens, the window ends up taking its clicks again. A pet that
  // cannot be picked up is a worse fault than the dead zone this fixes.
  NSWindow *win = petWindow();
  if (win != nil) {
    [win setIgnoresMouseEvents:NO];
  }
  gIgnoring = NO;
  });
}

int petIsIgnoringMouse(void) { return gIgnoring ? 1 : 0; }
