#import <Cocoa/Cocoa.h>
#include "statusitem_darwin.h"

// Implemented in Go (statusitem_darwin.go).
extern void goStatusClick(int tag);

// PetStatusTarget is the action target for the menu. The name is deliberately
// not a generic one: the whole reason this file exists is that two libraries
// both called their class AppDelegate and the binary would not link.
@interface PetStatusTarget : NSObject
- (void)onClick:(id)sender;
@end

@implementation PetStatusTarget
- (void)onClick:(id)sender {
  goStatusClick((int)[(NSMenuItem *)sender tag]);
}
@end

static NSStatusItem *petItem = nil;
static PetStatusTarget *petTarget = nil;
static NSMenuItem *petStateItem = nil;
static NSMenuItem *petSleepItem = nil;

static NSMenuItem *addItem(NSMenu *menu, NSString *title, int tag) {
  NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title
                                              action:@selector(onClick:)
                                       keyEquivalent:@""];
  [it setTarget:petTarget];
  [it setTag:tag];
  [menu addItem:it];
  return it;
}

// onMain runs a block on the main thread. AppKit objects may only be touched
// there, and every caller here arrives from a Go goroutine.
static void onMain(void (^block)(void)) {
  if ([NSThread isMainThread]) {
    block();
  } else {
    dispatch_async(dispatch_get_main_queue(), block);
  }
}

void petStatusInstall(const void *png, int len, int onTop, int muted, int shown) {
  NSData *data = [NSData dataWithBytes:png length:len];
  onMain(^{
    if (petItem != nil) {
      return;
    }
    petTarget = [[PetStatusTarget alloc] init];
    petItem = [[NSStatusBar systemStatusBar]
        statusItemWithLength:NSVariableStatusItemLength];

    NSImage *icon = [[NSImage alloc] initWithData:data];
    if (icon != nil) {
      [icon setSize:NSMakeSize(18, 18)];
      // A template image is recoloured by macOS for light and dark menu bars.
      [icon setTemplate:YES];
      petItem.button.image = icon;
    } else {
      // Better a visible letter than an invisible item.
      petItem.button.title = @"P";
    }
    petItem.button.toolTip = @"Digital Pet";

    NSMenu *menu = [[NSMenu alloc] init];
    [menu setAutoenablesItems:NO];

    petStateItem = addItem(menu, @"Idle", 0);
    [petStateItem setEnabled:NO];
    [menu addItem:[NSMenuItem separatorItem]];

    // A checkbox, like Always on Top: it reports whether the pet is on screen
    // and toggles it, rather than being a one-way door.
    NSMenuItem *show = addItem(menu, @"Show Pet", PET_SHOW);
    [show setState:shown ? NSControlStateValueOn : NSControlStateValueOff];
    addItem(menu, @"Pet Status", PET_STATUS);
    addItem(menu, @"Statistics", PET_STATS);
    addItem(menu, @"Change Pet…", PET_CHANGE);
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *top = addItem(menu, @"Always on Top", PET_ONTOP);
    [top setState:onTop ? NSControlStateValueOn : NSControlStateValueOff];
    NSMenuItem *mute = addItem(menu, @"Mute", PET_MUTE);
    [mute setState:muted ? NSControlStateValueOn : NSControlStateValueOff];
    [menu addItem:[NSMenuItem separatorItem]];

    petSleepItem = addItem(menu, @"Sleep", PET_SLEEP);
    [menu addItem:[NSMenuItem separatorItem]];
    addItem(menu, @"Quit Digital Pet", PET_QUIT);

    petItem.menu = menu;
  });
}

void petStatusSetState(const char *title) {
  NSString *s = [NSString stringWithUTF8String:title];
  onMain(^{
    [petStateItem setTitle:s];
  });
}

void petStatusSetCheck(int tag, int on) {
  onMain(^{
    NSMenuItem *it = [petItem.menu itemWithTag:tag];
    [it setState:on ? NSControlStateValueOn : NSControlStateValueOff];
  });
}

void petStatusSetSleepTitle(const char *title) {
  NSString *s = [NSString stringWithUTF8String:title];
  onMain(^{
    [petSleepItem setTitle:s];
  });
}

void petVisibleFrame(int *x, int *y, int *w, int *h) {
  __block NSRect frame = NSZeroRect;
  __block NSRect visible = NSZeroRect;
  void (^read)(void) = ^{
    // The screen the pet's own window is on. Wails reports window positions
    // relative to that screen, so asking a different one — the main screen,
    // say, on a two-monitor desk — would answer in the wrong coordinate space.
    NSScreen *screen = nil;
    for (NSWindow *win in [NSApp windows]) {
      if (win.screen != nil && win.isVisible) {
        screen = win.screen;
        break;
      }
    }
    if (screen == nil) {
      screen = [NSApp keyWindow].screen;
    }
    if (screen == nil) {
      screen = [NSScreen mainScreen];
    }
    if (screen != nil) {
      frame = screen.frame;
      visible = screen.visibleFrame;
    }
  };
  if ([NSThread isMainThread]) {
    read();
  } else {
    dispatch_sync(dispatch_get_main_queue(), read);
  }
  if (visible.size.width <= 0) {
    *x = *y = *w = *h = 0;
    return;
  }
  // Cocoa measures from the bottom left of the display; window positions are
  // measured from the top left. Flip the origin.
  *x = (int)(visible.origin.x - frame.origin.x);
  *y = (int)((frame.origin.y + frame.size.height) -
             (visible.origin.y + visible.size.height));
  *w = (int)visible.size.width;
  *h = (int)visible.size.height;
}

void petActivate(void) {
  onMain(^{
    [NSApp activateIgnoringOtherApps:YES];
  });
}

int petStatusMenuDump(char *buf, int cap) {
  __block NSMutableString *out = [NSMutableString string];
  void (^dump)(void) = ^{
    for (NSMenuItem *it in petItem.menu.itemArray) {
      if (it.isSeparatorItem) {
        continue;
      }
      [out appendFormat:@"%ld:%@%@|", (long)it.tag, it.title,
                        it.state == NSControlStateValueOn ? @"[on]" : @""];
    }
  };
  if ([NSThread isMainThread]) {
    dump();
  } else {
    dispatch_sync(dispatch_get_main_queue(), dump);
  }
  const char *s = [out UTF8String];
  int n = (int)strlen(s);
  if (n >= cap) {
    n = cap - 1;
  }
  memcpy(buf, s, n);
  buf[n] = 0;
  return n;
}

void petStatusClickItem(int tag) {
  onMain(^{
    NSMenuItem *it = [petItem.menu itemWithTag:tag];
    if (it != nil) {
      // Dispatch through the responder chain, which is how AppKit delivers a
      // real click — rather than performSelector, which merely resembles one.
      [NSApp sendAction:it.action to:it.target from:it];
    }
  });
}

int petStatusProbe(void) {
  __block int bits = 0;
  // Synchronous on purpose: a probe that answered before the work happened
  // would be worse than no probe. Never called from the main thread itself.
  void (^check)(void) = ^{
    if (petItem != nil) {
      bits |= 1;
      if ([petItem isVisible]) {
        bits |= 2;
      }
      if (petItem.button != nil) {
        bits |= 4;
        if (petItem.button.image != nil) {
          bits |= 8;
        }
      }
    }
  };
  if ([NSThread isMainThread]) {
    check();
  } else {
    dispatch_sync(dispatch_get_main_queue(), check);
  }
  return bits;
}
