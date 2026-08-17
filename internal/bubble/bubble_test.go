package bubble

import (
	"testing"
	"time"

	"github.com/lijuno/agent-digital-pet/internal/events"
	"github.com/lijuno/agent-digital-pet/internal/state"
)

var t0 = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestSilentWhenDialogueDisabled(t *testing.T) {
	s := NewTemplater("gentle", "Momo", false, 1)
	if _, ok := s.Say(t0, state.Idle, state.Celebrate, nil, 0); ok {
		t.Fatal("dialogue is off; the pet should say nothing")
	}
}

func TestSpeaksOnMeaningfulTransitions(t *testing.T) {
	s := NewTemplater("gentle", "Momo", true, 1)
	m, ok := s.Say(t0, state.Working, state.Celebrate, nil, 0)
	if !ok || m.Text == "" {
		t.Fatal("a celebration is worth commenting on")
	}
	if m.TTL <= 0 {
		t.Fatal("a bubble needs a lifetime")
	}
}

// A pet that talks during every tool call is noise, not company.
func TestRateLimited(t *testing.T) {
	s := NewTemplater("cheerful", "Momo", true, 7)
	if _, ok := s.Say(t0, state.Idle, state.Happy, nil, 0); !ok {
		t.Fatal("the first line should be allowed")
	}
	if _, ok := s.Say(t0.Add(2*time.Second), state.Happy, state.Celebrate, nil, 0); ok {
		t.Fatal("a second line two seconds later should be suppressed")
	}
	if _, ok := s.Say(t0.Add(MinGap+time.Second), state.Celebrate, state.Happy, nil, 0); !ok {
		t.Fatal("after the quiet period the pet may speak again")
	}
}

// The one exception: the user genuinely needs to know a request is waiting.
func TestAttentionBypassesTheQuietPeriod(t *testing.T) {
	s := NewTemplater("calm", "Momo", true, 3)
	s.Say(t0, state.Idle, state.Happy, nil, 0)
	if _, ok := s.Say(t0.Add(time.Second), state.Happy, state.Attention, nil, 0); !ok {
		t.Fatal("an attention request must not be swallowed by rate limiting")
	}
}

// §21: playful, not nagging.
func TestLongSessionCommentIsRare(t *testing.T) {
	s := NewTemplater("gentle", "Momo", true, 11)
	if _, ok := s.Say(t0, state.Idle, state.Tired, nil, 2*time.Hour); !ok {
		t.Fatal("the first long-session comment should be allowed")
	}
	later := t0.Add(MinGap + time.Minute)
	if _, ok := s.Say(later, state.Idle, state.Tired, nil, 3*time.Hour); ok {
		t.Fatal("the pet must not nag about the same long session minutes later")
	}
	muchLater := t0.Add(LongSessionGap + time.Minute)
	if _, ok := s.Say(muchLater, state.Idle, state.Tired, nil, 4*time.Hour); !ok {
		t.Fatal("after a long gap one more comment is fine")
	}
}

func TestNoTransitionNoTalk(t *testing.T) {
	s := NewTemplater("gentle", "Momo", true, 5)
	if _, ok := s.Say(t0, state.Working, state.Working, nil, 0); ok {
		t.Fatal("staying in the same state is not worth a line")
	}
}

func TestEventDrivenLinesDoNotNeedAStateChange(t *testing.T) {
	s := NewTemplater("gentle", "Momo", true, 5)
	e := events.Event{Source: "git", Event: events.GitCommit}
	if _, ok := s.Say(t0, state.Working, state.Working, &e, 0); !ok {
		t.Fatal("a commit is worth acknowledging even without a state change")
	}
}

func TestEveryPresetCoversEveryTrigger(t *testing.T) {
	base := templates["gentle"]
	for _, name := range Presets() {
		set, ok := templates[name]
		if !ok {
			t.Fatalf("preset %q has no templates", name)
		}
		for tr := range base {
			if len(set[tr]) == 0 {
				t.Errorf("preset %q is missing lines for %q", name, tr)
			}
		}
	}
}

func TestUnknownPresetFallsBackToGentle(t *testing.T) {
	s := NewTemplater("shakespearean", "Momo", true, 2)
	if _, ok := s.Say(t0, state.Idle, state.Attention, nil, 0); !ok {
		t.Fatal("an unrecognised personality should still speak")
	}
}
