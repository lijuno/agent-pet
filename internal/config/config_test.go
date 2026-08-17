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
	if cfg.Thresholds.SleepingAfter.D() != 30*time.Minute {
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
