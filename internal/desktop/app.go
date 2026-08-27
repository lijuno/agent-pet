package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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

	// hidden mirrors the window's visibility, so the Hide checkbox can
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

	a.resizeTo(scale)
	a.push()
}

// resizeTo resizes the window for a scale and keeps the character's feet where
// they were. Split out of SetScale so reloading the config can use it: SetScale
// also writes the config back, which is right when a menu changed the scale and
// wrong when the file is where the scale came from.
func (a *App) resizeTo(scale float64) {
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
	case "about", "about-close", "bug", "bug-close":
		// Not panels: About and Report a Bug are native windows, not overlays
		// in the webview. Handled here anyway because OpenPanel is the only
		// door the loopback API has into the window, and a test needs to open
		// and close them the same way it does everything else it cannot click.
		switch kind {
		case "about":
			a.ShowAbout()
		case "about-close":
			closeAbout()
		case "bug":
			a.ShowBugReport()
		case "bug-close":
			closeBugReport()
		}
		return nil
	case "status", "pets", "menu", "close":
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
	out["bug_report"] = bugReportReport()
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

// ReloadConfig re-reads config.yaml and applies what can be applied without a
// restart. It is on the menu because the alternative is quitting the pet to
// change a setting in a file the pet rewrites when it quits — which is a
// circle somebody hits the first time they edit it by hand.
//
// What it cannot do is move the listener. server.addr is bound once at
// startup; a new one takes a restart, and saying so is better than appearing
// to accept it.
func (a *App) ReloadConfig() string {
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		// Load returns a usable config even when it could not parse the file.
		// Applying it anyway would quietly replace the user's settings with
		// defaults, which is the opposite of what they asked for.
		a.log.Warn("reload", "err", err)
		return "could not read the config: " + err.Error()
	}

	old := a.eng.Config()
	if lib := a.eng.Library(); lib.Hide(cfg.Pet.Disabled, cfg.Pet.Active) {
		a.log.Warn("pet.disabled would hide every character; ignoring it",
			"disabled", cfg.Pet.Disabled)
	}
	a.eng.SetConfig(cfg)
	if p, ok := a.eng.ActivePet(); !ok || p.ID != cfg.Pet.Active {
		a.eng.SetPet(cfg.Pet.Active)
	}
	a.refreshPetMenu()

	if cfg.Pet.AlwaysOnTop != a.alwaysOnTop {
		a.SetAlwaysOnTop(cfg.Pet.AlwaysOnTop)
		setOnTopCheck(cfg.Pet.AlwaysOnTop)
	}
	// SetScale saves the config back, which is right when a menu changes the
	// scale and wrong here: the file is what we just read. Resize by hand.
	if cfg.Pet.Scale != old.Pet.Scale && cfg.Pet.Scale > 0 {
		a.resizeTo(cfg.Pet.Scale)
	}
	a.push()

	if cfg.Server.Addr != old.Server.Addr {
		return "reloaded — but server.addr needs a restart to take effect"
	}
	return "reloaded " + a.cfgPath
}

// Interact is the double-click reaction (§14).
func (a *App) Interact() {
	a.eng.Submit(events.Event{Source: "ui", Event: events.PetInteraction, SessionID: "ui"})
}

func (a *App) SetAlwaysOnTop(b bool) {
	a.alwaysOnTop = b
	wruntime.WindowSetAlwaysOnTop(a.ctx, b)
}

// SetUpdate records what a check found and retitles the menu-bar item. Called
// by the server when petctl posts a result; there is no other way for the app
// to learn one.
func (a *App) SetUpdate(st update.Status) {
	a.updMu.Lock()
	a.upd = st
	a.updMu.Unlock()
	setUpdateItem(st)
	a.saveUpdate(st)
}

// saveUpdate remembers what the last check found, so a restart does not forget
// it. Failure is logged and otherwise ignored: not being able to write a
// convenience file is not a reason to stop being a pet.
func (a *App) saveUpdate(st update.Status) {
	// Only a result that names a version is worth carrying across a restart.
	// Without a Latest this says "we looked and the channel had nothing", which
	// is the least useful thing to remember and the most likely to be stale by
	// the next launch — and it is what a bare {"available":false} produces,
	// which is the fixture the desktop suite posts. Remembering that turned a
	// test artefact into durable state that survived restarts.
	//
	// The file is left alone rather than removed: what it holds is the last
	// time we learned what the channel had, and that is still the best answer
	// available.
	if st.Latest == "" {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	path := config.UpdateResult()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.log.Warn("could not save the update result", "err", err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		a.log.Warn("could not save the update result", "err", err)
	}
}

// LoadUpdate reads back what was saved, re-deriving everything that depends on
// which build is running. It has no side effects: main hands the result to the
// server, which is the one door into "petd learned a result".
//
// Current comes from this binary and never from the file. If the app was
// updated while it was closed — which is exactly when nothing could be told —
// the stored Current names the version that got replaced, and Available was
// computed against it. Both are recomputed here, so a result saved before an
// update reads correctly after one: latest 0.2.0 saved while running 0.1.0
// becomes "up to date" once 0.2.0 is the thing running.
func LoadUpdate() (update.Status, bool) {
	b, err := os.ReadFile(config.UpdateResult())
	if err != nil {
		return update.Status{}, false
	}
	var st update.Status
	if err := json.Unmarshal(b, &st); err != nil {
		return update.Status{}, false
	}
	if st.Latest == "" {
		// Same rule on the way in, which also heals a file written before that
		// rule existed.
		return update.Status{}, false
	}
	st.Current = Version
	st.Available = st.Latest != "" && update.Compare(st.Current, st.Latest) < 0
	if st.Validate() != nil {
		// A file this build cannot make sense of is not worth guessing at.
		return update.Status{}, false
	}
	return st, true
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

// updateItemTitle is what the menu bar's update item says, and whether it is
// there at all. Kept out of the cgo file so it can be tested without a menu bar
// to look at.
//
// It appears only when there is something to install. It used to be permanent,
// reporting the last check — "Up to date", "Nothing published yet" — on the
// reasoning that the menu should be able to answer "have I got the latest?".
// It can still be asked: the Pet Status panel says what the last check found
// and how long ago it ran, which is the better answer anyway. What the menu
// carried every day was a line that mattered on one of them.
//
// It reports the last result; it does not check. The daemon opens no outbound
// connections and spawns no processes (ADR 0008), so checking happens in
// `petctl` — once a day from the Claude Code hook, or on demand from a shell.
//
// No relative time in the title. Nothing rebuilds this menu while it sits in
// the menu bar, so "checked 2 minutes ago" would still say that four hours
// later, and a label that goes quietly wrong is worse than no label.
func updateItemTitle(st update.Status) (string, bool) {
	if st.Available && st.Latest != "" {
		return "Update to " + st.Latest + "…", true
	}
	// Every other state — no check yet, nothing published, up to date, ahead of
	// the channel, and a check that failed — is a question rather than a thing
	// to do, and questions are answered in the panel.
	return "", false
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
// It is about the application, not about the pet. Two of these run side by
// side with separate settings, ports and update channels (ADR 0008), and "which
// one am I looking at" is the question this window exists to answer.
//
// The active character is deliberately not here. It is a preference the user
// changes from the menu whenever they like, so it belongs with the other
// per-session facts in the status panel; in a window called About it reads as
// though the character were part of what this build *is*.
func aboutText(in Info) (string, string) {
	var b strings.Builder
	b.WriteString("A desktop pet that reacts to what a coding agent is doing.\n\n")
	for _, r := range [][2]string{
		{"Version", in.Version},
		{"Channel", in.Channel},
		{"Event API", in.Addr},
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
func (a *App) ShowAbout() { showAbout(aboutText(a.GetInfo())) }

// CloseAbout hides it again.
func (a *App) CloseAbout() { closeAbout() }

// bugReportText is what the Report a Bug window says: what to do, and then the
// details to do it with.
//
// The window exists because the menu is the only part of this program somebody
// with a bug will think to look at, and "where do I report this?" had no answer
// anywhere in it. It answers with steps rather than a link alone: an issue
// saying "the pet froze" costs a round trip that the version and the log path
// would have saved.
//
// Kept out of the cgo file, and split from the details, so both can be tested
// without a screen — and so the button that copies the details cannot drift
// from the details on show.
func bugReportText(in Info, osVer, logPath string) (string, string) {
	var b strings.Builder
	b.WriteString("Something not working? Please open an issue.\n\n")
	b.WriteString("1. Report on GitHub opens a new issue with the details\n")
	b.WriteString("   below and your config already in it.\n")
	b.WriteString("2. Save Report writes those and the last 200 log lines to\n")
	b.WriteString("   a file and shows it in Finder. Drag it onto the issue —\n")
	b.WriteString("   a link cannot attach a file, only fill the form.\n")
	b.WriteString("3. Say what the pet did and what you expected, then post.\n\n")
	b.WriteString("Copy Report puts the same text on the clipboard.\n\n")
	b.WriteString(bugReportDetails(in, osVer, logPath))
	name := in.AppName
	if name == "" {
		name = "Agent Pet"
	}
	return "File a Bug — " + name, b.String()
}

// bugReportDetails is the block the Copy Details button puts on the clipboard,
// and the tail of what the window shows. Everything here is a fact about the
// build or about where its files are; nothing an agent reported is in it, so
// there is nothing to sanitise (§26).
func bugReportDetails(in Info, osVer, logPath string) string {
	var b strings.Builder
	for _, r := range [][2]string{
		{"Version", in.Version},
		{"Channel", in.Channel},
		{"macOS", osVer},
		{"Event API", in.Addr},
		{"Config", in.ConfigPath},
		{"Log", logPath},
		{"Issues", update.IssuesURL},
	} {
		v := r[1]
		if v == "" {
			// An empty column is indistinguishable from a broken one.
			v = "—"
		}
		fmt.Fprintf(&b, "%-10s %s\n", r[0], v)
	}
	return strings.TrimRight(b.String(), "\n")
}

// What a report carries of the two files beside it. The config is quoted whole
// because it is a few hundred bytes of settings and the whole of it is the
// answer to "what was it set to"; the log is quoted by the tail, because the
// end of a log is the part that was happening when whatever went wrong did.
const (
	maxReportConfig = 4 << 10
	reportLogLines  = 200
)

// maxIssueURL is where a prefilled issue stops being a URL. GitHub refuses an
// over-long request outright, and a button that lands on an error page is
// worse than one that opens a form with a little less in it.
const maxIssueURL = 6000

// readConfigSection is config.yaml quoted into a report, or a line saying why
// it is not.
//
// Silence would be the wrong answer to any of these. A config that cannot be
// read is itself a fact about a broken install, and a report that simply left
// it out looks like one written by somebody who could not be bothered.
func readConfigSection(path string) string {
	if path == "" {
		return "(no config path)"
	}
	b, err := os.ReadFile(path)
	switch {
	case err != nil:
		return "(could not read " + path + ": " + err.Error() + ")"
	case len(b) > maxReportConfig:
		return fmt.Sprintf("(%s is %d bytes — too big to quote here)", path, len(b))
	case strings.TrimSpace(string(b)) == "":
		return "(empty)"
	}
	return strings.TrimRight(string(b), "\n")
}

// readLogSection is the last lines of the log, on the same terms.
func readLogSection(path string, lines int) string {
	if path == "" {
		return "(no log path)"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "(could not read " + path + ": " + err.Error() + ")"
	}
	text := strings.TrimRight(string(b), "\n")
	if strings.TrimSpace(text) == "" {
		return "(empty)"
	}
	all := strings.Split(text, "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

// bugReportBundle is the whole report as plain text: the details, the config
// and the tail of the log. Copy Report puts it on the clipboard and Save
// Report writes it to a file — those differ in where it goes, not in what it
// says, which is why there is one of these and not two.
func bugReportBundle(in Info, osVer, logPath string) string {
	var b strings.Builder
	b.WriteString(bugReportDetails(in, osVer, logPath))
	b.WriteString("\n\n--- config.yaml ---\n")
	b.WriteString(readConfigSection(in.ConfigPath))
	fmt.Fprintf(&b, "\n\n--- petd.log (last %d lines) ---\n", reportLogLines)
	b.WriteString(readLogSection(logPath, reportLogLines))
	return b.String() + "\n"
}

// saveBugReport writes that bundle beside the pet's own files and hands back
// the path, for dragging onto an issue. A URL can prefill an issue and nothing
// more: attaching a file is a drag into the editor, so the pet's part is to
// put one somewhere it can be dragged from.
//
// Not the Desktop or Downloads, which macOS gates behind a permission prompt.
// A prompt in the middle of reporting a bug is one more thing to go wrong, and
// the data directory is somewhere this program already writes.
func saveBugReport(in Info, osVer, logPath string) (string, error) {
	dir := config.DataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// One file, overwritten. It exists to be dragged into an issue and then
	// forgotten; a directory filling with timestamped copies of it is litter.
	path := filepath.Join(dir, "bug-report.txt")
	if err := os.WriteFile(path, []byte(bugReportBundle(in, osVer, logPath)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// issueBody is the markdown a prefilled issue opens with. cfg empty means the
// config was left out — said in the body rather than silently, so nobody
// wonders why a file the window promised is missing.
func issueBody(in Info, osVer, logPath, cfg string) string {
	var b strings.Builder
	b.WriteString("**What happened**\n\n\n")
	b.WriteString("**What I expected**\n\n\n")
	b.WriteString("**Details**\n\n```\n")
	b.WriteString(bugReportDetails(in, osVer, logPath))
	b.WriteString("\n```\n\n")
	b.WriteString("**config.yaml**\n\n")
	if cfg == "" {
		b.WriteString("(too long for this form — it is in the saved report)\n\n")
	} else {
		b.WriteString("```yaml\n" + cfg + "\n```\n\n")
	}
	// The log is named, not quoted. It is a megabyte before it rotates and a
	// URL holds a few thousand characters, so the honest thing is to say where
	// the copy to attach comes from.
	b.WriteString("**Log**\n\n")
	b.WriteString("Not in here — Save Report in the pet's File a Bug window\n")
	b.WriteString("writes the details, the config and the last log lines to a\n")
	b.WriteString("file, and shows it in Finder. Drag it in to attach it.\n")
	return b.String()
}

func issueURL(body string) string {
	return update.IssuesURL + "/new?" + url.Values{"body": {body}}.Encode()
}

// bugReportIssueURL is the new-issue form with the report already in it, which
// is what the Report on GitHub button opens.
//
// GitHub fills the form and nothing more: the person signs in, reads what is
// about to be sent and presses the button, which is the only way the pet is
// ever party to publishing anything.
//
// None of this is agent-controlled. The version and the channel are build
// constants, the OS string comes from the system, and the paths and the config
// come from the user's own flags and file, so §26 — which is about what an
// agent reports becoming markup, a path or a command — has nothing to bite on.
// What url.Values.Encode does is the separate, ordinary safety: every value is
// percent-encoded, so nothing in a path or a config can end the query and
// start something else.
//
// The config comes out again if the URL grows past what GitHub will take. It
// is the one part of this that varies in size, and losing it is a smaller
// failure than an error page.
//
// Validated before it is returned, and the plain tracker is what comes back if
// it does not hold up. A URL this function got wrong would be one openURL
// silently refused, and a button that does nothing is worse than one that does
// less.
func bugReportIssueURL(in Info, osVer, logPath string) string {
	raw := issueURL(issueBody(in, osVer, logPath, readConfigSection(in.ConfigPath)))
	if len(raw) > maxIssueURL {
		raw = issueURL(issueBody(in, osVer, logPath, ""))
	}
	if err := update.ValidateNotesURL(raw); err != nil {
		return update.IssuesURL
	}
	return raw
}

// ShowBugReport opens the Report a Bug window. Bound to the frontend as well
// as reached from the status item, so the pet's own menu and the menu bar open
// the same one window.
func (a *App) ShowBugReport() {
	showBugReport(bugReportText(a.GetInfo(), osVersion(), config.LogFile()))
}

// CloseBugReport hides it again.
func (a *App) CloseBugReport() { closeBugReport() }

// OpenBugReportIssue backs the Report on GitHub button. Rebuilt at the press
// for the same reason the clipboard block is: Reload Config can move the paths
// under a running process.
func (a *App) OpenBugReportIssue() {
	openURL(bugReportIssueURL(a.GetInfo(), osVersion(), config.LogFile()))
}

// CopyBugReport backs the Copy Report button: the same text Save Report
// writes, for an issue written somewhere else — or for somebody signed out of
// GitHub, who has no form to prefill at all.
//
// Rebuilt at the press rather than remembered from the window: Reload Config
// can move the paths under a running process, and the log grows while the
// window sits open.
func (a *App) CopyBugReport() {
	copyToClipboard(bugReportBundle(a.GetInfo(), osVersion(), config.LogFile()))
	flashReportButton(reportCopyButton, "Copied")
}

// SaveBugReport writes the report to a file and shows it in Finder, which is
// the only way a file reaches a GitHub issue: attaching is a drag into the
// editor, and no URL can do it.
func (a *App) SaveBugReport() {
	path, err := saveBugReport(a.GetInfo(), osVersion(), config.LogFile())
	if err != nil {
		// The button says so, because the window is what the user is looking
		// at. The log gets the reason, which is no use on a button.
		a.log.Warn("could not write the bug report", "err", err)
		flashReportButton(reportSaveButton, "Could not save")
		return
	}
	flashReportButton(reportSaveButton, "Saved")
	revealFile(path)
}

func (a *App) Quit() { wruntime.Quit(a.ctx) }

// showWindow and emitPanel exist for the optional status-bar menu, which has to
// reach into the window from outside the webview.
// SetShown puts the pet on screen or takes it away. It backs the Hide checkbox
// in the menu-bar menu, which is the only surface that can offer it: a hidden
// pet cannot be clicked to bring itself back.
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
	a.syncHideCheck(a.hidden)
}

// Shown reports whether the pet is on screen.
func (a *App) Shown() bool { return !a.hidden }

// showWindow is what unticking Hide does. Three things have to be true for
// that to mean
// anything, and only the first was being done:
//
//   - the window is shown and un-minimised;
//   - the application is frontmost, or the window is shown behind whatever the
//     user is actually looking at;
//   - the window is somewhere on the screen. The pet can be dragged almost
//     entirely off an edge, and Hide is exactly what someone reaches for
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
	pet.AddText("Status", keys.CmdOrCtrl("i"), func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "pet:panel", "status")
	})
	pet.AddSeparator()
	pet.AddText("Change Character…", nil, func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "pet:panel", "pets")
	})
	pet.AddSeparator()

	top := pet.AddCheckbox("Always on Top", a.alwaysOnTop, nil, nil)
	top.OnClick(func(cb *menu.CallbackData) {
		a.SetAlwaysOnTop(cb.MenuItem.Checked)
	})
	pet.AddSeparator()
	pet.AddSeparator()
	pet.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.Quit() })

	m.Append(menu.EditMenu())
	return m
}
