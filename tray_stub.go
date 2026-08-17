//go:build !darwin

package main

import "context"

// The status-bar item is macOS-only. See statusitem_darwin.go.
func (a *App) startTray(context.Context) {}

func statusItemReport() string { return "not supported on this platform" }

func (a *App) visibleFrame() rect {
	sw, sh := a.screenSize()
	return rect{X: 0, Y: menuBarInset, W: sw, H: sh - menuBarInset}
}
