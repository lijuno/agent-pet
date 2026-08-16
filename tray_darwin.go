//go:build tray && darwin

// A persistent macOS status-bar item.
//
// This file is excluded from the default build. See ADR 0005: Wails v2 has no
// built-in status-bar support, and any third-party implementation has to share
// the Cocoa main run loop with Wails. `systray.Register` is the entry point
// designed for exactly that ("useful if the program needs to show other UI
// elements, for example, webview"), but it is still the most platform-fragile
// part of the app, so it is opt-in:
//
//	wails build -tags tray
//
// If the icon misbehaves on your macOS version, rebuild without the tag; you
// lose nothing but the status-bar item — the in-window right-click menu and the
// application menu carry the same commands.
package main

import (
	"context"
	_ "embed"
	"sync"

	"fyne.io/systray"

	"github.com/lijunix/agent-digital-pet/internal/state"
)

//go:embed build/trayicon.png
var trayIcon []byte

var trayOnce sync.Once

// startTray is called from App.startup on builds that include this file.
func (a *App) startTray(ctx context.Context) {
	trayOnce.Do(func() {
		systray.Register(func() { a.buildTray(ctx) }, nil)
	})
}

func (a *App) buildTray(ctx context.Context) {
	// A template icon lets macOS invert it automatically in dark mode.
	systray.SetTemplateIcon(trayIcon, trayIcon)
	systray.SetTooltip("Digital Pet")

	stateItem := systray.AddMenuItem("Idle", "Current pet state")
	stateItem.Disable()
	systray.AddSeparator()

	show := systray.AddMenuItem("Show Pet", "Bring the pet window to the front")
	status := systray.AddMenuItem("Pet Status", "Open the status panel")
	stats := systray.AddMenuItem("Statistics", "Open the statistics panel")
	change := systray.AddMenuItem("Change Pet…", "Pick a different character")
	systray.AddSeparator()
	onTop := systray.AddMenuItemCheckbox("Always on Top", "Keep the pet above other windows", a.alwaysOnTop)
	mute := systray.AddMenuItemCheckbox("Mute", "Stop speech bubbles", a.muted)
	systray.AddSeparator()
	sleep := systray.AddMenuItem("Sleep", "Send the pet to sleep")
	wake := systray.AddMenuItem("Wake", "Wake the pet up")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Quit Digital Pet")

	// Keep the disabled first item in sync with the pet, so the state is
	// readable without opening the window at all.
	go func() {
		ch, cancel := a.eng.Subscribe()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case up, ok := <-ch:
				if !ok {
					return
				}
				stateItem.SetTitle(trayLabel(up.Snapshot.State))
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-show.ClickedCh:
				a.showWindow()
			case <-status.ClickedCh:
				a.emitPanel("status")
			case <-stats.ClickedCh:
				a.emitPanel("stats")
			case <-change.ClickedCh:
				a.emitPanel("pets")
			case <-onTop.ClickedCh:
				next := !onTop.Checked()
				a.SetAlwaysOnTop(next)
				setChecked(onTop, next)
			case <-mute.ClickedCh:
				next := !mute.Checked()
				a.SetMuted(next)
				setChecked(mute, next)
			case <-sleep.ClickedCh:
				a.Sleep()
			case <-wake.ClickedCh:
				a.Wake()
			case <-quit.ClickedCh:
				a.Quit()
				return
			}
		}
	}()
}

func setChecked(i *systray.MenuItem, v bool) {
	if v {
		i.Check()
	} else {
		i.Uncheck()
	}
}

func trayLabel(s state.State) string {
	switch s {
	case state.Attention:
		return "Needs you"
	case state.Working:
		return "Working"
	case state.Thinking:
		return "Thinking"
	case state.Confused:
		return "Something failed"
	case state.Worried:
		return "Repeated failures"
	case state.Happy:
		return "Task done"
	case state.Celebrate:
		return "Tests passed"
	case state.Tired:
		return "Long session"
	case state.Sleeping:
		return "Sleeping"
	case state.Heart:
		return "Hello"
	default:
		return "Idle"
	}
}
