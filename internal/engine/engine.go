// Package engine wires the event intake, the state machine, the speech-bubble
// speaker and the subscribers together. It is the only package that owns
// goroutines, and it knows nothing about HTTP or about Wails.
package engine

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lijuno/agent-pet/internal/bubble"
	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/events"
	"github.com/lijuno/agent-pet/internal/petassets"
	"github.com/lijuno/agent-pet/internal/state"
)

// Update is what subscribers receive. It is a complete picture, not a delta, so
// a subscriber that misses one can simply take the next.
type Update struct {
	Snapshot state.Snapshot  `json:"snapshot"`
	Bubble   *bubble.Message `json:"bubble,omitempty"`
	Pet      string          `json:"pet"`
	Changed  bool            `json:"changed"`
	At       time.Time       `json:"at"`
}

// Sink receives every accepted event. Milestone 4's SQLite store attaches here;
// nothing else in the engine needs to change to add persistence.
type Sink interface {
	Record(ev events.Event, snap state.Snapshot)
}

type Engine struct {
	mu        sync.Mutex
	cfg       config.Config
	machine   *state.Machine
	speaker   bubble.Speaker
	lib       *petassets.Library
	activePet string

	log   *slog.Logger
	now   func() time.Time
	sinks []Sink

	subs   map[int]chan Update
	nextID int

	// last is retained so a late subscriber gets the current picture immediately.
	last Update
}

type Option func(*Engine)

// WithClock replaces the clock. Tests use it to drive the machine
// deterministically; nothing else should.
func WithClock(f func() time.Time) Option { return func(e *Engine) { e.now = f } }

func WithSink(s Sink) Option { return func(e *Engine) { e.sinks = append(e.sinks, s) } }

func New(cfg config.Config, lib *petassets.Library, log *slog.Logger, opts ...Option) *Engine {
	if log == nil {
		log = slog.Default()
	}
	e := &Engine{
		cfg:     cfg,
		machine: state.New(machineOptions(cfg)),
		lib:     lib,
		log:     log,
		now:     time.Now,
		subs:    map[int]chan Update{},
	}
	for _, o := range opts {
		o(e)
	}
	e.speaker = bubble.NewTemplater(
		cfg.Personality.Preset,
		cfg.Personality.Name,
		cfg.Behavior.Dialogue,
		e.now().UnixNano(),
	)
	if p, ok := lib.Any(cfg.Pet.Active); ok {
		e.activePet = p.ID
	}
	e.last = Update{Snapshot: e.machine.Snapshot(e.now()), Pet: e.activePet, At: e.now()}
	return e
}

func machineOptions(cfg config.Config) state.Options {
	t := cfg.Thresholds
	return state.Options{
		IdleAfter:        t.IdleAfter.D(),
		ToolPatience:     t.ToolPatience.D(),
		SleepingAfter:    t.SleepingAfter.D(),
		AttentionTimeout: t.AttentionTimeout.D(),
		SessionStale:     t.SessionStale.D(),
		HappyFor:         t.HappyFor.D(),
		CelebrateFor:     t.CelebrateFor.D(),
		ConfusedFor:      t.ConfusedFor.D(),
		HeartFor:         t.HeartFor.D(),
		WorriedAfter:     t.WorriedAfter,
	}
}

// Submit accepts an event from any goroutine. It normalises, applies and
// publishes synchronously: the whole operation is a few map writes, so there is
// no queue to overflow and no ordering ambiguity between two adapters.
func (e *Engine) Submit(ev events.Event) state.Snapshot {
	e.mu.Lock()
	now := e.now()
	norm := ev.Normalize(now)
	prev := e.machine.Current()
	e.machine.Apply(now, norm)
	snap, changed := e.machine.Advance(now)

	e.logEvent(norm, snap)

	var msg *bubble.Message
	if m, ok := e.speaker.Say(now, prev, snap.State, &norm, e.longestSession(snap)); ok {
		msg = &m
	}
	up := Update{Snapshot: snap, Bubble: msg, Pet: e.activePet, Changed: changed, At: now}
	e.last = up
	sinks := append([]Sink(nil), e.sinks...)
	e.publishLocked(up)
	e.mu.Unlock()

	for _, s := range sinks {
		s.Record(norm, snap)
	}
	return snap
}

// logEvent writes one concise line per event. §32: no prompts, no source code,
// no command arguments unless logging.verbose is explicitly enabled.
func (e *Engine) logEvent(ev events.Event, snap state.Snapshot) {
	attrs := []any{"source", ev.Source, "event", string(ev.Event), "state", string(snap.State)}
	if e.cfg.Logging.Verbose && len(ev.Metadata) > 0 {
		for k, v := range ev.Metadata {
			attrs = append(attrs, "meta."+k, v)
		}
	}
	e.log.Info("event", attrs...)
}

func (e *Engine) longestSession(snap state.Snapshot) time.Duration {
	var d time.Duration
	for _, s := range snap.Sessions {
		if s.Duration > d {
			d = s.Duration
		}
	}
	return d
}

// Tick advances time-based transitions. Called once a second by Run.
func (e *Engine) Tick() {
	e.mu.Lock()
	now := e.now()
	prev := e.machine.Current()
	snap, changed := e.machine.Advance(now)
	var msg *bubble.Message
	if changed {
		if m, ok := e.speaker.Say(now, prev, snap.State, nil, e.longestSession(snap)); ok {
			msg = &m
		}
	}
	up := Update{Snapshot: snap, Bubble: msg, Pet: e.activePet, Changed: changed, At: now}
	e.last = up
	if changed || msg != nil {
		e.publishLocked(up)
	}
	e.mu.Unlock()
}

// Run drives the clock until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Tick()
		}
	}
}

// Force pins a state for `petctl test`.
func (e *Engine) Force(s state.State, d time.Duration) state.Snapshot {
	e.mu.Lock()
	now := e.now()
	e.machine.Force(now, s, d)
	snap, changed := e.machine.Advance(now)
	up := Update{Snapshot: snap, Pet: e.activePet, Changed: changed, At: now}
	e.last = up
	e.publishLocked(up)
	e.mu.Unlock()
	return snap
}

func (e *Engine) ClearForce() {
	e.mu.Lock()
	e.machine.ClearForce()
	snap, changed := e.machine.Advance(e.now())
	up := Update{Snapshot: snap, Pet: e.activePet, Changed: changed, At: e.now()}
	e.last = up
	e.publishLocked(up)
	e.mu.Unlock()
}

func (e *Engine) Snapshot() state.Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.machine.Snapshot(e.now())
}

func (e *Engine) Last() Update {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last
}

func (e *Engine) Config() config.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// SetConfig applies a new config at runtime (threshold and personality changes).
func (e *Engine) SetConfig(cfg config.Config) {
	e.mu.Lock()
	e.cfg = cfg
	e.machine.SetOptions(machineOptions(cfg))
	e.speaker = bubble.NewTemplater(cfg.Personality.Preset, cfg.Personality.Name, cfg.Behavior.Dialogue, e.now().UnixNano())
	e.mu.Unlock()
}

func (e *Engine) Library() *petassets.Library { return e.lib }

func (e *Engine) ActivePet() (petassets.Pet, bool) {
	e.mu.Lock()
	id := e.activePet
	e.mu.Unlock()
	return e.lib.Get(id)
}

// SetPet switches the active character without restarting (acceptance criterion
// in §36).
func (e *Engine) SetPet(id string) (petassets.Pet, bool) {
	p, ok := e.lib.Get(id)
	if !ok {
		return petassets.Pet{}, false
	}
	e.mu.Lock()
	e.activePet = p.ID
	e.cfg.Pet.Active = p.ID
	snap := e.machine.Snapshot(e.now())
	up := Update{Snapshot: snap, Pet: p.ID, Changed: true, At: e.now()}
	e.last = up
	e.publishLocked(up)
	e.mu.Unlock()
	return p, true
}

// Subscribe returns a channel of updates and a cancel function. The channel is
// buffered and lossy on purpose: a slow subscriber drops intermediate frames
// rather than blocking the engine.
func (e *Engine) Subscribe() (<-chan Update, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := e.nextID
	e.nextID++
	ch := make(chan Update, 8)
	ch <- e.last
	e.subs[id] = ch
	return ch, func() {
		e.mu.Lock()
		if c, ok := e.subs[id]; ok {
			delete(e.subs, id)
			close(c)
		}
		e.mu.Unlock()
	}
}

func (e *Engine) publishLocked(u Update) {
	for _, ch := range e.subs {
		select {
		case ch <- u:
		default:
			// Subscriber is behind. Dropping is correct: the next update is a
			// complete picture anyway.
		}
	}
}
