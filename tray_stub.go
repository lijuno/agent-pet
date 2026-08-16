//go:build !(tray && darwin)

package main

import "context"

// startTray is a no-op unless the app was built with `-tags tray` on macOS.
// See ADR 0005 and tray_darwin.go.
func (a *App) startTray(context.Context) {}
