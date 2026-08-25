// Package config loads ~/.config/agent-pet/config.yaml (§24).
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

	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/update"
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
	Update       Update            `yaml:"update"`
}

type Pet struct {
	Active      string  `yaml:"active"`
	AlwaysOnTop bool    `yaml:"always_on_top"`
	Scale       float64 `yaml:"scale"`
	// DropShadow is the soft shadow the UI casts behind the whole sprite. It
	// lifts her off a busy wallpaper, but on a light one it reads as a grey
	// contour tracing every edge, which there is no way to detect from here.
	//
	// It is only that shadow. The ellipse under her feet is drawn into the
	// PNG frames themselves and stays either way — hence the narrow name.
	DropShadow bool `yaml:"drop_shadow"`
	Sound      bool `yaml:"sound"`
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
	AttentionTimeout Duration `yaml:"attention_timeout"`
	SessionStale     Duration `yaml:"session_stale"`
	HappyFor         Duration `yaml:"happy_for"`
	CelebrateFor     Duration `yaml:"celebrate_for"`
	ConfusedFor      Duration `yaml:"confused_for"`
	HeartFor         Duration `yaml:"heart_for"`
	WorriedAfter     int      `yaml:"worried_after"`
}

// Update is the over-the-air update settings (ADR 0008).
//
// Nothing here makes petd reach the network — it cannot; the code to do so is
// not linked into it. These settings are read by petctl, which does the
// checking and the installing.
type Update struct {
	// Check enables the automatic check that runs from the Claude Code
	// SessionStart hook. On by default. It only ever reaches an installation
	// that has no answer of its own: the file is written back in full on every
	// quit, so anybody who turned checks off has that on disk and an upgrade
	// cannot talk them round. `petctl update --check` works either way,
	// because typing it is consent.
	Check bool `yaml:"check"`
	// ManifestURL is a template; {channel} is filled in from the build's own
	// flavor. A fork points this at its own manifest.
	//
	// There is no channel setting. Which channel a build follows is a fact
	// about the build, not a preference — following the other one means running
	// the other app (ADR 0008).
	ManifestURL string `yaml:"manifest_url"`
	// Interval is the minimum gap between automatic checks. Releases here are
	// cut by hand, so checking often would only be checking more often than
	// anything can change.
	Interval Duration `yaml:"interval"`
}

// DefaultManifestURL is where the two channels are published. It is a file in
// the repository rather than the GitHub releases API on purpose: a release can
// exist for days before anybody is offered it, because the offer is a separate
// commit. See docs/adr/0008-over-the-air-updates.md.
const DefaultManifestURL = "https://raw.githubusercontent.com/" + update.Repo + "/master/updates/{channel}.json"

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
			DropShadow:  true,
			Sound:       false,
		},
		Behavior: Behavior{Dialogue: true, XP: true, ClickFocusesAgent: false},
		Window:   Window{X: -1, Y: -1},
		Server:   Server{Addr: flavor.Current().Addr},
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
			AttentionTimeout: Duration(10 * time.Minute),
			SessionStale:     Duration(2 * time.Hour),
			HappyFor:         Duration(6 * time.Second),
			CelebrateFor:     Duration(9 * time.Second),
			ConfusedFor:      Duration(8 * time.Second),
			HeartFor:         Duration(4 * time.Second),
			WorriedAfter:     3,
		},
		Logging: Logging{Level: "info", Verbose: false},
		Update: Update{
			Check:       true,
			ManifestURL: DefaultManifestURL,
			Interval:    Duration(24 * time.Hour),
		},
	}
}

// Path returns the config file location, honouring XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("AGENT_PET_CONFIG"); p != "" {
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
	// Named after the flavor, so the dev app and the release app never edit
	// each other's settings.
	return filepath.Join(base, flavor.Current().Slug, "config.yaml")
}

// DataDir is where pets, logs and (from Milestone 4) the database live.
func DataDir() string {
	if p := os.Getenv("AGENT_PET_DATA"); p != "" {
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
	return filepath.Join(base, flavor.Current().Slug)
}

func PetsDir() string { return filepath.Join(DataDir(), "pets") }
func LogsDir() string { return filepath.Join(DataDir(), "logs") }

// UpdateStamp is when the automatic update check last ran, recorded as a file's
// modification time.
//
// A file rather than a config key, because config.yaml is rewritten from memory
// when the app exits: a timestamp stored there would be lost or resurrected
// depending on when the pet happened to quit. Both petctl (which writes it) and
// the desktop shell (which deletes it when the channel changes) need the path,
// so it lives here rather than in either of them.
func UpdateStamp() string { return filepath.Join(DataDir(), "update-check") }

// UpdateResult is what the last check found, kept beside the stamp.
//
// A file for the same reason the stamp is one: config.yaml is rewritten from
// memory when the app exits. Separate from the stamp because that file's
// *mtime* is the throttle and giving it contents as well would mean one file
// meaning two things.
//
// It exists because petd holds a check result only in memory, and an update is
// a restart — so without this, the state right after updating is always "no
// update check yet", which is the one moment somebody is certain to look.
func UpdateResult() string { return filepath.Join(DataDir(), "update-result.json") }

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
		c.Server.Addr = flavor.Current().Addr
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
	fix(&c.Thresholds.AttentionTimeout, d.AttentionTimeout)
	fix(&c.Thresholds.SessionStale, d.SessionStale)
	fix(&c.Thresholds.HappyFor, d.HappyFor)
	fix(&c.Thresholds.CelebrateFor, d.CelebrateFor)
	fix(&c.Thresholds.ConfusedFor, d.ConfusedFor)
	fix(&c.Thresholds.HeartFor, d.HeartFor)

	if strings.TrimSpace(c.Update.ManifestURL) == "" {
		c.Update.ManifestURL = DefaultManifestURL
	}
	if c.Update.Interval <= 0 {
		c.Update.Interval = Default().Update.Interval
	}
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
