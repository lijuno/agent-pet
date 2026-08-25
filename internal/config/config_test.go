package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lijuno/agent-pet/internal/flavor"
)

func TestLoadCreatesDefaultsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Pet.Active != "momo" || cfg.Server.Addr != "127.0.0.1:9876" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file should have been written for the user to edit: %v", err)
	}
}

// A partial file must only override the keys it names — otherwise editing one
// setting silently resets everything else to zero values.
func TestPartialConfigKeepsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("pet:\n  active: byte\n  scale: 2\n"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Pet.Active != "byte" || cfg.Pet.Scale != 2 {
		t.Fatalf("the file's values should win: %+v", cfg.Pet)
	}
	if cfg.Server.Addr != "127.0.0.1:9876" {
		t.Fatalf("unmentioned keys should keep their defaults, got %q", cfg.Server.Addr)
	}
	// Compared against the default rather than a literal: this test is about
	// unmentioned keys keeping their defaults, not about what the default is.
	if cfg.Thresholds.SleepingAfter != Default().Thresholds.SleepingAfter {
		t.Fatalf("thresholds should keep their defaults, got %v", cfg.Thresholds.SleepingAfter.D())
	}
}

func TestDurationsParseInTheDocumentedForm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("thresholds:\n  sleeping_after: 45m\n  idle_after: 10s\n"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Thresholds.SleepingAfter.D() != 45*time.Minute {
		t.Fatalf("got %v", cfg.Thresholds.SleepingAfter.D())
	}
	if cfg.Thresholds.IdleAfter.D() != 10*time.Second {
		t.Fatalf("got %v", cfg.Thresholds.IdleAfter.D())
	}
}

// A zero duration is not "no waiting", it is a threshold nobody meant to set:
// zero tool_patience would put the pet to sleep in the middle of a long tool,
// which is exactly the bug the threshold exists to prevent.
func TestZeroThresholdIsCorrected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("thresholds:\n  tool_patience: 0s\n"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Thresholds.ToolPatience != Default().Thresholds.ToolPatience {
		t.Fatalf("a zero threshold should be corrected, got %v", cfg.Thresholds.ToolPatience.D())
	}
}

// A pet that refuses to start because of a typo would be a bad pet.
func TestBrokenConfigFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("pet: [this is not a mapping\n"), 0o644)

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("the parse failure should be reported")
	}
	if cfg.Pet.Active != "momo" {
		t.Fatalf("but the config must still be usable, got %+v", cfg.Pet)
	}
}

func TestSanitisation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("pet:\n  scale: -4\n  active: \"\"\nthresholds:\n  worried_after: 0\n"), 0o644)

	cfg, _ := Load(path)
	if cfg.Pet.Scale != 1.0 {
		t.Fatalf("a nonsense scale should be corrected, got %v", cfg.Pet.Scale)
	}
	if cfg.Pet.Active == "" {
		t.Fatal("an empty pet id should be corrected")
	}
	if cfg.Thresholds.WorriedAfter <= 0 {
		t.Fatal("worried_after must stay positive")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	in := Default()
	in.Pet.Active = "byte"
	in.Personality.Preset = "sarcastic"
	in.Thresholds.SleepingAfter = Duration(90 * time.Minute)
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Pet.Active != "byte" || out.Personality.Preset != "sarcastic" {
		t.Fatalf("round trip lost data: %+v", out)
	}
	if out.Thresholds.SleepingAfter.D() != 90*time.Minute {
		t.Fatalf("durations should survive the round trip, got %v", out.Thresholds.SleepingAfter.D())
	}
}

func TestIsLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:9876", "localhost:9876", "[::1]:9876", "127.1.2.3:80"} {
		if !IsLoopback(addr) {
			t.Errorf("%s should be loopback", addr)
		}
	}
	for _, addr := range []string{"0.0.0.0:9876", "192.168.1.10:9876", "[2001:db8::1]:80", "example.com:80"} {
		if IsLoopback(addr) {
			t.Errorf("%s must not be treated as loopback", addr)
		}
	}
}

// The shadow defaults on, which makes it the one boolean here that a config
// file written before it existed could silently turn off — an absent key and
// an explicit `false` decode to the same zero value unless the defaults are
// what the file is decoded into. They are; this holds that.
func TestShadowSurvivesAConfigThatPredatesIt(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "old.yaml")
	os.WriteFile(old, []byte("pet:\n  active: momo\n  scale: 1\n"), 0o644)
	cfg, err := Load(old)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Pet.DropShadow {
		t.Fatal("a file with no drop_shadow key should keep the shadow")
	}

	off := filepath.Join(dir, "off.yaml")
	os.WriteFile(off, []byte("pet:\n  drop_shadow: false\n"), 0o644)
	cfg, err = Load(off)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Pet.DropShadow {
		t.Fatal("an explicit false should turn the shadow off")
	}

	// And it has to survive the rewrite on shutdown, or the setting lasts
	// exactly as long as the session that changed it.
	back := filepath.Join(dir, "saved.yaml")
	if err := Save(back, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if cfg, err = Load(back); err != nil || cfg.Pet.DropShadow {
		t.Fatalf("the shadow should still be off after a round trip: %+v %v", cfg.Pet, err)
	}
}

// A fresh installation checks once a day. This test asserted the opposite
// until the default was flipped and it was left behind, which is worth a note:
// it is the kind of test that reads as policy, so a stale one describes a
// promise the program no longer keeps.
//
// The half that still has to hold is the second one. Load decodes into
// Default(), so every key a config file omits takes the current default —
// which is what lets a new default reach an installation that predates it.
// Somebody who turned checks off has that written down, and no upgrade may
// talk them round.
func TestUpdateChecksAreOnByDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Update.Check {
		t.Error("update.check defaults to off")
	}
	if cfg.Update.Interval.D() != 24*time.Hour {
		t.Errorf("update.interval = %s, want 24h", cfg.Update.Interval.D())
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("update:\n  check: false\n"), 0o644)
	if cfg, err = Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Update.Check {
		t.Error("an upgrade turned checks back on for somebody who had switched them off")
	}
}

func TestEmptyManifestURLFallsBackToTheDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("update:\n  manifest_url: \"\"\n  interval: 0s\n"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Update.ManifestURL != DefaultManifestURL {
		t.Errorf("manifest_url = %q, want the default", cfg.Update.ManifestURL)
	}
	if cfg.Update.Interval <= 0 {
		t.Error("a zero interval would check on every session start")
	}
}

// Two programs need this path: petctl writes it after a check, and the desktop
// shell deletes it when the channel changes. Neither may spell it itself.
func TestUpdateStampLivesInTheDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_PET_DATA", dir)
	if got, want := UpdateStamp(), filepath.Join(dir, "update-check"); got != want {
		t.Errorf("UpdateStamp() = %q, want %q", got, want)
	}
}

// The dev app and the release app must never edit each other's settings, and
// the only thing keeping them apart is the flavor in the path.
func TestPathsAreNamedAfterTheFlavour(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/cfg")
	t.Setenv("XDG_DATA_HOME", "/tmp/data")
	t.Setenv("AGENT_PET_CONFIG", "")
	t.Setenv("AGENT_PET_DATA", "")

	slug := flavor.Current().Slug
	if got, want := Path(), filepath.Join("/tmp/cfg", slug, "config.yaml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if got, want := DataDir(), filepath.Join("/tmp/data", slug); got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
	// The port too: two pets that share one are one pet, and the second exits
	// at startup looking like a build that did nothing.
	if Default().Server.Addr != flavor.Current().Addr {
		t.Errorf("default addr %q is not the flavour's %q", Default().Server.Addr, flavor.Current().Addr)
	}
}
