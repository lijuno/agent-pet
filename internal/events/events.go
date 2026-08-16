// Package events defines the internal event vocabulary that every adapter
// translates into. Nothing in this package knows about Claude Code, Codex, git,
// or any other producer.
package events

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Kind is an event name. It is a plain string rather than an enum because the
// system must tolerate unknown event types (§6): an adapter from a newer
// version may send something this build has never heard of, and that must not
// be an error.
type Kind string

const (
	SessionStarted Kind = "session_started"
	SessionEnded   Kind = "session_ended"

	ThinkingStarted Kind = "thinking_started"
	Working         Kind = "working"
	Idle            Kind = "idle"

	ToolStarted  Kind = "tool_started"
	ToolFinished Kind = "tool_finished"
	ToolFailed   Kind = "tool_failed"

	PermissionRequested Kind = "permission_requested"
	UserInputRequested  Kind = "user_input_requested"

	TaskCompleted Kind = "task_completed"
	TaskFailed    Kind = "task_failed"

	TestsStarted Kind = "tests_started"
	TestsPassed  Kind = "tests_passed"
	TestsFailed  Kind = "tests_failed"

	GitCommit Kind = "git_commit"

	Error     Kind = "error"
	Heartbeat Kind = "heartbeat"

	// PetInteraction is produced by the UI, not by an agent, when the user
	// pets the character.
	PetInteraction Kind = "pet_interaction"
)

// known is used for diagnostics and for deciding whether an event should drive
// a state transition. Unknown events are still accepted and still refresh the
// activity clock.
var known = map[Kind]bool{
	SessionStarted: true, SessionEnded: true,
	ThinkingStarted: true, Working: true, Idle: true,
	ToolStarted: true, ToolFinished: true, ToolFailed: true,
	PermissionRequested: true, UserInputRequested: true,
	TaskCompleted: true, TaskFailed: true,
	TestsStarted: true, TestsPassed: true, TestsFailed: true,
	GitCommit: true, Error: true, Heartbeat: true,
	PetInteraction: true,
}

func (k Kind) Known() bool { return known[k] }

// All returns every event name this build understands, sorted for stable
// output in `petctl` help and diagnostics.
func All() []Kind {
	out := make([]Kind, 0, len(known))
	for k := range known {
		out = append(out, k)
	}
	sortKinds(out)
	return out
}

func sortKinds(k []Kind) {
	for i := 1; i < len(k); i++ {
		for j := i; j > 0 && k[j] < k[j-1]; j-- {
			k[j], k[j-1] = k[j-1], k[j]
		}
	}
}

// Event is the single shape that flows through the system.
//
// Everything in an Event other than Source and Event is untrusted input from an
// external process (§26). It is sanitised on arrival and is never executed,
// interpolated into a shell command, or rendered as HTML.
type Event struct {
	Source    string            `json:"source"`
	Event     Kind              `json:"event"`
	SessionID string            `json:"session_id,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Limits on untrusted input. Generous enough for real tool names, small enough
// that a misbehaving adapter cannot grow the process.
const (
	MaxSourceLen    = 32
	MaxSessionIDLen = 64
	MaxKindLen      = 64
	MaxMetaKeys     = 16
	MaxMetaKeyLen   = 32
	MaxMetaValueLen = 256
)

// Normalize sanitises an event received from outside the process and fills in
// defaults. It never fails: an event that cannot be understood becomes an event
// with an unknown Kind, which the state machine ignores. Rejecting would mean a
// future adapter version could break a running pet.
func (e Event) Normalize(now time.Time) Event {
	out := Event{
		Source:    clean(e.Source, MaxSourceLen),
		Event:     Kind(clean(string(e.Event), MaxKindLen)),
		SessionID: clean(e.SessionID, MaxSessionIDLen),
		Timestamp: e.Timestamp,
	}
	if out.Source == "" {
		out.Source = "unknown"
	}
	if out.Timestamp.IsZero() {
		out.Timestamp = now
	}
	// A clock-skewed adapter must not park the pet in the future or resurrect
	// an ancient event as if it were current.
	if out.Timestamp.After(now.Add(time.Minute)) || out.Timestamp.Before(now.Add(-24*time.Hour)) {
		out.Timestamp = now
	}
	if out.SessionID == "" {
		// One implicit session per source, so an adapter that cannot report a
		// session id (Codex, git hooks) still works.
		out.SessionID = "default"
	}
	if len(e.Metadata) > 0 {
		out.Metadata = make(map[string]string, min(len(e.Metadata), MaxMetaKeys))
		for _, k := range sortedKeys(e.Metadata) {
			if len(out.Metadata) >= MaxMetaKeys {
				break
			}
			ck := clean(k, MaxMetaKeyLen)
			if ck == "" {
				continue
			}
			out.Metadata[ck] = clean(e.Metadata[k], MaxMetaValueLen)
		}
	}
	return out
}

// Key identifies the session an event belongs to.
func (e Event) Key() SessionKey { return SessionKey{Source: e.Source, ID: e.SessionID} }

// SessionKey is the identity of one agent session.
type SessionKey struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

func (k SessionKey) String() string { return k.Source + "/" + k.ID }

// clean strips control characters, collapses whitespace and truncates. Control
// characters are the reason this exists: an agent that puts a terminal escape
// sequence or a newline in a tool name must not be able to corrupt the log file
// or the speech bubble.
func clean(s string, maxLen int) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			// Newlines and tabs become a single space so a multi-line value
			// cannot forge extra log lines.
			if !lastSpace && b.Len() > 0 {
				if b.Len()+1 > maxLen {
					break
				}
				b.WriteByte(' ')
				lastSpace = true
			}
		case r == utf8.RuneError, !unicode.IsPrint(r):
			// Control characters, terminal escapes and invalid UTF-8 are dropped.
		default:
			if b.Len()+utf8.RuneLen(r) > maxLen {
				return strings.TrimSpace(b.String())
			}
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
