// Package bubble turns state changes into short speech-bubble lines (§16).
//
// Everything here is template-driven. There is no LLM and no network call; the
// application is fully functional without one, which is the requirement. An
// LLM-backed Speaker could be added later as a second implementation of the
// same interface.
package bubble

import (
	"math/rand"
	"strings"
	"time"

	"github.com/lijunix/agent-digital-pet/internal/events"
	"github.com/lijunix/agent-digital-pet/internal/state"
)

// Message is one line to show above the pet.
type Message struct {
	Text string        `json:"text"`
	TTL  time.Duration `json:"ttl_ns"`
}

// Speaker decides whether to say something about a transition.
type Speaker interface {
	Say(now time.Time, prev, next state.State, ev *events.Event, sessionAge time.Duration) (Message, bool)
}

// trigger keys the template table.
type trigger string

const (
	tWake        trigger = "wake"
	tThinking    trigger = "thinking"
	tWorking     trigger = "working"
	tAttention   trigger = "attention"
	tConfused    trigger = "confused"
	tWorried     trigger = "worried"
	tHappy       trigger = "happy"
	tCelebrate   trigger = "celebrate"
	tSleeping    trigger = "sleeping"
	tTired       trigger = "tired"
	tHeart       trigger = "heart"
	tCommit      trigger = "commit"
	tTestsFail   trigger = "tests_failed"
	tLongSession trigger = "long_session"
)

// Templater is the built-in Speaker.
type Templater struct {
	Personality string
	Name        string
	// Enabled mirrors behavior.dialogue in config.
	Enabled bool

	rng    *rand.Rand
	lastAt time.Time
	// lastLongSessionNag prevents the "we've been at this a while" line from
	// becoming nagging (§21).
	lastLongSessionNag time.Time
	// recent guards against the same line twice in a row.
	recent map[trigger]int
}

// MinGap is the floor between any two bubbles. A pet that talks during every
// tool call is noise, not company.
const MinGap = 25 * time.Second

// LongSessionGap is how often the pet may comment on a long session.
const LongSessionGap = 45 * time.Minute

func NewTemplater(personality, name string, enabled bool, seed int64) *Templater {
	if personality == "" {
		personality = "gentle"
	}
	if name == "" {
		name = "your pet"
	}
	return &Templater{
		Personality: strings.ToLower(personality),
		Name:        name,
		Enabled:     enabled,
		rng:         rand.New(rand.NewSource(seed)),
		recent:      map[trigger]int{},
	}
}

func (t *Templater) Say(now time.Time, prev, next state.State, ev *events.Event, sessionAge time.Duration) (Message, bool) {
	if !t.Enabled {
		return Message{}, false
	}

	tr, ttl, ok := t.triggerFor(prev, next, ev)
	if !ok {
		return Message{}, false
	}

	// attention is the one thing allowed to interrupt the quiet period: it is
	// the case where the user genuinely needs to look at the terminal.
	if tr != tAttention && now.Sub(t.lastAt) < MinGap {
		return Message{}, false
	}
	if tr == tLongSession && now.Sub(t.lastLongSessionNag) < LongSessionGap {
		return Message{}, false
	}

	text := t.pick(tr)
	if text == "" {
		return Message{}, false
	}
	t.lastAt = now
	if tr == tLongSession {
		t.lastLongSessionNag = now
	}
	return Message{Text: text, TTL: ttl}, true
}

func (t *Templater) triggerFor(prev, next state.State, ev *events.Event) (trigger, time.Duration, bool) {
	if ev != nil {
		switch ev.Event {
		case events.GitCommit:
			return tCommit, 5 * time.Second, true
		case events.TestsFailed:
			return tTestsFail, 6 * time.Second, true
		}
	}
	if prev == next {
		return "", 0, false
	}
	switch next {
	case state.Attention:
		return tAttention, 20 * time.Second, true
	case state.Celebrate:
		return tCelebrate, 6 * time.Second, true
	case state.Happy:
		return tHappy, 5 * time.Second, true
	case state.Worried:
		return tWorried, 6 * time.Second, true
	case state.Confused:
		return tConfused, 6 * time.Second, true
	case state.Sleeping:
		return tSleeping, 5 * time.Second, true
	case state.Tired:
		return tLongSession, 8 * time.Second, true
	case state.Heart:
		return tHeart, 4 * time.Second, true
	case state.Working:
		if prev == state.Sleeping {
			return tWake, 5 * time.Second, true
		}
		return tWorking, 4 * time.Second, true
	case state.Thinking:
		if prev == state.Sleeping {
			return tWake, 5 * time.Second, true
		}
		return "", 0, false
	case state.Idle:
		if prev == state.Sleeping {
			return tWake, 5 * time.Second, true
		}
		return "", 0, false
	}
	return "", 0, false
}

func (t *Templater) pick(tr trigger) string {
	set := templates[t.Personality]
	if set == nil {
		set = templates["gentle"]
	}
	lines := set[tr]
	if len(lines) == 0 {
		lines = templates["gentle"][tr]
	}
	if len(lines) == 0 {
		return ""
	}
	// Rotate past the last line used for this trigger, then jump a random extra
	// step. Excluding zero guarantees the same line never repeats back to back
	// while the sequence still feels varied.
	i := t.recent[tr]
	if n := len(lines); n > 1 {
		i = (i + 1 + t.rng.Intn(n-1)) % n
	}
	t.recent[tr] = i
	return strings.ReplaceAll(lines[i], "{name}", t.Name)
}

// Presets from §8. Personality changes what the pet says and how loudly it
// celebrates; it never changes anything about permissions or security.
var templates = map[string]map[trigger][]string{
	"gentle": {
		tWake:        {"Oh, you're back.", "Morning.", "Ready when you are."},
		tWorking:     {"On it.", "Working away.", "Let's see."},
		tAttention:   {"Claude needs you.", "Something's waiting for you.", "Your turn."},
		tConfused:    {"Hm, that didn't work.", "Something went sideways.", "Let's try again."},
		tWorried:     {"That's a few in a row.", "Maybe take a look at this one.", "This one's stubborn."},
		tHappy:       {"Done.", "That one's finished.", "Nice."},
		tCelebrate:   {"Tests passed!", "All green.", "Everything's passing."},
		tTestsFail:   {"Tests are unhappy.", "Some tests failed.", "Red, for now."},
		tCommit:      {"Committed.", "Saved that one.", "Nice, it's in."},
		tSleeping:    {"I'll nap here.", "Resting.", "Wake me up any time."},
		tLongSession: {"We've been at this a while.", "Maybe stretch a little?", "Long one today."},
		tHeart:       {"Hello there.", "That's nice.", "Aw."},
	},
	"cheerful": {
		tWake:        {"Hey hey!", "You're back!", "Let's go!"},
		tWorking:     {"Working on it!", "Here we go!", "Busy busy!"},
		tAttention:   {"Claude needs you!", "Hey! Over here!", "You're needed!"},
		tConfused:    {"Whoops!", "That didn't go to plan.", "Hmm, weird."},
		tWorried:     {"Okay, that's three.", "This one's fighting back.", "Ooh, tricky."},
		tHappy:       {"Done and done!", "Another one!", "Nailed it."},
		tCelebrate:   {"Tests passed! 🎉", "ALL GREEN!", "Look at that!"},
		tTestsFail:   {"Aw, some tests failed.", "Not green yet!", "So close."},
		tCommit:      {"Committed!", "That's in the history books.", "Saved!"},
		tSleeping:    {"Nap time!", "Zzz...", "See you soon!"},
		tLongSession: {"Big session today!", "Water break?", "You've earned a stretch."},
		tHeart:       {"Hi!", "Yay!", "That's the good stuff."},
	},
	"calm": {
		tWake:        {"Ready.", "Beginning.", "Here."},
		tWorking:     {"Working.", "In progress.", "Running."},
		tAttention:   {"Input needed.", "Waiting on you.", "A decision is required."},
		tConfused:    {"An error occurred.", "That failed.", "Unexpected result."},
		tWorried:     {"Three failures now.", "Repeated failure.", "This is not converging."},
		tHappy:       {"Complete.", "Finished.", "Task done."},
		tCelebrate:   {"Tests passed.", "All tests green.", "Suite is passing."},
		tTestsFail:   {"Tests failed.", "Suite is red.", "Failures reported."},
		tCommit:      {"Commit recorded.", "Committed.", "Change saved."},
		tSleeping:    {"Idle.", "Sleeping.", "Standing by."},
		tLongSession: {"Two hours elapsed.", "Long session.", "Consider a break."},
		tHeart:       {"Noted.", "Thank you.", "Acknowledged."},
	},
	"mischievous": {
		tWake:        {"Ooh, what are we breaking today?", "Back for more?", "This'll be fun."},
		tWorking:     {"Meddling.", "Poking at things.", "Doing crimes to your codebase."},
		tAttention:   {"It wants permission. Say yes. Or don't.", "Somebody needs your signature.", "Decision time!"},
		tConfused:    {"Ha. Broke it.", "That's a new one.", "Interesting failure."},
		tWorried:     {"Three for three. Impressive.", "It really doesn't want to work.", "Stubborn little thing."},
		tHappy:       {"Done. You're welcome.", "Too easy.", "Another one bites the dust."},
		tCelebrate:   {"Tests passed! Suspicious.", "All green. What did you skip?", "Green! Don't touch anything."},
		tTestsFail:   {"Tests said no.", "The suite has opinions.", "Red. Very red."},
		tCommit:      {"Committed. No takebacks.", "It's in the history now.", "Future you will love this."},
		tSleeping:    {"Napping. Don't do anything fun.", "Zzz.", "Wake me for the interesting bugs."},
		tLongSession: {"Still here? Bold.", "Your chair misses standing.", "This is hour three, you know."},
		tHeart:       {"Careful, I'll get used to that.", "Ooh.", "More of that."},
	},
	"sarcastic": {
		tWake:        {"Oh good, we're doing this.", "Back already.", "Delighted."},
		tWorking:     {"Doing the thing.", "Busy. Obviously.", "Working. Allegedly."},
		tAttention:   {"It needs you. Again.", "Your input is required. Shocking.", "Permission, please."},
		tConfused:    {"That went well.", "Flawless.", "Perfect, no notes."},
		tWorried:     {"Third time's the charm, surely.", "Still failing. Bold strategy.", "We're committed to this bit now."},
		tHappy:       {"It's done. Somehow.", "Task complete. Astonishing.", "Well, that worked."},
		tCelebrate:   {"Tests passed. I'm as surprised as you.", "Green. Enjoy it while it lasts.", "All passing. For now."},
		tTestsFail:   {"Tests failed. Called it.", "Red again.", "The suite disagrees."},
		tCommit:      {"Committed. It's permanent now.", "That's in the log forever.", "Git remembers."},
		tSleeping:    {"Finally, rest.", "Zzz. Don't wake me.", "Sleeping. Earned it."},
		tLongSession: {"Three hours. Very normal.", "Your posture is a war crime.", "Consider: outside."},
		tHeart:       {"Fine, that was nice.", "Don't tell anyone.", "Hm. Acceptable."},
	},
	"energetic": {
		tWake:        {"LET'S GO!", "I'm up! I'm up!", "Ready ready ready!"},
		tWorking:     {"GOING!", "Full speed!", "Crunching!"},
		tAttention:   {"HEY! You're needed!", "Permission! Now!", "Look at the terminal!"},
		tConfused:    {"Ouch!", "That broke!", "Error! Error!"},
		tWorried:     {"THREE fails!", "It keeps happening!", "Something's really wrong!"},
		tHappy:       {"DONE!", "Next!", "Boom. Finished."},
		tCelebrate:   {"TESTS PASSED!", "ALL GREEN! 🎉", "PERFECT RUN!"},
		tTestsFail:   {"Tests failed!", "Red!", "Try again!"},
		tCommit:      {"COMMITTED!", "In the repo!", "Locked in!"},
		tSleeping:    {"Powering down...", "Zzz!", "Recharging!"},
		tLongSession: {"Marathon session!", "Stretch break! Go!", "Hydrate!"},
		tHeart:       {"YES!", "Best human.", "More!"},
	},
}

// Presets returns the available personality names.
func Presets() []string {
	return []string{"gentle", "cheerful", "calm", "mischievous", "sarcastic", "energetic"}
}
