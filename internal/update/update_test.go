package update

import (
	"encoding/json"
	"strings"
	"testing"
)

// good is a manifest that must always parse, so a test asserting a rejection
// can change one field and know that field is the reason.
func good() map[string]any {
	return map[string]any{
		"channel": "release",
		"version": "0.2.0",
		"url":     "https://github.com/lijuno/agent-pet/releases/download/v0.2.0/agent-pet-0.2.0-universal.zip",
		"sha256":  strings.Repeat("a", 64),
		"size":    31457280,
	}
}

func manifestBytes(t *testing.T, m map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseManifestAcceptsAGoodOne(t *testing.T) {
	m, err := ParseManifest(manifestBytes(t, good()), Release)
	if err != nil {
		t.Fatalf("a valid manifest was rejected: %v", err)
	}
	if m.Version != "0.2.0" {
		t.Errorf("version = %q, want 0.2.0", m.Version)
	}
}

func TestParseManifestRejects(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		// The download URL is the field a hostile manifest would rewrite, so
		// every way of pointing it elsewhere is refused.
		{"a plain http asset", "url", "http://github.com/lijuno/agent-pet/releases/download/v1/a.zip"},
		{"another host", "url", "https://evil.test/lijuno/agent-pet/releases/download/v1/a.zip"},
		{"a host that merely starts the same", "url", "https://github.com.evil.test/lijuno/agent-pet/releases/download/v1/a.zip"},
		{"another repository", "url", "https://github.com/someone/else/releases/download/v1/a.zip"},
		{"a repository whose name starts the same", "url", "https://github.com/lijuno/agent-pet-evil/releases/download/v1/a.zip"},
		{"something that is not a release asset", "url", "https://github.com/lijuno/agent-pet/raw/master/a.zip"},
		{"a file: url", "url", "file:///tmp/a.zip"},
		{"a truncated hash", "sha256", strings.Repeat("a", 63)},
		{"a hash with non-hex in it", "sha256", strings.Repeat("g", 64)},
		{"an uppercase hash", "sha256", strings.Repeat("A", 64)},
		{"no size", "size", 0},
		{"an implausible size", "size", MaxAssetSize + 1},
		{"a negative size", "size", -1},
		{"a version that is not one", "version", "latest"},
		{"a version with only two parts", "version", "0.2"},
		{"an empty version", "version", ""},
		{"an unknown channel", "channel", "nightly"},
		{"a notes url off github", "notes_url", "https://evil.test/notes"},
		{"a javascript: notes url", "notes_url", "javascript:alert(1)"},
		{"a min_macos that is not a version", "min_macos", "Sonoma"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := good()
			m[c.field] = c.value
			if _, err := ParseManifest(manifestBytes(t, m), Release); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

// A manifest served at dev.json that claims to be the release channel means
// somebody has published the wrong file. Taking it would move a user between
// streams without asking.
func TestParseManifestRefusesTheWrongChannel(t *testing.T) {
	if _, err := ParseManifest(manifestBytes(t, good()), Dev); err == nil {
		t.Fatal("a release manifest was accepted for the dev channel")
	}
}

// An old client must not act on a manifest containing a field it does not
// understand: whatever that field says, it is being ignored.
func TestParseManifestRefusesUnknownFields(t *testing.T) {
	m := good()
	m["requires_confirmation"] = false
	if _, err := ParseManifest(manifestBytes(t, m), Release); err == nil {
		t.Fatal("a manifest with an unknown field was accepted")
	}
}

func TestManifestURLFillsTheChannel(t *testing.T) {
	const tpl = "https://raw.githubusercontent.com/lijuno/agent-pet/master/updates/{channel}.json"
	for _, c := range Channels {
		got, err := ManifestURL(tpl, c)
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		want := "https://raw.githubusercontent.com/lijuno/agent-pet/master/updates/" + string(c) + ".json"
		if got != want {
			t.Errorf("%s -> %q, want %q", c, got, want)
		}
	}
}

func TestManifestURLRefusesHTTPAndUnknownChannels(t *testing.T) {
	if _, err := ManifestURL("http://example.test/{channel}.json", Release); err == nil {
		t.Error("an http manifest URL was accepted")
	}
	if _, err := ManifestURL("https://example.test/{channel}.json", Channel("../../etc")); err == nil {
		t.Error("an unknown channel was accepted into a URL")
	}
}

func TestParseChannel(t *testing.T) {
	for _, s := range []string{"release", "Release", " dev ", "DEV"} {
		if _, ok := ParseChannel(s); !ok {
			t.Errorf("ParseChannel(%q) rejected a real channel", s)
		}
	}
	for _, s := range []string{"", "nightly", "release/../dev", "release dev"} {
		if c, ok := ParseChannel(s); ok {
			t.Errorf("ParseChannel(%q) returned %q", s, c)
		}
	}
}

func TestStatusValidate(t *testing.T) {
	// Anything running as this user can post one of these, and it ends up in a
	// menu-bar title.
	bad := []Status{
		{Latest: "0.2.0; rm -rf /", Available: true},
		{Latest: "<b>0.2.0</b>", Available: true},
		{Latest: "0.2.0", Available: true, NotesURL: "javascript:alert(1)"},
		{Latest: "0.2.0", Available: true, NotesURL: "https://evil.test/x"},
		{Latest: "0.2.0", Available: true, Channel: "nightly"},
		{Available: true},
	}
	for _, s := range bad {
		s := s
		if err := s.Validate(); err == nil {
			t.Errorf("Validate accepted %+v", s)
		}
	}

	s := Status{
		Channel: Release, Current: "0.1.0", Latest: "0.2.0", Available: true,
		NotesURL: "https://github.com/lijuno/agent-pet/releases/tag/v0.2.0",
		Error:    "a message\nwith\r\nnewlines\x07",
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("a good status was rejected: %v", err)
	}
	if strings.ContainsAny(s.Error, "\n\r\x07") {
		t.Errorf("control characters survived into %q", s.Error)
	}

	// "dev" is what an untagged build reports, and it has to be sayable.
	dev := Status{Current: DevBuild, Channel: Dev}
	if err := dev.Validate(); err != nil {
		t.Errorf("a dev build could not report its own version: %v", err)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.2.0", 0},
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "0.99.99", 1},
		{"0.2.1", "0.2.0", 1},
		{"v0.2.0", "0.2.0", 0},
		// A prerelease comes before the release it leads to.
		{"0.3.0-dev.1", "0.3.0", -1},
		{"0.3.0", "0.3.0-dev.1", 1},
		{"0.3.0-dev.1", "0.3.0-dev.2", -1},
		{"0.3.0-dev.2", "0.3.0-dev.10", -1},
		{"0.3.0-alpha", "0.3.0-beta", -1},
		{"0.3.0-dev.1", "0.3.0-dev.1", 0},
		// Numeric identifiers rank below alphanumeric ones.
		{"0.3.0-1", "0.3.0-alpha", -1},
		{"0.3.0-dev", "0.3.0-dev.1", -1},
		// A version that cannot be parsed never wins.
		{"garbage", "0.1.0", -1},
		{"0.1.0", "garbage", 1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for _, s := range []string{"0.1.0", "1.2.3", "0.3.0-dev.1", "v1.0.0", "10.20.30"} {
		if !ValidVersion(s) {
			t.Errorf("ValidVersion(%q) = false", s)
		}
	}
	for _, s := range []string{
		"", "dev", "1.2", "1.2.3.4", "1.2.x", "01.2.3", "1.2.3-",
		"1.2.3+build", "1.2.3 ", "-1.2.3", "1.2.3-dev..1", "latest",
	} {
		if ValidVersion(s) {
			t.Errorf("ValidVersion(%q) = true", s)
		}
	}
}

func TestNewer(t *testing.T) {
	if !Newer("0.1.0", "0.2.0") {
		t.Error("0.2.0 is newer than 0.1.0")
	}
	if Newer("0.2.0", "0.1.0") {
		t.Error("offered a downgrade")
	}
	if Newer("0.2.0", "0.2.0") {
		t.Error("offered the version already installed")
	}
	// The one that matters: a developer's own build is never replaced by a
	// release, whatever the numbers say.
	if Newer(DevBuild, "9.9.9") {
		t.Error("offered to overwrite a dev build")
	}
	if Newer("0.1.0", "garbage") {
		t.Error("offered an unparseable version")
	}
}

// sw_vers says "15.6" and a manifest sensibly says "12.0". Neither is semver,
// and both have to be comparable or the minimum-OS field is useless.
func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"15.6": "15.6.0", "12.0": "12.0.0", "15.6.1": "15.6.1", "v14.0": "14.0.0",
	} {
		got, ok := Normalize(in)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"Sonoma", "", "15", "15.x"} {
		if got, ok := Normalize(in); ok {
			t.Errorf("Normalize(%q) = %q, true", in, got)
		}
	}
	if Compare("15.6.0", "12.0.0") <= 0 {
		t.Error("15.6 should be newer than 12.0")
	}
}
