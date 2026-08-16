package events

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func TestNormalizeFillsDefaults(t *testing.T) {
	got := Event{Event: SessionStarted}.Normalize(now)
	if got.Source != "unknown" {
		t.Fatalf("missing source should become 'unknown', got %q", got.Source)
	}
	if got.SessionID != "default" {
		t.Fatalf("missing session should become 'default', got %q", got.SessionID)
	}
	if !got.Timestamp.Equal(now) {
		t.Fatalf("missing timestamp should become now, got %v", got.Timestamp)
	}
}

// A hook that emits a tool name containing an ANSI escape or a newline must not
// be able to forge a log line or inject anything into the bubble.
func TestNormalizeStripsControlCharacters(t *testing.T) {
	e := Event{
		Source: "claude",
		Event:  ToolStarted,
		Metadata: map[string]string{
			"tool": "bash\x1b[31m\nFAKE 12:00:00 claude session_started",
		},
	}.Normalize(now)

	v := e.Metadata["tool"]
	if strings.ContainsAny(v, "\n\r\x1b") {
		t.Fatalf("control characters survived: %q", v)
	}
	if !strings.HasPrefix(v, "bash") {
		t.Fatalf("legitimate content should be preserved, got %q", v)
	}
}

func TestNormalizeCapsMetadata(t *testing.T) {
	meta := map[string]string{}
	for i := 0; i < 100; i++ {
		meta[string(rune('a'+i%26))+strings.Repeat("x", i)] = strings.Repeat("v", 1000)
	}
	e := Event{Source: "x", Event: Working, Metadata: meta}.Normalize(now)
	if len(e.Metadata) > MaxMetaKeys {
		t.Fatalf("want at most %d keys, got %d", MaxMetaKeys, len(e.Metadata))
	}
	for k, v := range e.Metadata {
		if len(k) > MaxMetaKeyLen {
			t.Fatalf("key %q exceeds %d bytes", k, MaxMetaKeyLen)
		}
		if len(v) > MaxMetaValueLen {
			t.Fatalf("value for %q is %d bytes, max %d", k, len(v), MaxMetaValueLen)
		}
	}
}

func TestNormalizeTruncatesOnRuneBoundaries(t *testing.T) {
	e := Event{
		Source:   "x",
		Event:    Working,
		Metadata: map[string]string{"k": strings.Repeat("→", 500)},
	}.Normalize(now)
	v := e.Metadata["k"]
	for _, r := range v {
		if r == 0xFFFD {
			t.Fatalf("truncation split a rune: %q", v)
		}
	}
}

func TestNormalizeRejectsImplausibleTimestamps(t *testing.T) {
	future := Event{Source: "x", Event: Working, Timestamp: now.Add(72 * time.Hour)}.Normalize(now)
	if !future.Timestamp.Equal(now) {
		t.Fatalf("a far-future timestamp should be clamped, got %v", future.Timestamp)
	}
	ancient := Event{Source: "x", Event: Working, Timestamp: now.Add(-100 * time.Hour)}.Normalize(now)
	if !ancient.Timestamp.Equal(now) {
		t.Fatalf("an ancient timestamp should be clamped, got %v", ancient.Timestamp)
	}
	fine := now.Add(-30 * time.Second)
	ok := Event{Source: "x", Event: Working, Timestamp: fine}.Normalize(now)
	if !ok.Timestamp.Equal(fine) {
		t.Fatalf("a plausible timestamp should be kept, got %v", ok.Timestamp)
	}
}

func TestUnknownKindsAreAccepted(t *testing.T) {
	e := Event{Source: "future-agent", Event: "warp_drive_engaged"}.Normalize(now)
	if e.Event.Known() {
		t.Fatal("the event should be reported as unknown")
	}
	if e.Event != "warp_drive_engaged" {
		t.Fatalf("the name should survive normalisation, got %q", e.Event)
	}
}

func TestAllIsSorted(t *testing.T) {
	all := All()
	for i := 1; i < len(all); i++ {
		if all[i-1] > all[i] {
			t.Fatalf("All() must be sorted: %s before %s", all[i-1], all[i])
		}
	}
	if len(all) < 18 {
		t.Fatalf("expected the full event vocabulary, got %d", len(all))
	}
}
