//go:build !darwin

package desktop

import (
	"context"
	"errors"

	"github.com/lijuno/agent-pet/internal/update"
)

// The status-bar item is macOS-only. See statusitem_darwin.go.
func (a *App) startTray(context.Context) {}

func statusItemReport() string { return "not supported on this platform" }

func dockReport() string { return "not supported on this platform" }

func (a *App) usableArea() rect {
	sw, sh := a.screenSize()
	return rect{W: sw, H: sh - menuBarInset}
}

func (a *App) displayInset() (int, int) { return 0, menuBarInset }
func (a *App) activate()                {}
func (a *App) syncShownCheck(bool)      {}
func (a *App) refreshPetMenu()          {}
func setUpdateItem(update.Status)       {}
func showAbout(string, string)          {}
func closeAbout()                       {}
func aboutReport() string               { return "closed" }

// Alert is a no-op off macOS.
func Alert(string, string)        {}
func openURL(string)              {}
func (a *App) StatusMenu() string { return "" }

func (a *App) ClickStatusItem(string) error {
	return errors.New("no status item on this platform")
}
