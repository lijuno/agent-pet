package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lijunix/agent-digital-pet/internal/config"
	"github.com/lijunix/agent-digital-pet/internal/engine"
	"github.com/lijunix/agent-digital-pet/internal/events"
	"github.com/lijunix/agent-digital-pet/internal/petassets"
	"github.com/lijunix/agent-digital-pet/internal/state"
)

// App is the Wails-facing adapter. It is deliberately thin: it forwards engine
// updates to the frontend and turns frontend calls back into engine calls.
// Nothing in internal/ knows this file exists.
type App struct {
	ctx context.Context
	eng *engine.Engine
	log *slog.Logger

	cfgPath     string
	alwaysOnTop bool
	muted       bool
	addr        string

	// The character-only window size, and where the window was before an
	// overlay grew it. Restoring that rect puts the character back exactly.
	baseW, baseH int
	baseX, baseY int
	grown        bool
}

func NewApp(eng *engine.Engine, log *slog.Logger, cfgPath, addr string) *App {
	cfg := eng.Config()
	w, h := WindowSize(cfg.Pet.Scale)
	return &App{
		eng:         eng,
		log:         log,
		cfgPath:     cfgPath,
		addr:        addr,
		alwaysOnTop: cfg.Pet.AlwaysOnTop,
		baseW:       w,
		baseH:       h,
	}
}

// WindowSize is the window the character alone needs: the sprite at its
// rendered size, the gap the speech bubble sits in above it, and the shadow
// below. main.go opens the window at this size and OpenOverlay grows it from
// here.
func WindowSize(scale float64) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	// 40px frames drawn at 3x, times the user's scale — this mirrors the
	// frontend's own sizing in renderAnimation.
	sprite := int(40 * 3 * scale)
	// bubbleRoom keeps a two-line bubble on screen even with the pet pushed to
	// the very top of the display.
	const bubbleRoom = 56
	const shadow = 8
	w := 300
	if sprite+80 > w {
		w = sprite + 80
	}
	return w, sprite + bubbleRoom + shadow
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	cfg := a.eng.Config()

	if cfg.Window.X >= 0 && cfg.Window.Y >= 0 {
		wruntime.WindowSetPosition(ctx, cfg.Window.X, cfg.Window.Y)
	}
	wruntime.WindowSetAlwaysOnTop(ctx, a.alwaysOnTop)
	a.startTray(ctx)

	// Forward engine updates to the webview. The subscription is lossy by
	// design; the frontend only ever needs the latest picture.
	ch, cancel := a.eng.Subscribe()
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case up, ok := <-ch:
				if !ok {
					return
				}
				if a.muted {
					up.Bubble = nil
				}
				wruntime.EventsEmit(ctx, "pet:update", a.decorate(up))
			}
		}
	}()
}

// The window is normally only as tall as the character and the speech bubble
// above it. Every extra pixel is transparent dead space that the user cannot
// get rid of: macOS keeps a dragged window's top edge below the menu bar, so
// space reserved above the pet is screen the pet can never reach. Reserving a
// panel's worth of it put the whole top third of a display out of bounds.
//
// The panel gets its room when it is opened, and gives it back on close.

// menuBarInset is how much of the top of the screen a dragged window cannot
// enter. The Wails screen API reports size but not the usable area, so this is
// an estimate; being wrong only costs one panel opening downward when it could
// have opened upward.
const menuBarInset = 28

// sideMargin keeps an overlay off the very edge of the window.
const sideMargin = 8

type rect struct{ X, Y, W, H int }

// Placement is where the window goes while an overlay is open, and where the
// character sits inside it. It crosses to the frontend, which cannot know any
// of this: a webview has no idea where its window is on screen.
type Placement struct {
	// Side is "above" or "below" — which side of the character the overlay
	// is drawn on.
	Side string `json:"side"`
	// PetX is the character's centre, measured from the window's left edge.
	// It is normally half the window, but not when the window had to be
	// pushed sideways to keep the overlay on screen.
	PetX int `json:"pet_x"`
}

// placeOverlay fits a window containing both the character and an ow x oh
// overlay onto the screen, without moving the character.
//
// Vertically the overlay goes above when there is room and below when there is
// not, and the window grows by exactly the overlay's height either way. That
// keeps the character still: growing upward moves the top edge and leaves the
// bottom, growing downward does the reverse, and the frontend anchors the pet
// to whichever edge did not move.
//
// Horizontally the window is widened to hold the overlay and then slid back
// onto the screen if it hangs off an edge. Sliding the window would normally
// drag the character with it, which is why PetX comes back out: the frontend
// puts the character at that offset instead of in the middle, so it stays
// exactly where the user parked it while the panel beside it moves.
func placeOverlay(base rect, ow, oh, screenW, screenH int) (rect, Placement) {
	petCX := base.X + base.W/2

	roomAbove := base.Y - menuBarInset
	roomBelow := screenH - (base.Y + base.H)
	// Prefer above, which is where the panel has always been; drop below only
	// when it genuinely does not fit and below is the roomier side.
	below := roomAbove < oh && roomBelow > roomAbove

	out := rect{W: base.W, H: base.H + oh, Y: base.Y}
	if ow+2*sideMargin > out.W {
		out.W = ow + 2*sideMargin
	}
	if !below {
		out.Y = base.Y - oh
	}

	out.X = petCX - out.W/2
	if limit := screenW - out.W; limit < 0 {
		out.X = 0 // a window wider than the screen: nothing to choose between
	} else if out.X > limit {
		out.X = limit
	} else if out.X < 0 {
		out.X = 0
	}

	side := "above"
	if below {
		side = "below"
	}
	return out, Placement{Side: side, PetX: petCX - out.X}
}

// OpenOverlay makes room for an ow x oh overlay and reports where the frontend
// should draw the character and the panel. See placeOverlay.
func (a *App) OpenOverlay(ow, oh int) Placement {
	centred := Placement{Side: "above", PetX: a.baseW / 2}
	if a.ctx == nil || oh <= 0 {
		return centred
	}
	a.CloseOverlay() // never stack two growths

	x, y := wruntime.WindowGetPosition(a.ctx)
	a.baseX, a.baseY, a.grown = x, y, true

	sw, sh := a.screenSize()
	r, p := placeOverlay(rect{X: x, Y: y, W: a.baseW, H: a.baseH}, ow, oh, sw, sh)

	// Size and position are both set explicitly rather than trusting which
	// edge a resize anchors to, which is a platform detail.
	wruntime.WindowSetSize(a.ctx, r.W, r.H)
	wruntime.WindowSetPosition(a.ctx, r.X, r.Y)
	return p
}

// CloseOverlay returns the window to the character alone, exactly where it was.
func (a *App) CloseOverlay() {
	if a.ctx == nil || !a.grown {
		return
	}
	wruntime.WindowSetSize(a.ctx, a.baseW, a.baseH)
	wruntime.WindowSetPosition(a.ctx, a.baseX, a.baseY)
	a.grown = false
}

// Sizes are the presets offered in the menu. Free-form scales still work in
// config.yaml; these are the three worth clicking.
var Sizes = []struct {
	Name  string  `json:"name"`
	Scale float64 `json:"scale"`
}{
	{"Small", 0.7},
	{"Medium", 1.0},
	{"Large", 1.5},
}

// SizeName is the preset a scale corresponds to, or "" for a hand-edited one.
// Matching on a tolerance rather than equality keeps a float that has been
// through YAML and JSON from failing to tick its own box.
func SizeName(scale float64) string {
	for _, s := range Sizes {
		if d := s.Scale - scale; d < 0.01 && d > -0.01 {
			return s.Name
		}
	}
	return ""
}

// ListSizes backs the size section of the menu.
func (a *App) ListSizes() any { return Sizes }

// SetScale resizes the character and remembers the choice.
//
// The window has to be resized with it, and repositioned so the character does
// not walk across the screen every time the size changes: the pet is anchored
// to the bottom centre of the window, so that point is what must stay put.
func (a *App) SetScale(scale float64) {
	if scale <= 0 {
		return
	}
	cfg := a.eng.Config()
	cfg.Pet.Scale = scale
	a.eng.SetConfig(cfg)
	if err := config.Save(a.cfgPath, cfg); err != nil {
		a.log.Warn("could not save config", "err", err)
	}

	if a.ctx == nil {
		return
	}
	a.CloseOverlay()
	oldW, oldH := a.baseW, a.baseH
	a.baseW, a.baseH = WindowSize(scale)

	x, y := wruntime.WindowGetPosition(a.ctx)
	// Keep the character's feet where they were.
	petCX := x + oldW/2
	petBottom := y + oldH
	wruntime.WindowSetSize(a.ctx, a.baseW, a.baseH)
	wruntime.WindowSetPosition(a.ctx, petCX-a.baseW/2, petBottom-a.baseH)

	a.push()
}

// push re-sends the current view, so a change made here reaches the window
// without waiting for the next agent event.
func (a *App) push() {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "pet:update", a.decorate(a.eng.Last()))
}

// screenSize is the display the window is on. A sane fallback matters more
// than precision here: getting it wrong costs a panel that opens on the side
// with less room, not a broken window.
func (a *App) screenSize() (int, int) {
	screens, err := wruntime.ScreenGetAll(a.ctx)
	if err == nil {
		for _, s := range screens {
			if s.IsCurrent && s.Size.Width > 0 {
				return s.Size.Width, s.Size.Height
			}
		}
		for _, s := range screens {
			if s.IsPrimary && s.Size.Width > 0 {
				return s.Size.Width, s.Size.Height
			}
		}
	}
	return 1440, 900
}

func (a *App) shutdown(ctx context.Context) {
	// Save where the character is, not where a grown window happens to start.
	a.CloseOverlay()

	// Remember where the user parked the pet.
	x, y := wruntime.WindowGetPosition(ctx)
	cfg := a.eng.Config()
	cfg.Window.X, cfg.Window.Y = x, y
	cfg.Pet.AlwaysOnTop = a.alwaysOnTop
	if err := config.Save(a.cfgPath, cfg); err != nil {
		a.log.Warn("could not save config", "err", err)
	}
}

// View is the payload the frontend renders. It bundles the engine update with
// everything needed to draw: which pet, which animation, where the frames are.
type View struct {
	engine.Update
	Animation *AnimationView `json:"animation"`
	PetName   string         `json:"pet_name"`
	Muted     bool           `json:"muted"`
	OnTop     bool           `json:"on_top"`
	Scale     float64        `json:"scale"`
}

type AnimationView struct {
	// Resolved is the state actually being drawn, which differs from the pet
	// state when the pack is missing that animation.
	Resolved    string  `json:"resolved"`
	URL         string  `json:"url"`
	Frames      int     `json:"frames"`
	FPS         float64 `json:"fps"`
	Loop        bool    `json:"loop"`
	FrameWidth  int     `json:"frame_width"`
	FrameHeight int     `json:"frame_height"`
	Pixelated   bool    `json:"pixelated"`
}

func (a *App) decorate(up engine.Update) View {
	v := View{
		Update: up,
		Muted:  a.muted,
		OnTop:  a.alwaysOnTop,
		Scale:  a.eng.Config().Pet.Scale,
	}
	p, ok := a.eng.ActivePet()
	if !ok {
		return v
	}
	v.PetName = p.Name
	resolved, anim, found := p.Resolve(up.Snapshot.State)
	if !found {
		return v
	}
	v.Animation = &AnimationView{
		Resolved:    string(resolved),
		URL:         p.BaseURL + anim.File,
		Frames:      anim.Frames,
		FPS:         anim.FPS,
		Loop:        anim.Loop,
		FrameWidth:  p.FrameWidth,
		FrameHeight: p.FrameHeight,
		Pixelated:   p.Pixelated,
	}
	return v
}

// ---- methods bound to the frontend ----

// GetState returns the current view. Called once on load; after that the
// frontend lives on the pet:update event.
func (a *App) GetState() View {
	return a.decorate(a.eng.Last())
}

func (a *App) ListPets() []petassets.Pet { return a.eng.Library().List() }

func (a *App) SetPet(id string) bool {
	_, ok := a.eng.SetPet(id)
	if ok {
		cfg := a.eng.Config()
		_ = config.Save(a.cfgPath, cfg)
	}
	return ok
}

// Interact is the double-click reaction (§14).
func (a *App) Interact() {
	a.eng.Submit(events.Event{Source: "ui", Event: events.PetInteraction, SessionID: "ui"})
}

func (a *App) SetAlwaysOnTop(b bool) {
	a.alwaysOnTop = b
	wruntime.WindowSetAlwaysOnTop(a.ctx, b)
}

func (a *App) SetMuted(b bool) { a.muted = b }

// Sleep sends the pet to sleep on demand. It is a forced state rather than a
// fake event: pretending an agent went idle would corrupt the session model.
func (a *App) Sleep() {
	a.eng.Force(state.Sleeping, 8*time.Hour)
}

func (a *App) Wake() { a.eng.ClearForce() }

// Info backs the status and statistics panels.
type Info struct {
	Version     string   `json:"version"`
	Addr        string   `json:"addr"`
	ConfigPath  string   `json:"config_path"`
	PetsDir     string   `json:"pets_dir"`
	Personality string   `json:"personality"`
	States      []string `json:"states"`
}

func (a *App) GetInfo() Info {
	cfg := a.eng.Config()
	out := Info{
		Version:     version,
		Addr:        a.addr,
		ConfigPath:  a.cfgPath,
		PetsDir:     config.PetsDir(),
		Personality: cfg.Personality.Preset,
	}
	for _, s := range state.All() {
		out.States = append(out.States, string(s))
	}
	return out
}

func (a *App) Quit() { wruntime.Quit(a.ctx) }

// showWindow and emitPanel exist for the optional status-bar menu, which has to
// reach into the window from outside the webview.
func (a *App) showWindow() {
	wruntime.WindowShow(a.ctx)
	wruntime.WindowSetAlwaysOnTop(a.ctx, a.alwaysOnTop)
}

func (a *App) emitPanel(kind string) {
	a.showWindow()
	wruntime.EventsEmit(a.ctx, "pet:panel", kind)
}

// appMenu builds the macOS menu bar menu. The same items appear in the pet's
// own right-click menu in the webview; see ADR 0005 for why there are two.
func (a *App) appMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu())

	pet := m.AddSubmenu("Pet")
	pet.AddText("Pet Status", keys.CmdOrCtrl("i"), func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "pet:panel", "status")
	})
	pet.AddText("Statistics", keys.CmdOrCtrl("s"), func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "pet:panel", "stats")
	})
	pet.AddSeparator()
	pet.AddText("Change Pet…", nil, func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "pet:panel", "pets")
	})
	pet.AddSeparator()

	top := pet.AddCheckbox("Always on Top", a.alwaysOnTop, nil, nil)
	top.OnClick(func(cb *menu.CallbackData) {
		a.SetAlwaysOnTop(cb.MenuItem.Checked)
	})
	mute := pet.AddCheckbox("Mute", a.muted, nil, nil)
	mute.OnClick(func(cb *menu.CallbackData) {
		a.SetMuted(cb.MenuItem.Checked)
		wruntime.EventsEmit(a.ctx, "pet:muted", cb.MenuItem.Checked)
	})

	pet.AddSeparator()
	pet.AddText("Sleep", nil, func(*menu.CallbackData) { a.Sleep() })
	pet.AddText("Wake", nil, func(*menu.CallbackData) { a.Wake() })
	pet.AddSeparator()
	pet.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.Quit() })

	m.Append(menu.EditMenu())
	return m
}
