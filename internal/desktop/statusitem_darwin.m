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
  // Menu items and the bug-report window's buttons both arrive here, and both
  // answer -tag. NSMenuItem is not an NSControl, so there is no common class
  // to cast to; the tag is asked for directly.
  goStatusClick((int)[sender tag]);
}
@end

static NSStatusItem *petItem = nil;
static PetStatusTarget *petTarget = nil;
static NSMenuItem *petStateItem = nil;
static NSMenuItem *petChangeItem = nil;
static NSMenuItem *petUpdateItem = nil;
static NSButton *petReportCopyBtn = nil;
static NSButton *petReportOpenBtn = nil;
static NSButton *petReportSaveBtn = nil;

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

void petStatusInstall(const void *png, int len, int onTop, int hidden) {
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

    // "Show Status" rather than "Status": every other item in this menu is
    // something to do, and a bare noun among verbs reads as a heading for the
    // items under it.
    addItem(menu, @"Show Status", PET_STATUS);
    // A submenu, so the characters appear beside the menu bar menu rather
    // than in a panel next to the pet. An item with a submenu opens it instead
    // of firing its action, which is what we want here.
    petChangeItem = addItem(menu, @"Change Character", PET_CHANGE);
    NSMenu *pets = [[NSMenu alloc] init];
    [pets setAutoenablesItems:NO];
    petChangeItem.submenu = pets;

    NSMenuItem *top = addItem(menu, @"Always on Top", PET_ONTOP);
    [top setState:onTop ? NSControlStateValueOn : NSControlStateValueOff];

    // A checkbox, like Always on Top, and ticked for the state it names: the
    // pet is on screen almost always, so the box is empty almost always. It
    // was "Show Pet", ticked while the pet was visible — a box that is on
    // whenever nothing is unusual says nothing, and the item somebody hunts
    // for in the menu bar is the one that puts the pet away.
    //
    // Still a toggle rather than a one-way door: it is also how a pet dragged
    // off the edge of the screen comes back.
    NSMenuItem *hide = addItem(menu, @"Hide", PET_HIDE);
    [hide setState:hidden ? NSControlStateValueOn : NSControlStateValueOff];

    addItem(menu, @"Reload", PET_RELOAD);
    // Beside About rather than at the bottom: both answer questions about the
    // program rather than about the character, and somebody looking for one is
    // usually looking for the other — the version in About is the first thing
    // a bug report needs.
    addItem(menu, @"File a Bug", PET_REPORT);
    addItem(menu, @"About", PET_ABOUT);
    // Hidden until there is something to install. It used to be here always,
    // reporting the last check — "Up to date", "Nothing published yet" — which
    // is a line the menu carried every day to be useful on the rare one. The
    // Pet Status panel answers that question with the time of the check beside
    // it, which is the better place for an answer nobody needs at a glance.
    petUpdateItem = addItem(menu, @"", PET_UPDATE);
    [petUpdateItem setEnabled:NO];
    [petUpdateItem setHidden:YES];
    [menu addItem:[NSMenuItem separatorItem]];

    addItem(menu, @"Quit", PET_QUIT);

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

void petStatusSetUpdate(const char *title, int enabled, int shown) {
  NSString *t = [NSString stringWithUTF8String:title];
  onMain(^{
    [petUpdateItem setTitle:t];
    [petUpdateItem setEnabled:enabled ? YES : NO];
    [petUpdateItem setHidden:shown ? NO : YES];
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

void petAlert(const char *title, const char *body) {
  NSString *t = [NSString stringWithUTF8String:title];
  NSString *b = [NSString stringWithUTF8String:body];
  // Runs before Wails has started, so the application object may not exist
  // yet. +sharedApplication creates it; without this the alert never draws.
  [NSApplication sharedApplication];
  // Accessory apps do not come forward on their own, and an alert nobody can
  // see is the failure this exists to fix.
  [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
  [NSApp activateIgnoringOtherApps:YES];
  NSAlert *a = [[NSAlert alloc] init];
  [a setMessageText:t];
  [a setInformativeText:b];
  [a addButtonWithTitle:@"OK"];
  [a runModal];
}

// Both native windows — About and Report a Bug — are the same thing: a titled
// window holding read-only text. What differs is the text and, in the report's
// case, two buttons. The four helpers below are that shared shape; the windows
// themselves are only what each one adds.

// makeLabel is a text field that behaves like a label: no bezel, no
// background, not editable. Selectable when the text is something somebody
// will want to copy — the paths in About, the details in the bug report.
static NSTextField *makeLabel(NSRect frame, NSFont *font, BOOL selectable) {
  NSTextField *f = [[NSTextField alloc] initWithFrame:frame];
  [f setBezeled:NO];
  [f setDrawsBackground:NO];
  [f setEditable:NO];
  [f setSelectable:selectable];
  [f setFont:font];
  return f;
}

static NSWindow *makeInfoWindow(NSString *chromeTitle, NSRect frame) {
  NSWindow *w = [[NSWindow alloc]
      initWithContentRect:frame
                styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable)
                  backing:NSBackingStoreBuffered
                    defer:NO];
  // Closing hides it and the next open reuses it. Left at the default, AppKit
  // releases the window on close and the static becomes a dangling pointer
  // that the second open sends a message to.
  [w setReleasedWhenClosed:NO];
  [w setTitle:chromeTitle];
  return w;
}

// makeButton wires a button to the menu's target and action. A button press
// then reaches Go by the same path a menu click does, carrying a tag; there is
// one callback into Go and no reason to grow a second.
static NSButton *makeButton(NSString *title, int tag, NSRect frame) {
  NSButton *b = [[NSButton alloc] initWithFrame:frame];
  [b setTitle:title];
  [b setBezelStyle:NSBezelStyleRounded];
  [b setTarget:petTarget];
  [b setAction:@selector(onClick:)];
  [b setTag:tag];
  // The resting title, kept on the button itself. petReportFlash borrows the
  // title for a second and needs somewhere to read the real one back from —
  // somewhere a second press cannot have overwritten with "Copied".
  [b setIdentifier:title];
  return b;
}

// reportButton finds one of the window's buttons by its tag. They are not menu
// items, so itemWithTag: does not reach them.
static NSButton *reportButton(int tag) {
  if (petReportCopyBtn != nil && petReportCopyBtn.tag == tag) {
    return petReportCopyBtn;
  }
  if (petReportOpenBtn != nil && petReportOpenBtn.tag == tag) {
    return petReportOpenBtn;
  }
  if (petReportSaveBtn != nil && petReportSaveBtn.tag == tag) {
    return petReportSaveBtn;
  }
  return nil;
}

// centreWindow puts a window in the middle of the display, every time it
// opens rather than only on the first — the point of these windows is that
// they turn up where the eye is rather than wherever the pet was dragged.
//
// Placed by hand rather than with -center, which is documented to put the
// window in the *upper* third: it came out 147pt above the middle, which is
// exactly the complaint this was meant to answer.
static void centreWindow(NSWindow *win) {
  NSRect vis = [[NSScreen mainScreen] visibleFrame];
  NSRect f = [win frame];
  [win setFrameOrigin:NSMakePoint(NSMidX(vis) - f.size.width / 2,
                                  NSMidY(vis) - f.size.height / 2)];
}

// showWindow centres a window and brings it forward. The app is an accessory
// with no Dock icon, so it does not become active by being clicked: without
// the activation the window opens behind whatever has focus, which looks
// exactly like nothing happening.
static void showWindow(NSWindow *win) {
  centreWindow(win);
  [win makeKeyAndOrderFront:nil];
  [NSApp activateIgnoringOtherApps:YES];
}

// describeWindow is how a test sees a native window at all: reading one needs
// accessibility access, which this project does not have. extra is whatever
// the particular window has that its position does not say.
static NSString *describeWindow(NSWindow *win, NSString *extra) {
  if (win == nil || ![win isVisible]) {
    return @"closed";
  }
  NSRect f = [win frame];
  NSRect vis = [[win screen] visibleFrame];
  // Offset from the centre of the visible frame, which is what "centred"
  // means here — AppKit's own -center leaves it slightly high on purpose.
  int dx = (int)llabs((long long)(NSMidX(f) - NSMidX(vis)));
  int dy = (int)llabs((long long)(NSMidY(f) - NSMidY(vis)));
  return [NSString
      stringWithFormat:@"open %dx%d at %d,%d — %d,%d from centre%@",
                       (int)f.size.width, (int)f.size.height, (int)f.origin.x,
                       (int)f.origin.y, dx, dy, extra];
}

// fitReport says whether a wrapped label's text actually fits the box it was
// given. Both info windows are columns of paths, a path is as long as
// somebody's home directory makes it, and nothing in a test can look at a
// native window to notice the end went missing — this line is the only way the
// desktop suite sees it.
static NSString *fitReport(NSTextField *label) {
  NSSize fits =
      [label sizeThatFits:NSMakeSize(label.frame.size.width, 10000)];
  NSSize have = label.frame.size;
  // A point of slack: sizeThatFits rounds up off the typographic height, and a
  // label that fits exactly reports needing a fraction more than it was given.
  if (fits.height > have.height + 1 || fits.width > have.width + 1) {
    return [NSString stringWithFormat:@"text clipped (%dx%d needed, %dx%d given)",
                                      (int)fits.width, (int)fits.height,
                                      (int)have.width, (int)have.height];
  }
  return @"text fits";
}

static int writeReport(NSString *out, char *buf, int cap) {
  const char *s = [out UTF8String];
  int n = (int)strlen(s);
  if (n >= cap) {
    n = cap - 1;
  }
  memcpy(buf, s, n);
  buf[n] = 0;
  return n;
}

static NSWindow *petAboutWindow = nil;
static NSTextField *petAboutTitle = nil;
static NSTextField *petAboutBody = nil;

void petAboutShow(const char *title, const char *body) {
  NSString *t = [NSString stringWithUTF8String:title];
  NSString *b = [NSString stringWithUTF8String:body];
  onMain(^{
    if (petAboutWindow == nil) {
      // Wide and tall enough that the rows have somewhere to go. At 380 the
      // widest line needed 337 of the 340 points the body had: three points,
      // and a home directory six characters longer than this machine's would
      // have spent them. Wrapping keeps the text, but it has to land
      // somewhere — this leaves room for three of the five rows to take two
      // lines before the field runs out.
      NSRect frame = NSMakeRect(0, 0, 520, 300);
      petAboutWindow = makeInfoWindow(@"About", frame);
      CGFloat w = frame.size.width - 40;
      petAboutTitle = makeLabel(NSMakeRect(20, frame.size.height - 58, w, 24),
                                [NSFont boldSystemFontOfSize:16], NO);
      // Selectable: the paths in here are the ones somebody wants to copy.
      petAboutBody = makeLabel(
          NSMakeRect(20, 20, w, frame.size.height - 90),
          [NSFont monospacedSystemFontOfSize:11 weight:NSFontWeightRegular],
          YES);
      // Wrapped, not truncated, for the same reason as the bug report window:
      // the rows are paths, and a path is as long as somebody's home directory
      // makes it. Truncating takes the end off, which is the half that says
      // which of the two apps this is.
      [[petAboutBody cell] setWraps:YES];
      [petAboutBody setLineBreakMode:NSLineBreakByWordWrapping];
      [petAboutWindow.contentView addSubview:petAboutTitle];
      [petAboutWindow.contentView addSubview:petAboutBody];
    }
    [petAboutTitle setStringValue:t];
    [petAboutBody setStringValue:b];
    showWindow(petAboutWindow);
  });
}

void petAboutClose(void) {
  onMain(^{
    [petAboutWindow orderOut:nil];
  });
}

int petAboutReport(char *buf, int cap) {
  __block NSString *out = @"closed";
  void (^check)(void) = ^{
    out = describeWindow(petAboutWindow, [NSString stringWithFormat:@" — %@",
                                          fitReport(petAboutBody)]);
  };
  if ([NSThread isMainThread]) {
    check();
  } else {
    dispatch_sync(dispatch_get_main_queue(), check);
  }
  return writeReport(out, buf, cap);
}

static NSWindow *petReportWindow = nil;
static NSTextField *petReportTitle = nil;
static NSTextField *petReportBody = nil;

void petReportShow(const char *title, const char *body) {
  NSString *t = [NSString stringWithUTF8String:title];
  NSString *b = [NSString stringWithUTF8String:body];
  onMain(^{
    if (petReportWindow == nil) {
      NSRect frame = NSMakeRect(0, 0, 560, 400);
      petReportWindow = makeInfoWindow(@"File a Bug", frame);
      CGFloat w = frame.size.width - 40;
      petReportTitle = makeLabel(NSMakeRect(20, frame.size.height - 58, w, 24),
                                 [NSFont boldSystemFontOfSize:16], NO);
      // Selectable, and the buttons below are the shortcut rather than the
      // only way: somebody who wants three lines of this should be able to
      // take three lines.
      petReportBody = makeLabel(
          NSMakeRect(20, 68, w, frame.size.height - 140),
          [NSFont monospacedSystemFontOfSize:11 weight:NSFontWeightRegular],
          YES);
      // Wrapped, not truncated. The rows here are paths, and a path is as long
      // as somebody's home directory makes it: a label left to truncate would
      // cut off the end of the one thing this window exists to hand over.
      [[petReportBody cell] setWraps:YES];
      [petReportBody setLineBreakMode:NSLineBreakByWordWrapping];
      petReportCopyBtn = makeButton(@"Copy Report", PET_REPORT_COPY,
                                    NSMakeRect(20, 20, 140, 32));
      petReportSaveBtn = makeButton(@"Save Report", PET_REPORT_SAVE,
                                    NSMakeRect(170, 20, 140, 32));
      // Rightmost, because it is the one that leaves the machine.
      petReportOpenBtn = makeButton(@"Report on GitHub", PET_REPORT_OPEN,
                                    NSMakeRect(360, 20, 180, 32));
      [petReportWindow.contentView addSubview:petReportTitle];
      [petReportWindow.contentView addSubview:petReportBody];
      [petReportWindow.contentView addSubview:petReportCopyBtn];
      [petReportWindow.contentView addSubview:petReportSaveBtn];
      [petReportWindow.contentView addSubview:petReportOpenBtn];
    }
    [petReportTitle setStringValue:t];
    [petReportBody setStringValue:b];
    // Back to the resting titles: a window reopened an hour later should not
    // still be claiming something was just copied.
    [petReportCopyBtn setTitle:petReportCopyBtn.identifier];
    [petReportSaveBtn setTitle:petReportSaveBtn.identifier];
    showWindow(petReportWindow);
  });
}

void petReportClose(void) {
  onMain(^{
    [petReportWindow orderOut:nil];
  });
}

void petReportFlash(int tag, const char *title) {
  NSString *t = [NSString stringWithUTF8String:title];
  onMain(^{
    NSButton *b = reportButton(tag);
    if (b == nil) {
      return;
    }
    [b setTitle:t];
    // Restored on a delay rather than left as it is: "Copied" is the answer to
    // the press that just happened, and a button that keeps saying it stops
    // meaning anything the second time.
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(1.6 * NSEC_PER_SEC)),
                   dispatch_get_main_queue(), ^{
                     [b setTitle:b.identifier];
                   });
  });
}

void petRevealFile(const char *path) {
  NSString *p = [NSString stringWithUTF8String:path];
  onMain(^{
    NSURL *u = [NSURL fileURLWithPath:p];
    if (u == nil) {
      return;
    }
    [[NSWorkspace sharedWorkspace] activateFileViewerSelectingURLs:@[ u ]];
  });
}

int petReportReport(char *buf, int cap) {
  __block NSString *out = @"closed";
  void (^check)(void) = ^{
    // Whether the text fits, because the rows in this window are paths and a
    // path is as long as somebody's home directory makes it. A window that
    // silently cut off the end of the log path would be worse than useless —
    // that path is the reason to open it.
    NSString *text = fitReport(petReportBody);
    // The button titles go out too: the copy feedback is a button title, and
    // there is nothing else a test could look at to see that it happened.
    NSString *extra =
        [NSString stringWithFormat:@" — buttons: %@, %@, %@ — %@",
                                   [petReportCopyBtn title],
                                   [petReportSaveBtn title],
                                   [petReportOpenBtn title], text];
    out = describeWindow(petReportWindow, extra);
  };
  if ([NSThread isMainThread]) {
    check();
  } else {
    dispatch_sync(dispatch_get_main_queue(), check);
  }
  return writeReport(out, buf, cap);
}

void petCopyToPasteboard(const char *text) {
  NSString *s = [NSString stringWithUTF8String:text];
  onMain(^{
    NSPasteboard *pb = [NSPasteboard generalPasteboard];
    [pb clearContents];
    [pb setString:s forType:NSPasteboardTypeString];
  });
}

int petOSVersion(char *buf, int cap) {
  // No main thread needed: NSProcessInfo is not an AppKit object.
  return writeReport([[NSProcessInfo processInfo] operatingSystemVersionString],
                     buf, cap);
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
      return;
    }
    // Not everything clickable is in the menu. The bug-report window's buttons
    // carry tags too, and a test has no mouse to press them with.
    NSButton *b = reportButton(tag);
    if (b != nil) {
      [NSApp sendAction:b.action to:b.target from:b];
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
