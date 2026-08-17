package state

import (
	"sort"
	"time"

	"github.com/lijuno/agent-digital-pet/internal/events"
)

// Options are the tunable thresholds from §20 and §21. They are passed in
// rather than read from config so the machine stays free of I/O.
type Options struct {
	// IdleAfter: a session that reported work but has gone quiet drops back to
	// idle. Covers agents that never send an explicit idle event.
	IdleAfter time.Duration
	// SleepingAfter: no events from anyone for this long and the pet sleeps.
	// Short on purpose. A cat dozes the moment nothing is happening, and an
	// agent between turns is nothing happening — the pet is more restful and
	// more honest asleep than staring at a prompt nobody has typed yet. What
	// makes this affordable is that grey, not open eyes, is what says an agent
	// is connected: asleep in colour reads as "Claude is there and quiet",
	// asleep in grey as "Claude is gone".
	SleepingAfter time.Duration
	// AttentionTimeout: how long an unanswered permission request keeps the pet
	// in attention before it gives up. Prevents a stuck pet if the agent dies
	// mid-prompt.
	AttentionTimeout time.Duration
	// SessionStale: a session with no events for this long is forgotten, even
	// without session_ended. Agents crash; sessions must not leak.
	SessionStale time.Duration
	// MaxSessions bounds how many sessions are tracked at once. Every other
	// dimension of untrusted input is already bounded (events.MaxSourceLen and
	// friends); the number of distinct session ids was not, so an adapter that
	// minted a fresh id per event would pin thousands of sessions until
	// SessionStale and put every one of them in each /state and /stream payload.
	// Zero means DefaultMaxSessions, so a caller that builds Options field by
	// field cannot accidentally opt out of the bound.
	MaxSessions int

	HappyFor     time.Duration
	CelebrateFor time.Duration
	ConfusedFor  time.Duration
	HeartFor     time.Duration

	// WorriedAfter: consecutive failures in one session before confused
	// escalates to worried.
	WorriedAfter int
}

// DefaultMaxSessions is a generous ceiling: a person runs a handful of agents
// at once, never dozens. Reaching it means an adapter is misbehaving, and the
// pet should stay responsive rather than accumulate.
const DefaultMaxSessions = 64

func DefaultOptions() Options {
	return Options{
		IdleAfter:        30 * time.Second,
		SleepingAfter:    60 * time.Second,
		AttentionTimeout: 10 * time.Minute,
		SessionStale:     2 * time.Hour,
		MaxSessions:      DefaultMaxSessions,
		HappyFor:         6 * time.Second,
		CelebrateFor:     9 * time.Second,
		ConfusedFor:      8 * time.Second,
		HeartFor:         4 * time.Second,
		WorriedAfter:     3,
	}
}

// Session is one agent session's private automaton.
type Session struct {
	Key         events.SessionKey `json:"key"`
	StartedAt   time.Time         `json:"started_at"`
	LastEventAt time.Time         `json:"last_event_at"`

	// Base is the session's steady state: idle, thinking, working or attention.
	Base State `json:"base"`
	// Transient is a short-lived reaction (happy, celebrate, confused, heart)
	// that decays back to Base at TransientUntil.
	Transient      State     `json:"transient,omitempty"`
	TransientUntil time.Time `json:"transient_until,omitempty"`

	RunningTools   int    `json:"running_tools"`
	Failures       int    `json:"failures"`
	TasksCompleted int    `json:"tasks_completed"`
	LastTool       string `json:"last_tool,omitempty"`
	Ended          bool   `json:"ended"`
}

// Effective is the session's contribution to the visible pet state.
func (s *Session) Effective(now time.Time, o Options) State {
	if s.Transient != "" && now.Before(s.TransientUntil) {
		return s.Transient
	}
	base := s.Base
	if base == Attention && now.Sub(s.LastEventAt) > o.AttentionTimeout {
		base = Idle
	}
	// An agent that stopped reporting is not working any more.
	if (base == Working || base == Thinking) && now.Sub(s.LastEventAt) > o.IdleAfter {
		base = Idle
	}
	if s.Ended {
		return Idle
	}
	return base
}

// Stats are in-memory counters for the status popup. Durable statistics arrive
// with SQLite in Milestone 4.
type Stats struct {
	SessionsStarted int           `json:"sessions_started"`
	TasksCompleted  int           `json:"tasks_completed"`
	TestsPassed     int           `json:"tests_passed"`
	TestsFailed     int           `json:"tests_failed"`
	Errors          int           `json:"errors"`
	Commits         int           `json:"commits"`
	Interactions    int           `json:"interactions"`
	EventsSeen      int           `json:"events_seen"`
	ActiveDuration  time.Duration `json:"active_duration_ns"`
}

// SessionView is a session rendered for the UI and for `petctl status`.
type SessionView struct {
	Key      events.SessionKey `json:"key"`
	State    State             `json:"state"`
	Duration time.Duration     `json:"duration_ns"`
	Idle     time.Duration     `json:"idle_ns"`
	LastTool string            `json:"last_tool,omitempty"`
}

// Snapshot is the complete visible state of the pet at one instant.
type Snapshot struct {
	State     State             `json:"state"`
	Since     time.Time         `json:"since"`
	Reason    events.Kind       `json:"reason,omitempty"`
	Source    string            `json:"source,omitempty"`
	Sessions  []SessionView     `json:"sessions"`
	Stats     Stats             `json:"stats"`
	Forced    bool              `json:"forced"`
	LastEvent *events.Event     `json:"last_event,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// Machine reduces an event stream to a visible pet state.
//
// It holds no clock, no goroutines and no randomness: every method that needs
// the time takes it as an argument (ADR 0004). It is not safe for concurrent
// use; the engine serialises access.
type Machine struct {
	opts     Options
	sessions map[events.SessionKey]*Session

	current   State
	since     time.Time
	reason    events.Kind
	source    string
	lastEvent *events.Event

	lastActivity time.Time
	stats        Stats

	forced      State
	forcedUntil time.Time
}

func New(o Options) *Machine {
	return &Machine{
		opts:     o,
		sessions: map[events.SessionKey]*Session{},
		current:  Sleeping,
	}
}

func (m *Machine) Options() Options { return m.opts }

// SetOptions swaps thresholds at runtime (config reload).
func (m *Machine) SetOptions(o Options) { m.opts = o }

// Force pins the pet to a state for a duration, for `petctl test`. It bypasses
// the event pipeline entirely so animations can be checked without an agent.
func (m *Machine) Force(now time.Time, s State, d time.Duration) {
	m.forced = s
	m.forcedUntil = now.Add(d)
}

func (m *Machine) ClearForce() {
	m.forced = ""
	m.forcedUntil = time.Time{}
}

// Apply folds one event into the machine. The event must already be normalised.
func (m *Machine) Apply(now time.Time, ev events.Event) {
	m.stats.EventsSeen++
	m.lastActivity = now
	e := ev
	m.lastEvent = &e

	if !ev.Event.Known() {
		// §6: tolerate unknown event types. They count as activity — something
		// is clearly happening — but they never drive a transition, because we
		// cannot know what they mean.
		return
	}

	key := ev.Key()
	s := m.sessions[key]
	if s == nil {
		// Any event from an unseen session implicitly opens it. An adapter that
		// starts mid-session (petd restarted while Claude was running) still works.
		m.evictForRoom()
		s = &Session{Key: key, StartedAt: now, Base: Idle}
		m.sessions[key] = s
		if ev.Event != events.SessionStarted {
			m.stats.SessionsStarted++
		}
	}
	s.LastEventAt = now
	s.Ended = false

	m.applyToSession(now, s, ev)
	m.reason = ev.Event
	m.source = ev.Source
}

func (m *Machine) applyToSession(now time.Time, s *Session, ev events.Event) {
	o := m.opts
	setTransient := func(st State, d time.Duration) {
		s.Transient = st
		s.TransientUntil = now.Add(d)
	}

	switch ev.Event {
	case events.SessionStarted:
		s.StartedAt = now
		s.Base = Idle
		s.RunningTools = 0
		s.Failures = 0
		m.stats.SessionsStarted++

	case events.SessionEnded:
		s.Ended = true
		s.Base = Idle
		s.RunningTools = 0

	case events.ThinkingStarted:
		s.Base = Thinking

	case events.Working:
		s.Base = Working

	case events.Idle:
		s.Base = Idle
		s.RunningTools = 0

	case events.ToolStarted:
		s.RunningTools++
		s.Base = Working
		if t := ev.Metadata["tool"]; t != "" {
			s.LastTool = t
		}
		// A tool starting means the agent is unblocked; clear any pending ask.
		s.Transient = ""

	case events.ToolFinished:
		if s.RunningTools > 0 {
			s.RunningTools--
		}
		s.Failures = 0
		if s.RunningTools == 0 {
			// Between tools the agent is reasoning about the result.
			s.Base = Thinking
		}

	case events.ToolFailed:
		if s.RunningTools > 0 {
			s.RunningTools--
		}
		s.Failures++
		if s.RunningTools == 0 {
			s.Base = Thinking
		}
		setTransient(m.failState(s), o.ConfusedFor)
		m.stats.Errors++

	case events.PermissionRequested, events.UserInputRequested:
		s.Base = Attention
		s.Transient = ""

	case events.TaskCompleted:
		s.Base = Idle
		s.RunningTools = 0
		s.Failures = 0
		s.TasksCompleted++
		m.stats.TasksCompleted++
		setTransient(Happy, o.HappyFor)

	case events.TaskFailed:
		s.Base = Idle
		s.RunningTools = 0
		s.Failures++
		m.stats.Errors++
		setTransient(m.failState(s), o.ConfusedFor)

	case events.TestsStarted:
		s.Base = Working

	case events.TestsPassed:
		s.Failures = 0
		m.stats.TestsPassed++
		setTransient(Celebrate, o.CelebrateFor)

	case events.TestsFailed:
		s.Failures++
		m.stats.TestsFailed++
		setTransient(m.failState(s), o.ConfusedFor)

	case events.GitCommit:
		m.stats.Commits++
		setTransient(Happy, o.HappyFor)

	case events.Error:
		s.Failures++
		m.stats.Errors++
		setTransient(m.failState(s), o.ConfusedFor)

	case events.PetInteraction:
		m.stats.Interactions++
		setTransient(Heart, o.HeartFor)

	case events.Heartbeat:
		// Activity clock only. Deliberately no transition: a heartbeat means
		// "still alive", not "still working".
	}
}

// maxSessions resolves the configured bound, treating zero as the default.
func (m *Machine) maxSessions() int {
	if m.opts.MaxSessions > 0 {
		return m.opts.MaxSessions
	}
	return DefaultMaxSessions
}

// evictForRoom makes space for one new session once the cap is reached. The
// real sessions are the ones an agent is actively driving, so the quietest
// session is the one to lose: a flood of one-event sessions evicts itself
// rather than pushing out the session the user is watching.
func (m *Machine) evictForRoom() {
	limit := m.maxSessions()
	for len(m.sessions) >= limit {
		var victimKey events.SessionKey
		var victim *Session
		for k, s := range m.sessions {
			if victim == nil || betterVictim(s, k, victim, victimKey) {
				victimKey, victim = k, s
			}
		}
		if victim == nil {
			return
		}
		delete(m.sessions, victimKey)
	}
}

// betterVictim reports whether session a should be evicted before b: ended
// sessions first, then whichever has been quiet longest, then the lower key.
// The final tie-break keeps eviction independent of Go's randomised map order,
// so the machine stays a pure function of (events, clock) — ADR 0004.
func betterVictim(a *Session, ak events.SessionKey, b *Session, bk events.SessionKey) bool {
	if a.Ended != b.Ended {
		return a.Ended
	}
	if !a.LastEventAt.Equal(b.LastEventAt) {
		return a.LastEventAt.Before(b.LastEventAt)
	}
	return ak.String() < bk.String()
}

// failState escalates repeated failures in one session from confused to worried.
func (m *Machine) failState(s *Session) State {
	if s.Failures >= m.opts.WorriedAfter {
		return Worried
	}
	return Confused
}

// Tick advances time-based transitions: transient decay and stale session
// cleanup. It is idempotent for a given `now`.
func (m *Machine) Tick(now time.Time) {
	for k, s := range m.sessions {
		if s.Transient != "" && !now.Before(s.TransientUntil) {
			s.Transient = ""
			s.TransientUntil = time.Time{}
		}
		// Drop sessions that ended and have nothing left to show, and sessions
		// that simply went silent.
		if s.Ended && s.Transient == "" {
			delete(m.sessions, k)
			continue
		}
		if now.Sub(s.LastEventAt) > m.opts.SessionStale {
			delete(m.sessions, k)
		}
	}
	if m.forced != "" && !now.Before(m.forcedUntil) {
		m.ClearForce()
	}
}

// Resolve computes the visible state. Call Tick first.
// Resolve computes the visible state. Call Tick first.
//
// Two rules, and no special cases inside either:
//
//   - No sessions, no pet. An agent that reported it was leaving is gone, and
//     so is one that never arrived; there is nothing to stay up for.
//   - Idle and quiet for SleepingAfter, and she sleeps.
//
// The second rule says `best == Idle` rather than "anything calmer than
// working", which is both shorter and the reason there is no exception list.
// A pending request is `attention`, not idle, so it is never slept through —
// that falls out of the rule instead of being carved out of it. Reactions are
// not idle either, though they expire in seconds and could never have reached
// the threshold anyway.
func (m *Machine) Resolve(now time.Time) State {
	if m.forced != "" && now.Before(m.forcedUntil) {
		return m.forced
	}

	best := State("")
	for _, s := range m.sessions {
		st := s.Effective(now, m.opts)
		if best == "" {
			best = st
		} else {
			best = Max(best, st)
		}
	}

	if best == "" {
		return Sleeping
	}
	if best == Idle && now.Sub(m.lastActivity) >= m.opts.SleepingAfter {
		return Sleeping
	}
	return best
}

// Advance ticks, resolves and records the transition. It returns the snapshot
// and whether the visible state changed.
func (m *Machine) Advance(now time.Time) (Snapshot, bool) {
	m.Tick(now)
	next := m.Resolve(now)
	changed := next != m.current
	if changed {
		m.current = next
		m.since = now
	}
	if m.since.IsZero() {
		m.since = now
	}
	return m.snapshot(now), changed
}

func (m *Machine) Snapshot(now time.Time) Snapshot { return m.snapshot(now) }

func (m *Machine) snapshot(now time.Time) Snapshot {
	views := make([]SessionView, 0, len(m.sessions))
	for _, s := range m.sessions {
		views = append(views, SessionView{
			Key:      s.Key,
			State:    s.Effective(now, m.opts),
			Duration: now.Sub(s.StartedAt),
			Idle:     now.Sub(s.LastEventAt),
			LastTool: s.LastTool,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Key.String() < views[j].Key.String() })

	snap := Snapshot{
		State:     m.current,
		Since:     m.since,
		Reason:    m.reason,
		Source:    m.source,
		Sessions:  views,
		Stats:     m.stats,
		Forced:    m.forced != "" && now.Before(m.forcedUntil),
		LastEvent: m.lastEvent,
	}
	if m.lastEvent != nil {
		snap.Meta = m.lastEvent.Metadata
	}
	return snap
}

// Current returns the visible state without advancing time.
func (m *Machine) Current() State { return m.current }
