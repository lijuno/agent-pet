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

void petStatusInstall(const void *png, int len, int onTop, int muted) {
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

    addItem(menu, @"Show Pet", PET_SHOW);
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
