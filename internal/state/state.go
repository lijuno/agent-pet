package state

// State is a visible pet state. These are exactly the states in §7 of the
// requirements; a pet asset pack is expected to provide an animation for each.
type State string

const (
	Sleeping  State = "sleeping"
	Idle      State = "idle"
	Thinking  State = "thinking"
	Working   State = "working"
	Attention State = "attention"
	Confused  State = "confused"
	Worried   State = "worried"
	Happy     State = "happy"
	Celebrate State = "celebrate"
	Heart     State = "heart"
)

// All returns every state, in the order a pet pack should provide them.
func All() []State {
	return []State{Sleeping, Idle, Thinking, Working, Attention, Confused, Worried, Happy, Celebrate, Heart}
}

func Valid(s State) bool {
	for _, v := range All() {
		if v == s {
			return true
		}
	}
	return false
}

// priority implements the reduction order from §22, extended to cover every
// state. When several sessions are in different states, the pet shows the
// highest-priority one: the user needs to see "something wants you" before
// "something is busy".
var priority = map[State]int{
	Attention: 100,
	Worried:   90,
	Confused:  80,
	Celebrate: 70,
	Happy:     60,
	Heart:     55,
	Working:   50,
	Thinking:  40,
	Idle:      20,
	Sleeping:  10,
}

func Priority(s State) int { return priority[s] }

// Max returns the higher-priority of two states.
func Max(a, b State) State {
	if Priority(b) > Priority(a) {
		return b
	}
	return a
}

// Fallback is the chain a renderer walks when a pet pack is missing an
// animation for a state. Every chain terminates at Idle, which every pack must
// provide.
var Fallback = map[State][]State{
	Sleeping:  {Idle},
	Thinking:  {Working, Idle},
	Working:   {Thinking, Idle},
	Attention: {Confused, Idle},
	Confused:  {Worried, Idle},
	Worried:   {Confused, Idle},
	Happy:     {Celebrate, Idle},
	Celebrate: {Happy, Idle},
	Heart:     {Happy, Idle},
}
