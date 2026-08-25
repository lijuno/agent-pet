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
static NSMenuItem *petChangeItem = nil;
static NSMenuItem *petUpdateItem = nil;

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
    petItem.button.toolTip = @"Agent Pet";

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
    // A submenu, so the characters appear beside the menu bar menu rather
    // than in a panel next to the pet. An item with a submenu opens it instead
    // of firing its action, which is what we want here.
    petChangeItem = addItem(menu, @"Change Pet", PET_CHANGE);
    NSMenu *pets = [[NSMenu alloc] init];
    [pets setAutoenablesItems:NO];
    petChangeItem.submenu = pets;
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *top = addItem(menu, @"Always on Top", PET_ONTOP);
    [top setState:onTop ? NSControlStateValueOn : NSControlStateValueOff];
    NSMenuItem *mute = addItem(menu, @"Mute", PET_MUTE);
    [mute setState:muted ? NSControlStateValueOn : NSControlStateValueOff];

    // Hidden until a check finds something. An always-present item reporting
    // nothing is worse than no item.
    // Always present. It reports the last check rather than appearing only
    // when there is something to install, so the menu can answer "have I got
    // the latest?" as well as "there is a new one".
    petUpdateItem = addItem(menu, @"No update check yet", PET_UPDATE);
    [petUpdateItem setEnabled:NO];
    [menu addItem:[NSMenuItem separatorItem]];

    addItem(menu, @"Quit Pet", PET_QUIT);

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

void petStatusClearPets(void) {
  onMain(^{
    NSMenu *sub = [[NSMenu alloc] init];
    [sub setAutoenablesItems:NO];
    petChangeItem.submenu = sub;
  });
}

void petStatusAddPet(const char *title, int tag, int checked) {
  NSString *t = [NSString stringWithUTF8String:title];
  onMain(^{
    NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:t
                                                action:@selector(onClick:)
                                         keyEquivalent:@""];
    [it setTarget:petTarget];
    [it setTag:tag];
    [it setState:checked ? NSControlStateValueOn : NSControlStateValueOff];
    [petChangeItem.submenu addItem:it];
  });
}

void petStatusSetUpdate(const char *title, int enabled) {
  NSString *t = [NSString stringWithUTF8String:title];
  onMain(^{
    [petUpdateItem setTitle:t];
    [petUpdateItem setEnabled:enabled ? YES : NO];
  });
}

void petOpenURL(const char *url) {
  NSString *s = [NSString stringWithUTF8String:url];
  onMain(^{
    NSURL *u = [NSURL URLWithString:s];
    // Checked again here, at the last point before Launch Services sees it: a
    // scheme other than https is how a URL becomes something other than a page.
    if (u == nil || ![[u scheme] isEqualToString:@"https"]) {
      return;
    }
    [[NSWorkspace sharedWorkspace] openURL:u];
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

void petHideFromDock(void) {
  // Always dispatch_async, never onMain's synchronous path. Wails sets the
  // policy to Regular in applicationWillFinishLaunching; if this ran inline on
  // the main thread while that was still ahead of us, Wails would overwrite it
  // and the Dock icon would come back. Queuing it guarantees we go last.
  dispatch_async(dispatch_get_main_queue(), ^{
    [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
  });
}

int petActivationPolicy(void) {
  __block int policy = 0;
  // Synchronous for the same reason petStatusProbe is: an answer that arrived
  // before the work happened would be worse than no answer.
  void (^read)(void) = ^{
    policy = (int)[NSApp activationPolicy];
  };
  if ([NSThread isMainThread]) {
    read();
  } else {
    dispatch_sync(dispatch_get_main_queue(), read);
  }
  return policy;
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
      // Ticked, hidden and disabled are all reported: a test cannot see a
      // menu, so "present but not shown" and "present but not pressable" both
      // have to be sayable. The update item is always present and is disabled
      // whenever there is no page behind it.
      [out appendFormat:@"%ld:%@%@%@%@|", (long)it.tag, it.title,
                        it.state == NSControlStateValueOn ? @"[on]" : @"",
                        it.isHidden ? @"[hidden]" : @"",
                        it.isEnabled ? @"" : @"[off]"];
      for (NSMenuItem *sub in it.submenu.itemArray) {
        [out appendFormat:@"%ld:>%@%@|", (long)sub.tag, sub.title,
                          sub.state == NSControlStateValueOn ? @"[on]" : @""];
      }
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
    if (it == nil) {
      for (NSMenuItem *top in petItem.menu.itemArray) {
        NSMenuItem *found = [top.submenu itemWithTag:tag];
        if (found != nil) {
          it = found;
          break;
        }
      }
    }
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
