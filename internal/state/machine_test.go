package state

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/lijuno/agent-pet/internal/events"
)

// clock is a synthetic clock. The machine takes `now` as an argument
// everywhere, so tests never sleep and never flake (ADR 0004).
type clock struct{ t time.Time }

func newClock() *clock {
	return &clock{t: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
}
func (c *clock) now() time.Time                { return c.t }
func (c *clock) add(d time.Duration) time.Time { c.t = c.t.Add(d); return c.t }

func ev(kind events.Kind, session string) events.Event {
	return events.Event{Source: "claude", Event: kind, SessionID: session}
}

// step applies an event and returns the resulting visible state.
func step(m *Machine, c *clock, e events.Event) State {
	m.Apply(c.now(), e)
	s, _ := m.Advance(c.now())
	return s.State
}

func advance(m *Machine, c *clock, d time.Duration) State {
	c.add(d)
	s, _ := m.Advance(c.now())
	return s.State
}

// TestSpecExampleSequence walks the exact transition chain in §7 of the
// requirements.
func TestSpecExampleSequence(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())

	if got := m.Current(); got != Sleeping {
		t.Fatalf("a fresh pet should be asleep, got %s", got)
	}

	cases := []struct {
		event events.Kind
		want  State
	}{
		{events.SessionStarted, Idle},
		{events.ThinkingStarted, Thinking},
		{events.ToolStarted, Working},
		{events.TaskCompleted, Happy},
	}
	for _, tc := range cases {
		if got := step(m, c, ev(tc.event, "a")); got != tc.want {
			t.Fatalf("after %s: want %s, got %s", tc.event, tc.want, got)
		}
	}

	// happy is transient and decays back to idle
	if got := advance(m, c, DefaultOptions().HappyFor+time.Second); got != Idle {
		t.Fatalf("happy should decay to idle, got %s", got)
	}
	// and then, after the sleep threshold, to sleeping
	if got := advance(m, c, DefaultOptions().SleepingAfter); got != Sleeping {
		t.Fatalf("idle should become sleeping after the timeout, got %s", got)
	}
}

func TestToolLifecycleReturnsToThinking(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())

	step(m, c, ev(events.SessionStarted, "a"))
	step(m, c, ev(events.ToolStarted, "a"))
	if got := step(m, c, ev(events.ToolStarted, "a")); got != Working {
		t.Fatalf("two concurrent tools should still be working, got %s", got)
	}
	if got := step(m, c, ev(events.ToolFinished, "a")); got != Working {
		t.Fatalf("one tool still running, want working, got %s", got)
	}
	if got := step(m, c, ev(events.ToolFinished, "a")); got != Thinking {
		t.Fatalf("no tools running, want thinking, got %s", got)
	}
}

// TestPriorityAcrossSessions is the §22 scenario: three concurrent sessions in
// different states reduce to the highest-priority one.
func TestPriorityAcrossSessions(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())

	m.Apply(c.now(), ev(events.ToolStarted, "a"))                                                 // working
	m.Apply(c.now(), events.Event{Source: "codex", Event: events.SessionStarted, SessionID: "c"}) // idle
	snap, _ := m.Advance(c.now())
	if snap.State != Working {
		t.Fatalf("working should beat idle, got %s", snap.State)
	}

	m.Apply(c.now(), ev(events.PermissionRequested, "b"))
	snap, _ = m.Advance(c.now())
	if snap.State != Attention {
		t.Fatalf("attention must win over working, got %s", snap.State)
	}
	if len(snap.Sessions) != 3 {
		t.Fatalf("want 3 tracked sessions, got %d", len(snap.Sessions))
	}

	// Answering the request in session b releases attention; a is still working.
	if got := step(m, c, ev(events.ToolStarted, "b")); got != Working {
		t.Fatalf("after the permission is answered, want working, got %s", got)
	}
}

func TestRepeatedFailuresEscalateToWorried(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())
	step(m, c, ev(events.SessionStarted, "a"))

	if got := step(m, c, ev(events.Error, "a")); got != Confused {
		t.Fatalf("first error should be confused, got %s", got)
	}
	step(m, c, ev(events.Error, "a"))
	if got := step(m, c, ev(events.Error, "a")); got != Worried {
		t.Fatalf("third error should escalate to worried, got %s", got)
	}
	// A success clears the streak.
	step(m, c, ev(events.ToolStarted, "a"))
	step(m, c, ev(events.ToolFinished, "a"))
	if got := step(m, c, ev(events.Error, "a")); got != Confused {
		t.Fatalf("after a success the streak resets, want confused, got %s", got)
	}
}

func TestAttentionTimesOut(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	m := New(o)
	step(m, c, ev(events.PermissionRequested, "a"))

	// A pending request is `attention`, not idle, so the sleep rule does not
	// apply to it at all. Nobody comes to answer a prompt they were never
	// shown.
	if got := advance(m, c, o.SleepingAfter+time.Second); got != Attention {
		t.Fatalf("a pending request must outlast the sleep threshold, got %s", got)
	}
	if got := advance(m, c, o.AttentionTimeout-o.SleepingAfter-2*time.Second); got != Attention {
		t.Fatalf("attention should persist until the timeout, got %s", got)
	}
	// Having given up, and with nothing having happened for far longer than
	// the sleep threshold, sleeping is where it lands.
	if got := advance(m, c, 2*time.Second); got != Sleeping {
		t.Fatalf("an unanswered request should give up and sleep, got %s", got)
	}
}

// An agent that said it was leaving is gone. There is nothing to stay up for,
// and no timer to wait out: the pet used to stand there awake for a full
// SleepingAfter after the session closed.
func TestReportedExitSleepsAtOnce(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())
	step(m, c, ev(events.SessionStarted, "a"))
	step(m, c, ev(events.ToolStarted, "a"))
	if got := step(m, c, ev(events.SessionEnded, "a")); got != Sleeping {
		t.Fatalf("a reported exit should sleep at once, got %s", got)
	}
	if s := m.Snapshot(c.now()); len(s.Sessions) != 0 {
		t.Fatalf("the session should be gone, got %d", len(s.Sessions))
	}
}

// The sleep rule reads `best == Idle`, so anything that is not idle is simply
// outside it. This is the case that matters: a request nobody answered.
func TestPendingRequestIsNeverSleptThrough(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	m := New(o)
	step(m, c, ev(events.PermissionRequested, "a"))
	for i := 0; i < 5; i++ {
		if got := advance(m, c, o.SleepingAfter); got != Attention {
			t.Fatalf("after %v quiet the request should still show, got %s",
				time.Duration(i+1)*o.SleepingAfter, got)
		}
	}
}

func TestSilentAgentFallsBackToIdle(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	m := New(o)
	step(m, c, ev(events.ToolStarted, "a"))

	if got := advance(m, c, o.IdleAfter+time.Second); got != Idle {
		t.Fatalf("an agent that stopped reporting should not stay working, got %s", got)
	}
}

func TestSessionEndedKeepsCelebrationThenClearsSession(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	m := New(o)
	step(m, c, ev(events.SessionStarted, "a"))
	step(m, c, ev(events.TestsPassed, "a"))
	if got := step(m, c, ev(events.SessionEnded, "a")); got != Celebrate {
		t.Fatalf("the celebration should survive the session ending, got %s", got)
	}
	advance(m, c, o.CelebrateFor+time.Second)
	if s := m.Snapshot(c.now()); len(s.Sessions) != 0 {
		t.Fatalf("the ended session should be dropped once its reaction expires, got %d", len(s.Sessions))
	}
}

func TestStaleSessionIsForgotten(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	m := New(o)
	step(m, c, ev(events.ToolStarted, "a"))
	advance(m, c, o.SessionStale+time.Minute)
	if s := m.Snapshot(c.now()); len(s.Sessions) != 0 {
		t.Fatalf("a crashed agent must not leak a session, got %d", len(s.Sessions))
	}
	if m.Current() != Sleeping {
		t.Fatalf("with no sessions and no activity the pet sleeps, got %s", m.Current())
	}
}

// An adapter bug that mints a fresh session id per event must not grow the
// machine without limit, nor bloat every /state and /stream payload with
// sessions nobody is watching.
func TestSessionFloodIsBounded(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	o.MaxSessions = 8
	m := New(o)

	for i := 0; i < 500; i++ {
		c.add(time.Second)
		step(m, c, ev(events.ToolStarted, "flood-"+strconv.Itoa(i)))
	}
	if s := m.Snapshot(c.now()); len(s.Sessions) > o.MaxSessions {
		t.Fatalf("session flood must stay bounded at %d, got %d", o.MaxSessions, len(s.Sessions))
	}
}

// Zero must not read as "unbounded": machineOptions builds Options field by
// field, so an unset cap has to fall back to the default rather than disable it.
func TestZeroMaxSessionsStillBounded(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	o.MaxSessions = 0
	m := New(o)

	for i := 0; i < DefaultMaxSessions*3; i++ {
		c.add(time.Second)
		step(m, c, ev(events.ToolStarted, "flood-"+strconv.Itoa(i)))
	}
	if s := m.Snapshot(c.now()); len(s.Sessions) > DefaultMaxSessions {
		t.Fatalf("an unset cap must fall back to %d, got %d", DefaultMaxSessions, len(s.Sessions))
	}
}

// The session the user is actually watching must survive a flood of
// single-event sessions, and eviction must not disturb the visible state.
func TestFloodDoesNotEvictTheActiveSession(t *testing.T) {
	c := newClock()
	o := DefaultOptions()
	o.MaxSessions = 4
	m := New(o)

	step(m, c, ev(events.PermissionRequested, "real"))
	for i := 0; i < 50; i++ {
		c.add(time.Second)
		step(m, c, ev(events.Heartbeat, "flood-"+strconv.Itoa(i)))
		// Keep the real session the most recently active one.
		step(m, c, ev(events.PermissionRequested, "real"))
	}

	var found bool
	for _, s := range m.Snapshot(c.now()).Sessions {
		if s.Key.ID == "real" {
			found = true
		}
	}
	if !found {
		t.Fatal("the session the user is watching was evicted by a flood")
	}
	if got := m.Current(); got != Attention {
		t.Fatalf("eviction must not disturb the visible state: want attention, got %s", got)
	}
}

// Eviction must not reintroduce map-iteration nondeterminism (ADR 0004).
func TestEvictionIsDeterministic(t *testing.T) {
	run := func() []string {
		c := newClock()
		o := DefaultOptions()
		o.MaxSessions = 5
		m := New(o)
		// Same timestamp for every session, so only the tie-break decides.
		for i := 0; i < 40; i++ {
			step(m, c, ev(events.ToolStarted, "s-"+strconv.Itoa(i)))
		}
		var keys []string
		for _, s := range m.Snapshot(c.now()).Sessions {
			keys = append(keys, s.Key.String())
		}
		return keys
	}
	want := run()
	for i := 0; i < 20; i++ {
		if got := run(); !reflect.DeepEqual(got, want) {
			t.Fatalf("eviction differed between runs:\n %v\n %v", want, got)
		}
	}
}

func TestUnknownEventCountsAsActivityButDoesNotTransition(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())
	step(m, c, ev(events.SessionStarted, "a"))
	before := m.Current()

	got := step(m, c, ev("quantum_entangled", "a"))
	if got != before {
		t.Fatalf("an unknown event must not change state: %s -> %s", before, got)
	}
	if s := m.Snapshot(c.now()); s.Stats.EventsSeen != 2 {
		t.Fatalf("unknown events should still be counted, got %d", s.Stats.EventsSeen)
	}
}

func TestForcedStateOverridesEverythingThenExpires(t *testing.T) {
	c := newClock()
	m := New(DefaultOptions())
	step(m, c, ev(events.PermissionRequested, "a"))

	m.Force(c.now(), Celebrate, 10*time.Second)
	s, _ := m.Advance(c.now())
	if s.State != Celebrate || !s.Forced {
		t.Fatalf("petctl test should pin the state, got %s forced=%v", s.State, s.Forced)
	}
	if got := advance(m, c, 11*time.Second); got != Attention {
		t.Fatalf("after the forced state expires the real state returns, got %s", got)
	}
}

func TestDeterminism(t *testing.T) {
	seq := []events.Kind{
		events.SessionStarted, events.ThinkingStarted, events.ToolStarted,
		events.ToolFailed, events.Error, events.PermissionRequested,
		events.ToolStarted, events.TaskCompleted, events.TestsPassed,
		events.GitCommit, events.SessionEnded,
	}
	run := func() []State {
		c := newClock()
		m := New(DefaultOptions())
		var out []State
		for _, k := range seq {
			out = append(out, step(m, c, ev(k, "a")))
			out = append(out, advance(m, c, 2*time.Second))
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same events produced different states at %d: %s vs %s", i, a[i], b[i])
		}
	}
}

func TestPriorityOrderMatchesSpec(t *testing.T) {
	order := []State{Attention, Worried, Confused, Celebrate, Happy, Working, Thinking, Idle, Sleeping}
	for i := 1; i < len(order); i++ {
		if Priority(order[i-1]) <= Priority(order[i]) {
			t.Fatalf("%s should outrank %s", order[i-1], order[i])
		}
	}
}

func TestFallbackChainsTerminateAtIdle(t *testing.T) {
	for _, s := range All() {
		if s == Idle {
			continue
		}
		chain := Fallback[s]
		if len(chain) == 0 {
			t.Fatalf("%s has no fallback chain", s)
		}
		if chain[len(chain)-1] != Idle {
			t.Fatalf("%s fallback must end at idle, got %v", s, chain)
		}
	}
}
