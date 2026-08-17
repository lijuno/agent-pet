//go:build !darwin

package main

import "context"

// The status-bar item is macOS-only. See statusitem_darwin.go.
func (a *App) startTray(context.Context) {}
