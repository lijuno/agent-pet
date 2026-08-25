package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/engine"
	"github.com/lijuno/agent-pet/internal/events"
	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/petassets"
	"github.com/lijuno/agent-pet/internal/state"
	"github.com/lijuno/agent-pet/internal/update"
)

// App is the Wails-facing adapter. It is deliberately thin: it forwards engine
// updates to the frontend and turns frontend calls back into engine calls.
// The dependency runs one way — nothing the engine, the state machine or the
// server does knows this package exists.
//
// Wails binds every exported method here, and names the JavaScript namespace
// after this package: the frontend reaches them as window.go.desktop.App.
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

	// Where the last overlay landed, for diagnostics. Written from the
	// frontend thread, read by an HTTP handler.
	mu         sync.Mutex
	overlay    rect
	hasOverlay bool

	// hidden mirrors the window's visibility, so the Show Pet checkbox can
	// report it. Wails has no "is the window visible" call to ask.
	hidden bool

	// upd is the last update check result, as reported by petctl over the
	// event API. The app never works this out for itself: it holds no HTTP
	// client and opens no connections (ADR 0008).
	updMu sync.Mutex
	upd   update.Status
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
		hidden:      cfg.Window.StartHidden,
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
		lastPet := a.eng.Last().Pet
		for {
			select {
			case <-ctx.Done():
				return
			case up, ok := <-ch:
				if !ok {
					return
				}
				// Every route to a different character passes through here: the
				// menu bar, the pet's own menu, and the loopback API. Hooking
				// any one of them left the menu bar ticking a character that
				// was no longer on screen.
				if up.Pet != lastPet {
					lastPet = up.Pet
					a.refreshPetMenu()
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
// screen is the usable area of the display: the whole thing minus the menu bar
// and the Dock. Placing against the full display puts a menu behind the Dock,
// which looks exactly like falling off the bottom of the screen.
func placeOverlay(base rect, ow, oh int, screen rect) (rect, Placement) {
	petCX := base.X + base.W/2

	// With an unknown screen, keep the panel where it has always been rather
	// than move it on a guess.
	below := false
	if screen.H > 0 {
		roomAbove := base.Y - screen.Y
		roomBelow := (screen.Y + screen.H) - (base.Y + base.H)
		// Prefer above; drop below only when it genuinely does not fit and
		// below is the roomier side.
		below = roomAbove < oh && roomBelow > roomAbove
	}

	out := rect{W: base.W, H: base.H + oh, Y: base.Y}
	if ow+2*sideMargin > out.W {
		out.W = ow + 2*sideMargin
	}
	if !below {
		out.Y = base.Y - oh
	}

	out.X = petCX - out.W/2
	if screen.W > 0 {
		if limit := screen.X + screen.W - out.W; limit < screen.X {
			out.X = screen.X // wider than the screen: nothing to choose between
		} else if out.X > limit {
			out.X = limit
		} else if out.X < screen.X {
			out.X = screen.X
		}
	}

	petX := petCX - out.X
	// The character can be dragged far enough off the side of the screen that
	// its centre is outside the window we just pulled back on. Placing it there
	// would hide it completely the moment a menu opened. Keep it at the edge
	// it went out of.
	if petX < 0 {
		petX = 0
	} else if petX > out.W {
		petX = out.W
	}

	side := "above"
	if below {
		side = "below"
	}
	return out, Placement{Side: side, PetX: petX}
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

	r, p := placeOverlay(rect{X: x, Y: y, W: a.baseW, H: a.baseH}, ow, oh, a.usableArea())

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

// OpenPanel and MoveWindow let the loopback API drive the window. They do
// nothing a user cannot do with a mouse; they exist because a test has no
// mouse, and the only way to know whether a menu is clipped in a corner is to
// open one in a corner.
func (a *App) OpenPanel(kind string) error {
	if a.ctx == nil {
		return fmt.Errorf("no window")
	}
	switch kind {
	case "about", "about-close":
		// Not panels: About is a native window, not an overlay in the webview.
		// Handled here anyway because OpenPanel is the only door the loopback
		// API has into the window, and a test needs to open and close it the
		// same way it does everything else it cannot click.
		if kind == "about" {
			a.ShowAbout()
		} else {
			closeAbout()
		}
		return nil
	case "status", "stats", "pets", "menu", "close":
	default:
		return fmt.Errorf("unknown panel %q", kind)
	}
	wruntime.EventsEmit(a.ctx, "pet:panel", kind)
	return nil
}

func (a *App) MoveWindow(x, y int) error {
	if a.ctx == nil {
		return fmt.Errorf("no window")
	}
	a.CloseOverlay()
	wruntime.WindowSetPosition(a.ctx, x, y)
	a.mu.Lock()
	a.hasOverlay = false
	a.mu.Unlock()
	return nil
}

// ReportOverlay is the frontend telling the backend where an overlay actually
// landed, in window coordinates.
//
// It exists to be tested. Whether a menu is clipped by the edge of a screen is
// invisible to every other kind of check here: the geometry unit tests only
// prove the arithmetic, and nothing on this machine is allowed to look at the
// screen. With this, `petctl doctor` can state where the menu really is and
// whether it fits, and a test can drive the window into each corner and read
// the answer back.
func (a *App) ReportOverlay(left, top, width, height int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.overlay = rect{X: left, Y: top, W: width, H: height}
	a.hasOverlay = width > 0 && height > 0
}

// overlayReport describes the last overlay's position on screen, and whether
// any of it fell off.
func (a *App) overlayReport() string {
	a.mu.Lock()
	o, ok := a.overlay, a.hasOverlay
	a.mu.Unlock()
	if !ok {
		return "none open"
	}
	wx, wy := wruntime.WindowGetPosition(a.ctx)
	v := a.usableArea()
	l, t := wx+o.X, wy+o.Y
	r, b := l+o.W, t+o.H

	var off []string
	if l < v.X {
		off = append(off, fmt.Sprintf("%dpt off the left", v.X-l))
	}
	if r > v.X+v.W {
		off = append(off, fmt.Sprintf("%dpt off the right", r-(v.X+v.W)))
	}
	if t < v.Y {
		off = append(off, fmt.Sprintf("%dpt under the menu bar", v.Y-t))
	}
	if b > v.Y+v.H {
		off = append(off, fmt.Sprintf("%dpt behind the Dock or off the bottom", b-(v.Y+v.H)))
	}
	where := fmt.Sprintf("%dx%d at %d,%d", o.W, o.H, l, t)
	if len(off) == 0 {
		return where + " — fully on screen"
	}
	return where + " — " + strings.Join(off, ", ")
}

// DesktopDiagnostics reports what the window and the menu bar are actually
// doing. None of it can be seen from a test — there is no screen to look at —
// so the app is asked instead, and `petctl doctor` prints the answer.
func (a *App) DesktopDiagnostics() map[string]string {
	out := map[string]string{"menu_bar": statusItemReport(), "dock": dockReport()}
	if a.ctx == nil {
		return out
	}
	out["overlay"] = a.overlayReport()
	out["status_menu"] = a.StatusMenu()
	// A native window is one a test has no other way to see: it is not in the
	// webview and nothing may read a real menu or window without accessibility
	// access, which this project does not have.
	out["about"] = aboutReport()
	if a.hidden {
		out["visible"] = "no — hidden from the menu bar"
	} else {
		out["visible"] = "yes"
	}
	x, y := wruntime.WindowGetPosition(a.ctx)
	w, h := wruntime.WindowGetSize(a.ctx)
	out["window"] = fmt.Sprintf("%dx%d at %d,%d", w, h, x, y)
	out["window_base"] = fmt.Sprintf("%dx%d", a.baseW, a.baseH)

	sw, sh := a.screenSize()
	out["screen"] = fmt.Sprintf("%dx%d", sw, sh)
	v := a.usableArea()
	// Window positions are relative to the usable area, so it always starts at
	// 0,0 — printing its size next to the display's is the difference that
	// matters, and the one that was being got wrong.
	ix, iy := a.displayInset()
	out["usable"] = fmt.Sprintf("%dx%d (windows are placed in this, from 0,0)", v.W, v.H)
	out["display_inset"] = fmt.Sprintf("%d left, %d top taken by the Dock and menu bar", ix, iy)
	if screens, err := wruntime.ScreenGetAll(a.ctx); err == nil {
		for _, s := range screens {
			if s.IsCurrent {
				// Logical and physical differ on a Retina display, and using
				// the wrong one silently disables every edge calculation.
				out["screen_physical"] = fmt.Sprintf("%dx%d", s.PhysicalSize.Width, s.PhysicalSize.Height)
				break
			}
		}
	}
	return out
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
	Animation  *AnimationView `json:"animation"`
	PetName    string         `json:"pet_name"`
	Muted      bool           `json:"muted"`
	OnTop      bool           `json:"on_top"`
	Scale      float64        `json:"scale"`
	DropShadow bool           `json:"drop_shadow"`
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
		Update:     up,
		Muted:      a.muted,
		OnTop:      a.alwaysOnTop,
		Scale:      a.eng.Config().Pet.Scale,
		DropShadow: a.eng.Config().Pet.DropShadow,
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

// SetUpdate records what a check found and retitles the menu-bar item. Called
// by the server when petctl posts a result; there is no other way for the app
// to learn one.
func (a *App) SetUpdate(st update.Status) {
	a.updMu.Lock()
	a.upd = st
	a.updMu.Unlock()
	setUpdateItem(st)
}

// GetUpdate is what the frontend shows in the status panel.
func (a *App) GetUpdate() update.Status {
	a.updMu.Lock()
	defer a.updMu.Unlock()
	return a.upd
}

// UpdateChannel is the channel this build follows. It is a fact about the
// build, not a setting: the other channel is a different application.
func (a *App) UpdateChannel() update.Channel { return flavor.Current().Channel }

// updateItemTitle is what the menu bar's update item says. Kept out of the
// cgo file so it can be tested without a menu bar to look at.
//
// The item is always shown. Hidden until an update existed, it meant the menu
// could never answer "have I got the latest?" — which is the question somebody
// opens it to ask, and the one they cannot answer any other way without a
// terminal.
//
// It reports the last result; it does not check. The daemon opens no outbound
// connections and spawns no processes (ADR 0008), so checking happens in
// `petctl` — once a day from the Claude Code hook, or on demand from a shell.
//
// No relative time in the title. Nothing rebuilds this menu while it sits in
// the menu bar, so "checked 2 minutes ago" would still say that four hours
// later, and a label that goes quietly wrong is worse than no label.
func updateItemTitle(st update.Status) string {
	switch {
	case st.Available && st.Latest != "":
		return "Update to " + st.Latest + "…"
	case st.Error != "":
		// "no update" and "could not find out" are different answers and the
		// pet does not guess between them — same rule as Status.Error itself.
		return "Update check failed"
	case st.CheckedAt.IsZero():
		return "No update check yet"
	case st.Latest == "":
		return "Nothing published yet"
	case st.Latest == st.Current:
		return "Up to date"
	default:
		// The running build is newer than the channel offers. Normal on a
		// prerelease, and neither an update nor "up to date".
		return "Ahead of the channel"
	}
}

// openReleaseNotes opens the page for the version on offer. The pet installs
// nothing itself: it has no updater in it, and `petctl update` is what does the
// work.
func (a *App) openReleaseNotes() {
	a.updMu.Lock()
	url := a.upd.NotesURL
	a.updMu.Unlock()
	if url != "" {
		openURL(url)
	}
}

// SetDropShadow turns the shadow behind the sprite on or off and remembers the
// choice. Unlike the size, nothing about the window changes: the shadow is
// drawn inside the frame the character already occupies.
func (a *App) SetDropShadow(b bool) {
	cfg := a.eng.Config()
	cfg.Pet.DropShadow = b
	a.eng.SetConfig(cfg)
	if err := config.Save(a.cfgPath, cfg); err != nil {
		a.log.Warn("could not save config", "err", err)
	}
}

// Info backs the status and statistics panels.
type Info struct {
	Version string `json:"version"`
	// AppName and Channel are what distinguish the two applications, which run
	// side by side and otherwise look identical in a panel. See ADR 0008.
	AppName     string   `json:"app_name"`
	Channel     string   `json:"channel"`
	Addr        string   `json:"addr"`
	ConfigPath  string   `json:"config_path"`
	PetsDir     string   `json:"pets_dir"`
	Personality string   `json:"personality"`
	States      []string `json:"states"`
}

func (a *App) GetInfo() Info {
	cfg := a.eng.Config()
	out := Info{
		Version:     Version,
		AppName:     flavor.Current().AppName,
		Channel:     string(flavor.Current().Channel),
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

// aboutText builds the two strings the About window shows. Split out of
// ShowAbout so it can be tested without a window: this is columns of text, and
// columns are exactly the thing that quietly stops lining up.
//
// It names the application, not the pet. Two of these run side by side with
// separate settings, ports and update channels (ADR 0008), and "which one am I
// looking at" is the question this window exists to answer. The character is a
// detail about the app, so it is a row like any other.
func aboutText(in Info, petName string) (string, string) {
	var b strings.Builder
	b.WriteString("A desktop pet that reacts to what a coding agent is doing.\n\n")
	for _, r := range [][2]string{
		{"Version", in.Version},
		{"Channel", in.Channel},
		{"Event API", in.Addr},
		{"Character", petName},
		{"Config", in.ConfigPath},
		{"Pets", in.PetsDir},
	} {
		v := r[1]
		if v == "" {
			// An empty column is indistinguishable from a broken one.
			v = "—"
		}
		fmt.Fprintf(&b, "%-10s %s\n", r[0], v)
	}
	name := in.AppName
	if name == "" {
		name = "Agent Pet"
	}
	return name, strings.TrimRight(b.String(), "\n")
}

// ShowAbout opens the About window. Bound to the frontend as well as reached
// from the status item, so both menus open the same one window.
func (a *App) ShowAbout() {
	var petName string
	if p, ok := a.eng.ActivePet(); ok {
		petName = p.Name
	}
	showAbout(aboutText(a.GetInfo(), petName))
}

// CloseAbout hides it again.
func (a *App) CloseAbout() { closeAbout() }

func (a *App) Quit() { wruntime.Quit(a.ctx) }

// showWindow and emitPanel exist for the optional status-bar menu, which has to
// reach into the window from outside the webview.
// SetShown puts the pet on screen or takes it away. It backs the Show Pet
// checkbox in the menu-bar menu, which is the only surface that can offer it:
// a hidden pet cannot be clicked to bring itself back.
func (a *App) SetShown(shown bool) {
	if a.ctx == nil {
		return
	}
	if shown {
		a.hidden = false
		a.showWindow()
	} else {
		a.CloseOverlay()
		wruntime.WindowHide(a.ctx)
		a.hidden = true
	}
	// Tick the box here rather than at the call site: visibility changes from
	// the menu, from the API and from startup, and a checkbox that only tracks
	// one of those is lying the rest of the time.
	a.syncShownCheck(!a.hidden)
}

// Shown reports whether the pet is on screen.
func (a *App) Shown() bool { return !a.hidden }

// showWindow is "Show Pet". Three things have to be true for that to mean
// anything, and only the first was being done:
//
//   - the window is shown and un-minimised;
//   - the application is frontmost, or the window is shown behind whatever the
//     user is actually looking at;
//   - the window is somewhere on the screen. The pet can be dragged almost
//     entirely off an edge, and "Show Pet" is exactly what someone reaches for
//     when they have lost it — so if it is outside the usable area, bring it
//     back to the nearest point inside.
func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	a.CloseOverlay()
	a.hidden = false
	wruntime.WindowUnminimise(a.ctx)
	wruntime.WindowShow(a.ctx)
	wruntime.WindowSetAlwaysOnTop(a.ctx, a.alwaysOnTop)
	a.activate()

	v := a.usableArea()
	if v.W == 0 {
		return
	}
	x, y := wruntime.WindowGetPosition(a.ctx)
	nx, ny := clampToArea(x, y, a.baseW, a.baseH, v)
	if nx != x || ny != y {
		wruntime.WindowSetPosition(a.ctx, nx, ny)
	}
}

// clampToArea brings a window fully back inside the usable area, moving it as
// little as possible.
func clampToArea(x, y, w, h int, v rect) (int, int) {
	if v.W <= 0 || v.H <= 0 {
		return x, y // unknown screen: leave the window alone
	}
	if right := v.X + v.W - w; x > right {
		x = right
	}
	if x < v.X {
		x = v.X
	}
	if bottom := v.Y + v.H - h; y > bottom {
		y = bottom
	}
	if y < v.Y {
		y = v.Y
	}
	return x, y
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
	pet.AddSeparator()
	pet.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.Quit() })

	m.Append(menu.EditMenu())
	return m
}
