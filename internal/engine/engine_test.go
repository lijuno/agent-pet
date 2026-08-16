package engine

import (
	"io"
	"log/slog"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lijunix/agent-digital-pet/internal/config"
	"github.com/lijunix/agent-digital-pet/internal/events"
	"github.com/lijunix/agent-digital-pet/internal/petassets"
	"github.com/lijunix/agent-digital-pet/internal/state"
)

const manifestA = `{"id":"a","name":"Alpha","animations":{"idle":"idle.png","working":"working.png"}}`
const manifestB = `{"id":"b","name":"Beta","animations":{"idle":"idle.png"}}`

type recorder struct {
	events []events.Event
	states []state.State
}

func (r *recorder) Record(ev events.Event, snap state.Snapshot) {
	r.events = append(r.events, ev)
	r.states = append(r.states, snap.State)
}

func newEngine(t *testing.T) (*Engine, *recorder, *time.Time) {
	t.Helper()
	lib := petassets.NewLibrary()
	err := lib.LoadBuiltin(fstest.MapFS{
		"pets/a/manifest.json": {Data: []byte(manifestA)},
		"pets/b/manifest.json": {Data: []byte(manifestB)},
	}, "pets", "/pets")
	if err != nil {
		t.Fatalf("pets: %v", err)
	}
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	rec := &recorder{}
	cfg := config.Default()
	cfg.Pet.Active = "a"
	e := New(cfg, lib, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithClock(func() time.Time { return now }),
		WithSink(rec))
	return e, rec, &now
}

func TestSubmitDrivesStateAndFeedsSinks(t *testing.T) {
	e, rec, _ := newEngine(t)

	snap := e.Submit(events.Event{Source: "claude", Event: events.ToolStarted})
	if snap.State != state.Working {
		t.Fatalf("want working, got %s", snap.State)
	}
	if len(rec.events) != 1 || rec.events[0].Event != events.ToolStarted {
		t.Fatalf("the sink should have seen the event, got %+v", rec.events)
	}
	// The sink receives the normalised event, not the raw one.
	if rec.events[0].SessionID != "default" {
		t.Fatalf("the sink should receive a normalised event, got %q", rec.events[0].SessionID)
	}
}

func TestSubscriberGetsCurrentStateThenUpdates(t *testing.T) {
	e, _, _ := newEngine(t)

	ch, cancel := e.Subscribe()
	defer cancel()

	select {
	case first := <-ch:
		if first.Pet != "a" {
			t.Fatalf("a new subscriber should get the current picture, got %+v", first)
		}
	default:
		t.Fatal("a new subscriber should receive the current state immediately")
	}

	e.Submit(events.Event{Source: "claude", Event: events.PermissionRequested})
	select {
	case up := <-ch:
		if up.Snapshot.State != state.Attention {
			t.Fatalf("want attention, got %s", up.Snapshot.State)
		}
	default:
		t.Fatal("the subscriber should have been notified")
	}
}

// A subscriber that stops reading must not be able to block the engine.
func TestSlowSubscriberDoesNotBlockTheEngine(t *testing.T) {
	e, _, _ := newEngine(t)
	_, cancel := e.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			e.Submit(events.Event{Source: "claude", Event: events.Heartbeat})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the engine blocked on a subscriber that never read")
	}
}

func TestSwitchPetWithoutRestart(t *testing.T) {
	e, _, _ := newEngine(t)
	if p, ok := e.ActivePet(); !ok || p.ID != "a" {
		t.Fatalf("expected pet a, got %v/%v", p.ID, ok)
	}
	if _, ok := e.SetPet("nope"); ok {
		t.Fatal("switching to an unknown pet should fail")
	}
	p, ok := e.SetPet("b")
	if !ok || p.Name != "Beta" {
		t.Fatalf("switch failed: %+v %v", p, ok)
	}
	if got := e.Last().Pet; got != "b" {
		t.Fatalf("the update should carry the new pet, got %q", got)
	}
}

func TestForceAndClear(t *testing.T) {
	e, _, now := newEngine(t)
	e.Force(state.Celebrate, 5*time.Second)
	if got := e.Snapshot(); got.State != state.Celebrate {
		t.Fatalf("want celebrate, got %s", got.State)
	}
	e.ClearForce()
	if got := e.Snapshot(); got.State == state.Celebrate {
		t.Fatal("clearing should release the forced state")
	}
	_ = now
}

func TestTickDecaysTransientState(t *testing.T) {
	e, _, now := newEngine(t)
	e.Submit(events.Event{Source: "claude", Event: events.TaskCompleted})
	if e.Snapshot().State != state.Happy {
		t.Fatalf("want happy, got %s", e.Snapshot().State)
	}
	*now = now.Add(30 * time.Second)
	e.Tick()
	if got := e.Last().Snapshot.State; got != state.Idle {
		t.Fatalf("happy should have decayed to idle, got %s", got)
	}
}

func TestConfigReloadChangesThresholds(t *testing.T) {
	e, _, now := newEngine(t)
	cfg := e.Config()
	cfg.Thresholds.SleepingAfter = config.Duration(time.Minute)
	e.SetConfig(cfg)

	e.Submit(events.Event{Source: "claude", Event: events.SessionEnded})
	*now = now.Add(90 * time.Second)
	e.Tick()
	if got := e.Last().Snapshot.State; got != state.Sleeping {
		t.Fatalf("the new sleep threshold should apply, got %s", got)
	}
}
