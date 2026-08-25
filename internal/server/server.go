// Package server exposes the local event API (§25) on a loopback listener.
//
// Security posture (§26): loopback-only bind, bounded request bodies, strict
// field decoding, sanitised metadata, and no path in this package that turns
// request content into a command, a file path, or HTML.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/engine"
	"github.com/lijuno/agent-pet/internal/events"
	"github.com/lijuno/agent-pet/internal/state"
	"github.com/lijuno/agent-pet/internal/update"
)

// MaxBody caps request bodies. Large enough for any legitimate hook payload,
// small enough that a runaway producer cannot grow the process.
const MaxBody = 16 << 10

// Version is stamped by main.
var Version = "dev"

type Server struct {
	eng  *engine.Engine
	log  *slog.Logger
	mux  *http.ServeMux
	http *http.Server
	ln   net.Listener
	// Desktop reports window and menu-bar facts that only the Wails layer can
	// see. It is a function rather than a field so this package stays ignorant
	// of the desktop shell, and nil in a headless test.
	Desktop func() map[string]string
	// Panel opens one of the pet's overlays. It exists so the window can be
	// driven from a test — proving a menu is not clipped in a corner needs the
	// menu actually opened in that corner, and a mouse is not available to a
	// test. Nil in a headless run.
	Panel func(kind string) error
	// MoveWindow parks the window, for the same reason.
	MoveWindow func(x, y int) error
	// StatusItem performs a menu-bar menu item, and StatusMenu reports what
	// that menu says. Nothing else can see a menu bar.
	StatusItem func(name string) error
	StatusMenu func() string
	// SetShown hides or shows the pet, the same toggle the menu bar offers.
	SetShown func(bool)
	// OnUpdate is called when a check reports a result, so the menu bar can
	// say so. Nil in a headless run.
	OnUpdate func(update.Status)
	// upd holds what the last check found. See update.go.
	upd updateState
	// startedAt supports the uptime field in /healthz and `petctl doctor`.
	startedAt time.Time
}

func New(eng *engine.Engine, log *slog.Logger) *Server {
	s := &Server{eng: eng, log: log, mux: http.NewServeMux(), startedAt: time.Now()}
	s.routes()
	s.http = &http.Server{
		Handler:           s.withGuards(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// No WriteTimeout: /stream is a long-lived SSE response.
		IdleTimeout: 60 * time.Second,
	}
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/event", s.handleEvent)
	s.mux.HandleFunc("/state", s.handleState)
	s.mux.HandleFunc("/stream", s.handleStream)
	s.mux.HandleFunc("/test", s.handleTest)
	s.mux.HandleFunc("/interact", s.handleInteract)
	s.mux.HandleFunc("/pets", s.handlePets)
	s.mux.HandleFunc("/pet", s.handleSetPet)
	s.mux.HandleFunc("/diagnostics", s.handleDiagnostics)
	s.mux.HandleFunc("/window", s.handleWindow)
	s.mux.HandleFunc("/update", s.handleUpdate)
}

// Listen binds the address. It refuses non-loopback addresses unless the config
// explicitly allows it, so a careless edit cannot expose the pet to the LAN.
func (s *Server) Listen(cfg config.Server) error {
	if !config.IsLoopback(cfg.Addr) && !cfg.AllowNonLoopback {
		return fmt.Errorf("refusing to bind %q: not a loopback address; set server.allow_non_loopback if you really mean it", cfg.Addr)
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	s.ln = ln
	return nil
}

func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Serve blocks until the server is closed.
func (s *Server) Serve() error {
	if s.ln == nil {
		return errors.New("Listen must be called before Serve")
	}
	err := s.http.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

// withGuards applies the transport-level protections to every route.
func (s *Server) withGuards(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject requests that did not arrive over loopback even if the
		// listener somehow ended up bound more widely.
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
				http.Error(w, "loopback only", http.StatusForbidden)
				return
			}
		}
		// A browser on some other page must not be able to poke the pet.
		if o := r.Header.Get("Origin"); o != "" && !isLocalOrigin(o) {
			http.Error(w, "bad origin", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// isLocalOrigin reports whether an Origin header names this machine.
//
// The host is compared exactly rather than by prefix. `http://localhost` is a
// prefix of `http://localhost.evil.com`, a name anyone may register and point
// at 127.0.0.1; a page served from it would otherwise have passed this guard
// and been able to read /diagnostics and drive /window.
func isLocalOrigin(o string) bool {
	u, err := url.Parse(strings.ToLower(o))
	if err != nil {
		return false
	}
	// The Wails frontend is served from its own scheme, whose host is an
	// internal detail of the webview rather than a network name.
	if u.Scheme == "wails" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}

// decode reads a strict JSON body: unknown fields are an error, so a typo in a
// hook script is reported instead of silently ignored.
func decode(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("content-type must be application/json")
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("empty body")
		}
		return err
	}
	return nil
}

func methodIs(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		w.Header().Set("Allow", method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

// ---- handlers ----

type healthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
	PID     int    `json:"pid"`
	// Exe is which binary is answering. An updater that has just swapped a
	// bundle needs to know it is talking to the app it installed and not to
	// another copy that already held the port — and the version alone cannot
	// tell it, because two copies of the same version answer identically.
	Exe string `json:"exe,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// PID was declared and never set, so it answered 0 to everyone who asked.
	exe, _ := os.Executable()
	writeJSON(w, http.StatusOK, healthResponse{
		OK:      true,
		Version: Version,
		Uptime:  time.Since(s.startedAt).Round(time.Second).String(),
		PID:     os.Getpid(),
		Exe:     exe,
	})
}

// eventRequest mirrors events.Event but keeps metadata as raw JSON values so a
// hook can send numbers or booleans without a type error. Everything is
// stringified and sanitised before it reaches the state machine.
type eventRequest struct {
	Source    string         `json:"source"`
	Event     string         `json:"event"`
	SessionID string         `json:"session_id"`
	Timestamp string         `json:"timestamp"`
	Metadata  map[string]any `json:"metadata"`
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodPost) {
		return
	}
	var req eventRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.Event == "" {
		badRequest(w, "event is required")
		return
	}
	ev := events.Event{
		Source:    req.Source,
		Event:     events.Kind(req.Event),
		SessionID: req.SessionID,
		Metadata:  stringifyMeta(req.Metadata),
	}
	if req.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
			ev.Timestamp = t
		}
	}
	snap := s.eng.Submit(ev)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"accepted": true,
		"known":    ev.Event.Known(),
		"state":    snap.State,
	})
}

// stringifyMeta flattens arbitrary JSON scalars to strings and drops anything
// structured. Nested objects have no meaning to the pet and are a needless
// attack surface.
func stringifyMeta(m map[string]any) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case bool:
			out[k] = fmt.Sprintf("%t", t)
		case float64:
			out[k] = strings.TrimSuffix(fmt.Sprintf("%.6f", t), ".000000")
		case nil:
			out[k] = ""
		default:
			// objects and arrays are dropped
		}
	}
	return out
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodGet) {
		return
	}
	up := s.eng.Last()
	up.Snapshot = s.eng.Snapshot()
	writeJSON(w, http.StatusOK, up)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodGet) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch, cancel := s.eng.Subscribe()
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case up, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(up)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: state\ndata: %s\n\n", b)
			flusher.Flush()
		}
	}
}

type testRequest struct {
	State    string `json:"state"`
	Duration string `json:"duration"`
	Clear    bool   `json:"clear"`
}

// handleTest backs `petctl test <state>` (§31): it pins a state so animations
// can be reviewed without running an agent.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodPost) {
		return
	}
	var req testRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.Clear {
		s.eng.ClearForce()
		writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
		return
	}
	st := state.State(req.State)
	if !state.Valid(st) {
		badRequest(w, fmt.Sprintf("unknown state %q; valid: %s", req.State, joinStates(state.All())))
		return
	}
	d := 12 * time.Second
	if req.Duration != "" {
		parsed, err := time.ParseDuration(req.Duration)
		if err != nil {
			badRequest(w, "invalid duration: "+err.Error())
			return
		}
		if parsed <= 0 || parsed > time.Hour {
			badRequest(w, "duration must be between 0 and 1h")
			return
		}
		d = parsed
	}
	snap := s.eng.Force(st, d)
	writeJSON(w, http.StatusOK, map[string]any{"state": snap.State, "for": d.String()})
}

func (s *Server) handleInteract(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodPost) {
		return
	}
	snap := s.eng.Submit(events.Event{Source: "ui", Event: events.PetInteraction, SessionID: "ui"})
	writeJSON(w, http.StatusOK, map[string]any{"state": snap.State})
}

func (s *Server) handlePets(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodGet) {
		return
	}
	active := ""
	if p, ok := s.eng.ActivePet(); ok {
		active = p.ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active": active,
		"pets":   s.eng.Library().List(),
	})
}

type setPetRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleSetPet(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodPost) {
		return
	}
	var req setPetRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	p, ok := s.eng.SetPet(req.ID)
	if !ok {
		badRequest(w, "unknown pet "+req.ID)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

type windowRequest struct {
	Panel      string `json:"panel"`
	X          *int   `json:"x"`
	Y          *int   `json:"y"`
	StatusItem string `json:"status_item"`
	Shown      *bool  `json:"shown"`
}

// handleWindow drives the window from outside the process, so the placement of
// a menu in the corner of a screen can be checked by something other than a
// person with a mouse. It moves nothing an ordinary user cannot move by
// dragging, and opens nothing they cannot open by clicking.
func (s *Server) handleWindow(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodPost) {
		return
	}
	var req windowRequest
	if err := decode(r, &req); err != nil {
		badRequest(w, err.Error())
		return
	}
	if req.X != nil && req.Y != nil {
		if s.MoveWindow == nil {
			badRequest(w, "no window to move: petd is running headless")
			return
		}
		if err := s.MoveWindow(*req.X, *req.Y); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if req.Panel != "" {
		if s.Panel == nil {
			badRequest(w, "no window to open a panel in: petd is running headless")
			return
		}
		if err := s.Panel(req.Panel); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	if req.Shown != nil {
		if s.SetShown == nil {
			badRequest(w, "no window: petd is running headless")
			return
		}
		s.SetShown(*req.Shown)
	}
	if req.StatusItem != "" {
		if s.StatusItem == nil {
			badRequest(w, "no status item: petd is running headless")
			return
		}
		if err := s.StatusItem(req.StatusItem); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Diagnostics is the payload behind `petctl doctor` (§30).
type Diagnostics struct {
	Version           string            `json:"version"`
	Uptime            string            `json:"uptime"`
	Addr              string            `json:"addr"`
	ConfigPath        string            `json:"config_path"`
	DataDir           string            `json:"data_dir"`
	DataWrite         string            `json:"data_writable"`
	ActivePet         string            `json:"active_pet"`
	Animations        int               `json:"animations"`
	MissingAnimations []string          `json:"missing_animations,omitempty"`
	Pets              []string          `json:"pets"`
	Sessions          int               `json:"sessions"`
	EventsSeen        int               `json:"events_seen"`
	State             string            `json:"state"`
	Integrations      map[string]string `json:"integrations"`
	// Desktop is window geometry and menu-bar status: things with no other way
	// of being checked, since nothing in a test can look at a screen.
	Desktop map[string]string `json:"desktop,omitempty"`
	// Update is what the last update check found, if one has run.
	Update update.Status `json:"update"`
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !methodIs(w, r, http.MethodGet) {
		return
	}
	snap := s.eng.Snapshot()
	cfg := s.eng.Config()

	d := Diagnostics{
		Version:      Version,
		Uptime:       time.Since(s.startedAt).Round(time.Second).String(),
		Addr:         s.Addr(),
		ConfigPath:   config.Path(),
		DataDir:      config.DataDir(),
		DataWrite:    writableStatus(config.DataDir()),
		Sessions:     len(snap.Sessions),
		EventsSeen:   snap.Stats.EventsSeen,
		State:        string(snap.State),
		Integrations: map[string]string{},
	}
	if s.Desktop != nil {
		d.Desktop = s.Desktop()
	}
	d.Update = s.updateStatus()
	for _, p := range s.eng.Library().List() {
		d.Pets = append(d.Pets, p.ID)
	}
	if p, ok := s.eng.ActivePet(); ok {
		d.ActivePet = p.ID
		d.Animations = len(p.Animations)
		for _, m := range p.Missing() {
			d.MissingAnimations = append(d.MissingAnimations, string(m))
		}
	}
	// The engine knows nothing about Claude Code or Codex, so it can only
	// report what it has actually received. Whether an adapter is installed is
	// a question about somebody else's configuration file, which petctl answers
	// in `doctor` — this package does not go looking in ~/.claude (§29: never
	// claim a state that cannot be observed from here).
	for name, tog := range cfg.Integrations {
		switch {
		case !tog.Enabled:
			d.Integrations[name] = "disabled"
		case sourceSeen(snap, name):
			d.Integrations[name] = "events received"
		default:
			d.Integrations[name] = "enabled, no events yet"
		}
	}
	writeJSON(w, http.StatusOK, d)
}

func sourceSeen(snap state.Snapshot, source string) bool {
	for _, s := range snap.Sessions {
		if s.Key.Source == source {
			return true
		}
	}
	return false
}

func writableStatus(dir string) string {
	if err := ensureWritable(dir); err != nil {
		return "no: " + err.Error()
	}
	return "yes"
}

func joinStates(ss []state.State) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
