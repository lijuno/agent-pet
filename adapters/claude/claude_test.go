package claude

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTranslateCoversEveryInstalledHook(t *testing.T) {
	// Whatever we ask Claude Code to send us, we must know what to do with.
	for _, h := range Hooks {
		payload := []byte(`{"hook_event_name":"` + h + `","session_id":"abc"}`)
		ev, ok := Translate(payload)
		if !ok {
			t.Fatalf("%s is installed but has no mapping", h)
		}
		if ev.Source != Source || ev.Event == "" {
			t.Fatalf("%s produced a malformed event: %+v", h, ev)
		}
		if ev.SessionID != "abc" {
			t.Fatalf("%s dropped the session id", h)
		}
	}
}

func TestToolNameIsCarriedButNothingElseIs(t *testing.T) {
	payload := []byte(`{
		"hook_event_name":"PreToolUse",
		"session_id":"abc",
		"tool_name":"Bash",
		"tool_input":{"command":"rm -rf /very/secret/path"},
		"cwd":"/Users/someone/secret-project",
		"transcript_path":"/Users/someone/.claude/transcript.json"
	}`)
	ev, ok := Translate(payload)
	if !ok {
		t.Fatal("PreToolUse should translate")
	}
	if ev.Event != "tool_started" {
		t.Fatalf("want tool_started, got %s", ev.Event)
	}
	if got := ev.Metadata["tool"]; got != "Bash" {
		t.Fatalf("want tool=Bash, got %q", got)
	}
	// §26: no prompts, no code, no command arguments, no paths.
	blob, _ := json.Marshal(ev)
	for _, leak := range []string{"rm -rf", "secret-project", "transcript", "/Users/"} {
		if strings.Contains(string(blob), leak) {
			t.Fatalf("event leaked %q: %s", leak, blob)
		}
	}
}

// Failure is reported by Claude Code, never inferred here.
func TestToolFailureComesFromItsOwnHook(t *testing.T) {
	ok, _ := Translate([]byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`))
	if ok.Event != "tool_finished" {
		t.Fatalf("PostToolUse fires only on success, want tool_finished, got %s", ok.Event)
	}
	bad, _ := Translate([]byte(`{"hook_event_name":"PostToolUseFailure","tool_name":"Bash"}`))
	if bad.Event != "tool_failed" {
		t.Fatalf("want tool_failed, got %s", bad.Event)
	}
}

func TestUnusablePayloadsAreSilent(t *testing.T) {
	for _, in := range []string{
		``,
		`not json at all`,
		`{}`,
		`{"hook_event_name":"SomeHookFromTheFuture","session_id":"a"}`,
		`null`,
	} {
		if _, ok := Translate([]byte(in)); ok {
			t.Fatalf("input %q should have been ignored, not translated", in)
		}
	}
}

func TestInstallPreservesEverythingElse(t *testing.T) {
	original := []byte(`{
	  "model": "opus",
	  "env": {"FOO": "bar"},
	  "hooks": {
	    "PreToolUse": [
	      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/audit.sh"}]}
	    ]
	  }
	}`)
	out, err := Install(original, "/usr/local/bin/petctl")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "opus" {
		t.Fatalf("install dropped an unrelated setting: %s", out)
	}
	if env, _ := got["env"].(map[string]any); env["FOO"] != "bar" {
		t.Fatalf("install dropped a nested setting: %s", out)
	}
	if !strings.Contains(string(out), "/usr/local/bin/audit.sh") {
		t.Fatalf("install dropped the user's own hook: %s", out)
	}
}

// The round trip is the property that matters: adding and removing our hooks
// must leave a file the user would not notice we had touched.
func TestUninstallRestoresTheOriginal(t *testing.T) {
	for _, original := range []string{
		`{"model":"opus"}`,
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/bin/audit.sh"}]}]}}`,
		`{}`,
	} {
		installed, err := Install([]byte(original), "/usr/local/bin/petctl")
		if err != nil {
			t.Fatal(err)
		}
		removed, err := Uninstall(installed)
		if err != nil {
			t.Fatal(err)
		}
		var want, got any
		_ = json.Unmarshal([]byte(original), &want)
		if err := json.Unmarshal(removed, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("round trip changed the file:\n original: %s\n after:    %s", original, removed)
		}
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	once, err := Install([]byte(`{}`), "/usr/local/bin/petctl")
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Install(once, "/usr/local/bin/petctl")
	if err != nil {
		t.Fatal(err)
	}
	n, err := Installed(twice)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(Hooks) {
		t.Fatalf("installing twice should leave %d hooks, got %d", len(Hooks), n)
	}
}

// Re-running install after the binary moves should repoint the hooks, not
// leave a second set aimed at a path that no longer exists.
func TestReinstallUpdatesThePath(t *testing.T) {
	old, err := Install([]byte(`{}`), "/old/path/petctl")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Install(old, "/new/path/petctl")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "/old/path/petctl") {
		t.Fatalf("stale path survived reinstall: %s", updated)
	}
	if n, _ := Installed(updated); n != len(Hooks) {
		t.Fatalf("want %d hooks after reinstall, got %d", len(Hooks), n)
	}
}

// Overwriting a file we could not parse would destroy real configuration.
func TestMalformedSettingsAreRefusedNotOverwritten(t *testing.T) {
	if _, err := Install([]byte(`{"hooks": `), "/usr/local/bin/petctl"); err == nil {
		t.Fatal("a malformed settings file must be refused, not replaced")
	}
	if _, err := Uninstall([]byte(`{oops`)); err == nil {
		t.Fatal("uninstall must refuse a malformed settings file too")
	}
}

func TestEmptyFileIsFine(t *testing.T) {
	out, err := Install([]byte("  \n"), "/usr/local/bin/petctl")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := Installed(out); n != len(Hooks) {
		t.Fatalf("want %d hooks, got %d", len(Hooks), n)
	}
}

func TestPathWithSpacesIsQuoted(t *testing.T) {
	cmd := Command("/Users/a b/petctl")
	if !strings.HasPrefix(cmd, `"/Users/a b/petctl"`) {
		t.Fatalf("a path with a space must be quoted, got %s", cmd)
	}
	if strings.HasPrefix(Command("/usr/bin/petctl"), `"`) {
		t.Fatal("an ordinary path should not be quoted")
	}
}
