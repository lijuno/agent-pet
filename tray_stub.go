//go:build !darwin

package main

import (
	"context"
	"errors"
)

// The status-bar item is macOS-only. See statusitem_darwin.go.
func (a *App) startTray(context.Context) {}

func statusItemReport() string { return "not supported on this platform" }

func (a *App) usableArea() rect {
	sw, sh := a.screenSize()
	return rect{W: sw, H: sh - menuBarInset}
}

func (a *App) displayInset() (int, int) { return 0, menuBarInset }
func (a *App) activate()                {}
func (a *App) syncShownCheck(bool)      {}
func (a *App) refreshPetMenu()          {}
func (a *App) StatusMenu() string       { return "" }

func (a *App) ClickStatusItem(string) error {
	return errors.New("no status item on this platform")
}
