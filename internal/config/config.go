// Package config loads ~/.config/digital-pet/config.yaml (§24).
//
// Loading never fails hard: a missing file yields defaults and is written back,
// and a malformed file yields defaults plus a warning. A pet that refuses to
// start because of a typo in a YAML file is a bad pet.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Pet          Pet               `yaml:"pet"`
	Behavior     Behavior          `yaml:"behavior"`
	Window       Window            `yaml:"window"`
	Server       Server            `yaml:"server"`
	Integrations map[string]Toggle `yaml:"integrations"`
	Personality  Personality       `yaml:"personality"`
	Thresholds   Thresholds        `yaml:"thresholds"`
	Logging      Logging           `yaml:"logging"`
}

type Pet struct {
	Active      string  `yaml:"active"`
	AlwaysOnTop bool    `yaml:"always_on_top"`
	Scale       float64 `yaml:"scale"`
	Sound       bool    `yaml:"sound"`
}

type Behavior struct {
	Dialogue bool `yaml:"dialogue"`
	XP       bool `yaml:"xp"`
	// ClickFocusesAgent: clicking an attention-state pet raises the terminal.
	// Not implemented in Milestone 1; the flag exists so the config file is
	// stable across versions.
	ClickFocusesAgent bool `yaml:"click_focuses_agent"`
}

type Window struct {
	// X and Y are the last window position, persisted so the pet reappears
	// where the user left it. -1 means "let the OS decide".
	X           int  `yaml:"x"`
	Y           int  `yaml:"y"`
	StartHidden bool `yaml:"start_hidden"`
}

type Server struct {
	Addr string `yaml:"addr"`
	// AllowNonLoopback must be set explicitly to bind anything but 127.0.0.1.
	// See ADR 0002 and §26.
	AllowNonLoopback bool `yaml:"allow_non_loopback"`
}

type Toggle struct {
	Enabled bool `yaml:"enabled"`
}

type Personality struct {
	Name      string `yaml:"name"`
	Preset    string `yaml:"personality"`
	Energy    int    `yaml:"energy"`
	Curiosity int    `yaml:"curiosity"`
	Snark     int    `yaml:"snark"`
	Patience  int    `yaml:"patience"`
}

// Thresholds mirrors §20 and §21. Values are Go durations ("30s", "5m", "2h").
type Thresholds struct {
	IdleAfter        Duration `yaml:"idle_after"`
	SleepingAfter    Duration `yaml:"sleeping_after"`
	TiredAfter       Duration `yaml:"tired_after"`
	AttentionTimeout Duration `yaml:"attention_timeout"`
	SessionStale     Duration `yaml:"session_stale"`
	HappyFor         Duration `yaml:"happy_for"`
	CelebrateFor     Duration `yaml:"celebrate_for"`
	ConfusedFor      Duration `yaml:"confused_for"`
	HeartFor         Duration `yaml:"heart_for"`
	WorriedAfter     int      `yaml:"worried_after"`
}

type Logging struct {
	Level string `yaml:"level"`
	// Verbose enables logging of event metadata (tool names, messages).
	// Off by default: §32 says default logs contain event categories only.
	Verbose bool `yaml:"verbose"`
}

// Duration is a time.Duration that marshals as a human string in YAML.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	// Accept the "30m" style from the requirements as well as "1h30m".
	v, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

func Default() Config {
	return Config{
		Pet: Pet{
			Active:      "momo",
			AlwaysOnTop: true,
			Scale:       1.0,
			Sound:       false,
		},
		Behavior: Behavior{Dialogue: true, XP: true, ClickFocusesAgent: false},
		Window:   Window{X: -1, Y: -1},
		Server:   Server{Addr: "127.0.0.1:9876"},
		Integrations: map[string]Toggle{
			"claude": {Enabled: true},
			"codex":  {Enabled: true},
			"git":    {Enabled: false},
		},
		Personality: Personality{
			Name: "SanMao (三毛)", Preset: "gentle",
			Energy: 65, Curiosity: 80, Snark: 10, Patience: 90,
		},
		Thresholds: Thresholds{
			IdleAfter:        Duration(30 * time.Second),
			SleepingAfter:    Duration(60 * time.Second),
			TiredAfter:       Duration(120 * time.Minute),
			AttentionTimeout: Duration(10 * time.Minute),
			SessionStale:     Duration(2 * time.Hour),
			HappyFor:         Duration(6 * time.Second),
			CelebrateFor:     Duration(9 * time.Second),
			ConfusedFor:      Duration(8 * time.Second),
			HeartFor:         Duration(4 * time.Second),
			WorriedAfter:     3,
		},
		Logging: Logging{Level: "info", Verbose: false},
	}
}

// Path returns the config file location, honouring XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("DIGITAL_PET_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "digital-pet", "config.yaml")
}

// DataDir is where pets, logs and (from Milestone 4) the database live.
func DataDir() string {
	if p := os.Getenv("DIGITAL_PET_DATA"); p != "" {
		return p
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "data"
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "digital-pet")
}

func PetsDir() string { return filepath.Join(DataDir(), "pets") }
func LogsDir() string { return filepath.Join(DataDir(), "logs") }

// Load reads the config, filling in defaults for anything absent. The returned
// error is advisory: cfg is always usable.
func Load(path string) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, Save(path, cfg)
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	// Decoding into the defaults means a partial file only overrides the keys
	// it actually contains.
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Default(), fmt.Errorf("parse %s: %w (using defaults)", path, err)
	}
	return cfg.sanitised(), nil
}

func (c Config) sanitised() Config {
	if c.Pet.Scale <= 0 || c.Pet.Scale > 6 {
		c.Pet.Scale = 1.0
	}
	if c.Pet.Active == "" {
		c.Pet.Active = "momo"
	}
	if c.Server.Addr == "" {
		c.Server.Addr = "127.0.0.1:9876"
	}
	if c.Thresholds.WorriedAfter <= 0 {
		c.Thresholds.WorriedAfter = 3
	}
	d := Default().Thresholds
	fix := func(v *Duration, def Duration) {
		if *v <= 0 {
			*v = def
		}
	}
	fix(&c.Thresholds.IdleAfter, d.IdleAfter)
	fix(&c.Thresholds.SleepingAfter, d.SleepingAfter)
	fix(&c.Thresholds.TiredAfter, d.TiredAfter)
	fix(&c.Thresholds.AttentionTimeout, d.AttentionTimeout)
	fix(&c.Thresholds.SessionStale, d.SessionStale)
	fix(&c.Thresholds.HappyFor, d.HappyFor)
	fix(&c.Thresholds.CelebrateFor, d.CelebrateFor)
	fix(&c.Thresholds.ConfusedFor, d.ConfusedFor)
	fix(&c.Thresholds.HeartFor, d.HeartFor)
	return c
}

// Save writes the config atomically, creating parent directories.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// IsLoopback reports whether addr is safe to bind without an explicit override.
func IsLoopback(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "", "127.0.0.1", "localhost", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}
