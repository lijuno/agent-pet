// Package claude translates Claude Code hook payloads into pet events.
//
// It does not import internal/. The contract between an adapter and the pet is
// the wire format documented in docs/events.md, not a Go type, so the event
// shape is restated here on purpose: petd and this adapter are free to be
// different versions of themselves.
package claude

import (
	"encoding/json"
	"strings"
)

// Source is the value every event from this adapter carries.
const Source = "claude"

// Event is the body POSTed to /event. See docs/events.md.
type Event struct {
	Source    string            `json:"source"`
	Event     string            `json:"event"`
	SessionID string            `json:"session_id,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// hookPayload is the subset of the hook stdin JSON this adapter reads. Claude
// Code sends a good deal more (transcript_path, permission_mode, effort, cwd);
// none of it is any of the pet's business, and §26 says not to take what we do
// not need. Unknown fields are ignored rather than rejected so a newer Claude
// Code cannot break the adapter.
type hookPayload struct {
	HookEventName string `json:"hook_event_name"`
	SessionID     string `json:"session_id"`
	ToolName      string `json:"tool_name"`
	// Reason distinguishes the two very different things SessionEnd means.
	Reason string `json:"reason"`
}

// Hooks are the hook events this adapter subscribes to, in the order they are
// written into settings.json. Install and uninstall both work from this list,
// so adding a mapping below is the only edit needed to support a new one.
//
// Every entry maps to something Claude Code actually reports. Nothing here is
// inferred from the shape of a command line or the text of a message: the
// adapters README is explicit that a pet which cries wolf about permission is
// worse than a pet that stays quiet.
var Hooks = []string{
	"SessionStart",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"PermissionRequest",
	"Notification",
	"Stop",
	"StopFailure",
	"SessionEnd",
}

// mapping is the whole translation table.
//
// PostToolUse fires only when a tool succeeded; failures arrive as a separate
// PostToolUseFailure event. That is why tool_failed needs no guesswork — and
// why a failing `go test` surfaces as a confused pet on its own, without this
// adapter pattern-matching command lines to decide what a test run is.
var mapping = map[string]string{
	"SessionStart":       "session_started",
	"UserPromptSubmit":   "thinking_started",
	"PreToolUse":         "tool_started",
	"PostToolUse":        "tool_finished",
	"PostToolUseFailure": "tool_failed",
	"PermissionRequest":  "permission_requested",
	"Notification":       "user_input_requested",
	"Stop":               "task_completed",
	"StopFailure":        "error",
	// Only when the reason says Claude Code is going away; see endsTheAgent.
	"SessionEnd": "session_ended",
}

// endsTheAgent reports whether a SessionEnd reason means Claude Code itself is
// going away, rather than one conversation being replaced by another.
//
// Only two reasons say so plainly. Everything else — `clear`, `resume`, a
// reason this build has never heard of, or none at all — is treated as the
// agent still being there, which is the safer way to be wrong: a pet that
// stays in colour for a Claude Code that has quit still falls asleep a minute
// later, while a pet that greys out for one that is running is simply lying.
func endsTheAgent(reason string) bool {
	switch reason {
	case "prompt_input_exit", "logout":
		return true
	}
	return false
}

// Translate converts one hook payload into an event. It reports false when the
// payload is unusable or the hook is one this build has no mapping for, which
// the caller treats as "say nothing" rather than as an error: a hook must never
// fail the thing it is hooked to.
func Translate(stdin []byte) (Event, bool) {
	var p hookPayload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return Event{}, false
	}
	name, ok := mapping[p.HookEventName]
	if !ok {
		return Event{}, false
	}
	if p.HookEventName == "SessionEnd" && !endsTheAgent(p.Reason) {
		// The conversation ended, not Claude Code. Rewinding, /clear and
		// resuming another session all fire SessionEnd while the agent carries
		// on running — reporting those as the agent leaving is what made the
		// pet grey out with Claude Code still open, and stay grey until the
		// next session happened to begin.
		//
		// The session really is over, so it goes idle rather than being left
		// mid-tool. What it must not do is disappear.
		name = "idle"
	}
	ev := Event{Source: Source, Event: name, SessionID: p.SessionID}

	// The tool name is the one piece of payload worth showing: it is what the
	// status panel displays, and it is a bare identifier like "Bash" or "Edit",
	// never a command line or an argument. §26 keeps prompts, code and
	// arguments out of the pet entirely.
	if t := strings.TrimSpace(p.ToolName); t != "" {
		ev.Metadata = map[string]string{"tool": t}
	}
	// One of a handful of fixed words, and the thing you need to know when the
	// pet greys out at the wrong moment.
	if r := strings.TrimSpace(p.Reason); r != "" && p.HookEventName == "SessionEnd" {
		ev.Metadata = map[string]string{"reason": r}
	}
	return ev, true
}
