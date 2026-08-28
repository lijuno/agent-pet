package config

import (
	"os"
	"path/filepath"
	"reflect"
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
	if cfg.Pet.Active != "sanmao" || cfg.Server.Addr != "127.0.0.1:9876" {
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
	if cfg.Pet.Active != "sanmao" {
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
	os.WriteFile(old, []byte("pet:\n  active: sanmao\n  scale: 1\n"), 0o644)
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

// TestRetiredPetIdIsCarriedForward covers the one rename that has happened to a
// shipped pack. Sanmao's id was `momo` for the app's whole life until three
// characters made an id nobody could match to a name too expensive to keep, so
// every config.yaml written before the rename names a pack that is no longer
// there. Left alone, Any() falls back to whichever character sorts first and a
// cat owner opens the app to find a stranger in the window.
func TestRetiredPetIdIsCarriedForward(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("pet:\n  active: momo\n"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Pet.Active != "sanmao" {
		t.Fatalf("a config naming the retired id should land on sanmao, got %q", cfg.Pet.Active)
	}
}

// settled makes two configs comparable across a trip through YAML, which turns
// a nil slice into an empty one. They mean the same thing and print the same,
// so without this a failure message shows two identical structs.
func settled(c Config) Config {
	if len(c.Pet.Disabled) == 0 {
		c.Pet.Disabled = nil
	}
	return c
}

// The whole point of SaveOwned: an edit made while the pet is running is still
// there afterwards. It was not — the app wrote its startup copy back over the
// file on every scale change, every character change and every quit.
func TestSaveOwnedKeepsAnEditMadeWhileTheAppRan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(
		"thresholds:\n  idle_after: 45s\npersonality:\n  name: Byte\npet:\n  sound: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// What the app has held since before that edit, plus a window it moved.
	live := Default()
	live.Window.X, live.Window.Y = 300, 120

	if err := SaveOwned(path, live); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Thresholds.IdleAfter.D() != 45*time.Second {
		t.Errorf("idle_after is %s; the edit was overwritten", got.Thresholds.IdleAfter.D())
	}
	if got.Personality.Name != "Byte" {
		t.Errorf("personality.name is %q; the edit was overwritten", got.Personality.Name)
	}
	if !got.Pet.Sound {
		t.Error("pet.sound was overwritten, and nothing in the app can set it")
	}
	if got.Window.X != 300 || got.Window.Y != 120 {
		t.Errorf("the window position was not saved: %d,%d", got.Window.X, got.Window.Y)
	}
}

// Everything the menu and the window can change has to survive a quit.
func TestSaveOwnedWritesWhatTheAppDecides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	live := Default()
	live.Pet.Active = "peach"
	live.Pet.AlwaysOnTop = !live.Pet.AlwaysOnTop
	live.Pet.Scale = 2
	live.Pet.DropShadow = !live.Pet.DropShadow
	live.Window.X, live.Window.Y = 11, 22

	if err := SaveOwned(path, live); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settled(got).Pet, settled(live).Pet) {
		t.Errorf("pet settings did not survive:\n got %+v\nwant %+v", got.Pet, live.Pet)
	}
	if got.Window.X != 11 || got.Window.Y != 22 {
		t.Errorf("window position did not survive: %d,%d", got.Window.X, got.Window.Y)
	}
}

// The other half of the contract, and the one that catches a merge that grew:
// a live config with every field changed must move exactly five of them.
func TestSaveOwnedChangesNothingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}

	live := Default()
	live.Pet.Active, live.Pet.Scale = "peach", 2
	live.Pet.AlwaysOnTop, live.Pet.DropShadow = !live.Pet.AlwaysOnTop, !live.Pet.DropShadow
	live.Window.X, live.Window.Y = 11, 22
	// None of these may reach the file. Every one is somebody's hand edit.
	live.Pet.Sound = !live.Pet.Sound
	live.Pet.Disabled = []string{"peach"}
	live.Window.StartHidden = !live.Window.StartHidden
	live.Thresholds.IdleAfter = Duration(99 * time.Second)
	live.Personality.Name = "Nobody"
	live.Personality.Preset = "nonsense"
	live.Behavior.Dialogue = !live.Behavior.Dialogue
	live.Logging.Level = "debug"
	live.Logging.Verbose = true
	live.Server.Addr = "127.0.0.1:1"
	live.Server.AllowNonLoopback = true
	live.Update.Check = !live.Update.Check
	live.Integrations = map[string]Toggle{"claude": {Enabled: false}}

	if err := SaveOwned(path, live); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	want := Default()
	want.Pet.Active, want.Pet.Scale = "peach", 2
	want.Pet.AlwaysOnTop, want.Pet.DropShadow = live.Pet.AlwaysOnTop, live.Pet.DropShadow
	want.Window.X, want.Window.Y = 11, 22
	if !reflect.DeepEqual(settled(got), settled(want)) {
		t.Errorf("SaveOwned moved something that is not the app's to move:\n got %+v\nwant %+v", got, want)
	}
}

// The first quit on a machine that has never had a config file still has to
// leave one holding where the pet was parked.
func TestSaveOwnedCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	live := Default()
	live.Window.X, live.Window.Y = 7, 8
	if err := SaveOwned(path, live); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Window.X != 7 || got.Window.Y != 8 {
		t.Errorf("window position is %d,%d", got.Window.X, got.Window.Y)
	}
}
