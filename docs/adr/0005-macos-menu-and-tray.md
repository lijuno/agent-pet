# ADR 0005 — Menu bar: native app menu now, status-bar icon behind a build tag

Status: accepted (Milestone 1)

## Context

The product calls for a floating pet plus a menu bar presence (§14's right-click
menu: Pet Status, Change Pet, Always on Top, Mute, Sleep, Statistics, Quit).

Wails v2 has no built-in system tray / status bar item. It is a Wails v3 feature.
Third-party options (`fyne.io/systray`, `energye/systray`) work but must share
the Cocoa main run loop with Wails, which is the fragile part of the setup.

## Decision

Three surfaces, in order of reliability:

1. **In-window context menu.** Right-clicking the pet opens an HTML menu
   rendered in the transparent window. This is the primary control surface and
   has no platform risk.
2. **Native macOS application menu.** Wails' `Menu` option puts a real
   `Digital Pet` menu in the macOS menu bar with the same items, available
   whenever the app is frontmost.
3. **Persistent status-bar icon** — implemented in `tray_darwin.go` behind the
   `tray` build tag, using `fyne.io/systray`'s `Register` (external-run-loop)
   entry point so it does not fight Wails for the main thread. Off by default.

Build with the icon:

```
wails build -tags tray
```

## Consequences

- The default build has no extra native dependency and cannot fail to compile
  because of the tray.
- Users who want the always-present icon opt in with one flag; if the shared
  run loop misbehaves on a given macOS version, the default build is unaffected.
- Wails v3 will make surface 3 first-class; the menu construction is already
  factored into `menuItems()` so it can be reused verbatim.
