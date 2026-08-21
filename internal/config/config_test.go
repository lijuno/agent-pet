package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// The app stored its config and pet packs under "digital-pet" before it was
// renamed. An upgrade that does not carry those across starts from defaults
// and loses the user's packs — and since the config is rewritten from memory
// on shutdown, the old file would be gone before anyone noticed.
func TestMigrateLegacyMovesThePreRenameDirectories(t *testing.T) {
	cfgBase, dataBase := legacyEnv(t)

	os.MkdirAll(filepath.Join(cfgBase, "digital-pet"), 0o755)
	os.WriteFile(filepath.Join(cfgBase, "digital-pet", "config.yaml"),
		[]byte("pet:\n  active: byte\n"), 0o644)
	os.MkdirAll(filepath.Join(dataBase, "digital-pet", "pets", "mine"), 0o755)

	if errs := MigrateLegacy(); len(errs) != 0 {
		t.Fatalf("migrate: %v", errs)
	}

	cfg, err := Load(Path())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Pet.Active != "byte" {
		t.Fatalf("the old config should be readable at the new path, got %q", cfg.Pet.Active)
	}
	if _, err := os.Stat(filepath.Join(PetsDir(), "mine")); err != nil {
		t.Fatalf("the user's pet pack should have come across: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfgBase, "digital-pet")); err == nil {
		t.Fatal("the old directory should be gone, not copied")
	}
}

// Both directories present means the migration already happened, or the user
// has both installs. Either way it is a state to leave alone and not complain
// about: the rename would fail anyway, and reporting that as an error puts a
// warning in the log on every single start.
func TestMigrateLegacyIsQuietWhenBothExist(t *testing.T) {
	cfgBase, _ := legacyEnv(t)

	os.MkdirAll(filepath.Join(cfgBase, "digital-pet"), 0o755)
	os.WriteFile(filepath.Join(cfgBase, "digital-pet", "config.yaml"),
		[]byte("pet:\n  active: old\n"), 0o644)
	os.MkdirAll(filepath.Join(cfgBase, "agent-pet"), 0o755)
	os.WriteFile(filepath.Join(cfgBase, "agent-pet", "config.yaml"),
		[]byte("pet:\n  active: current\n"), 0o644)

	if errs := MigrateLegacy(); len(errs) != 0 {
		t.Fatalf("already-migrated is not a failure: %v", errs)
	}

	cfg, _ := Load(Path())
	if cfg.Pet.Active != "current" {
		t.Fatalf("the current config should have been left alone, got %q", cfg.Pet.Active)
	}
}

// The same when the destination is empty, which is easy to assume rename would
// simply absorb. It does not — it refuses that too — so without the guard a
// leftover digital-pet is a warning on every start rather than a no-op, and
// its stale packs stay invisible either way.
func TestMigrateLegacyLeavesAnEmptyDestinationAlone(t *testing.T) {
	_, dataBase := legacyEnv(t)

	os.MkdirAll(filepath.Join(dataBase, "digital-pet", "pets", "stale"), 0o755)
	os.MkdirAll(filepath.Join(dataBase, "agent-pet"), 0o755)

	if errs := MigrateLegacy(); len(errs) != 0 {
		t.Fatalf("migrate: %v", errs)
	}

	if _, err := os.Stat(filepath.Join(PetsDir(), "stale")); err == nil {
		t.Fatal("the old directory should not have replaced the one in use")
	}
}

// Someone who pointed the app at a directory of their own did not ask for the
// default one to be moved underneath them.
func TestMigrateLegacyLeavesOverriddenLocationsAlone(t *testing.T) {
	cfgBase, dataBase := legacyEnv(t)
	t.Setenv("AGENT_PET_CONFIG", filepath.Join(t.TempDir(), "elsewhere.yaml"))
	t.Setenv("AGENT_PET_DATA", t.TempDir())

	os.MkdirAll(filepath.Join(cfgBase, "digital-pet"), 0o755)
	os.MkdirAll(filepath.Join(dataBase, "digital-pet"), 0o755)

	MigrateLegacy()

	for _, base := range []string{cfgBase, dataBase} {
		if _, err := os.Stat(filepath.Join(base, "digital-pet")); err != nil {
			t.Fatalf("%s should still be there: %v", base, err)
		}
	}
}

// The variables were DIGITAL_PET_* before the rename. Ignoring them would send
// anyone with one exported to the default location without saying so.
func TestEnvAcceptsThePreRenameVariables(t *testing.T) {
	legacyEnv(t)
	old := filepath.Join(t.TempDir(), "old.yaml")
	t.Setenv("DIGITAL_PET_CONFIG", old)
	if got := Path(); got != old {
		t.Fatalf("the old variable should still be honoured, got %q", got)
	}

	current := filepath.Join(t.TempDir(), "current.yaml")
	t.Setenv("AGENT_PET_CONFIG", current)
	if got := Path(); got != current {
		t.Fatalf("the current variable should win, got %q", got)
	}
}

// Points the config and data directories at temporary ones and clears every
// override, so a variable in the developer's own shell cannot decide the
// result of a test about which directory is chosen.
func legacyEnv(t *testing.T) (cfgBase, dataBase string) {
	t.Helper()
	cfgBase, dataBase = t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgBase)
	t.Setenv("XDG_DATA_HOME", dataBase)
	for _, k := range []string{
		"AGENT_PET_CONFIG", "AGENT_PET_DATA", "DIGITAL_PET_CONFIG", "DIGITAL_PET_DATA",
	} {
		t.Setenv(k, "")
	}
	return cfgBase, dataBase
}
