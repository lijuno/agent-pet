// Package petassets loads pet asset packs (§11) from the embedded built-ins and
// from the user's pets directory. It never contacts a network service: after a
// pack exists on disk, animating the pet is pure local file reading (§12).
package petassets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lijuno/agent-pet/internal/state"
)

// Animation describes one state's sprite strip: N frames of FrameWidth ×
// FrameHeight laid out left to right in a single image.
type Animation struct {
	File   string  `json:"file"`
	Frames int     `json:"frames"`
	FPS    float64 `json:"fps"`
	Loop   bool    `json:"loop"`
}

// UnmarshalJSON accepts either the object form or the bare filename form used
// in §11's example manifest, so hand-written packs following the spec load
// without modification.
func (a *Animation) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = Animation{File: s, Frames: 1, FPS: 1, Loop: true}
		return nil
	}
	type raw Animation
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*a = Animation(r)
	if a.Frames <= 0 {
		a.Frames = 1
	}
	if a.FPS <= 0 {
		a.FPS = 4
	}
	return nil
}

// Manifest is manifest.json.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     int    `json:"version"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	FrameWidth  int    `json:"frame_width"`
	FrameHeight int    `json:"frame_height"`
	// Scale is the pack's preferred pixel multiplier. Pixel-art packs want an
	// integer scale so pixels stay square.
	Scale      float64                   `json:"scale"`
	Pixelated  bool                      `json:"pixelated"`
	Animations map[state.State]Animation `json:"animations"`
}

// Pet is a loaded pack plus where it came from.
type Pet struct {
	Manifest
	// Dir is the on-disk directory for user packs, empty for built-ins.
	Dir string `json:"dir,omitempty"`
	// Builtin packs are embedded in the binary and cannot be deleted.
	Builtin bool `json:"builtin"`
	// BaseURL is the prefix the frontend uses to fetch frames.
	BaseURL string `json:"base_url"`
}

// Resolve returns the animation to play for a state, walking the fallback chain
// (ADR 0003) so an incomplete pack still renders. The returned state is the one
// actually used, which the UI shows in debug mode.
func (p Pet) Resolve(s state.State) (state.State, Animation, bool) {
	if a, ok := p.Animations[s]; ok && a.File != "" {
		return s, a, true
	}
	for _, alt := range state.Fallback[s] {
		if a, ok := p.Animations[alt]; ok && a.File != "" {
			return alt, a, true
		}
	}
	if a, ok := p.Animations[state.Idle]; ok && a.File != "" {
		return state.Idle, a, true
	}
	return s, Animation{}, false
}

// Missing lists the states this pack does not provide.
func (p Pet) Missing() []state.State {
	var out []state.State
	for _, s := range state.All() {
		if a, ok := p.Animations[s]; !ok || a.File == "" {
			out = append(out, s)
		}
	}
	return out
}

// Library holds every pet available to the runtime.
type Library struct {
	pets map[string]Pet
}

func NewLibrary() *Library { return &Library{pets: map[string]Pet{}} }

// LoadBuiltin reads packs out of an embedded filesystem. root is the directory
// holding one subdirectory per pet.
func (l *Library) LoadBuiltin(fsys fs.FS, root, urlPrefix string) error {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return fmt.Errorf("read builtin pets: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, path(root, e.Name(), "manifest.json"))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		p, err := parse(data, e.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		p.Builtin = true
		p.BaseURL = strings.TrimRight(urlPrefix, "/") + "/" + p.ID + "/"
		l.pets[p.ID] = p
	}
	return errors.Join(errs...)
}

// LoadDir reads user packs from disk. Disk packs shadow built-ins with the same
// id, which is how a user replaces a shipped pet with their own version.
// A missing directory is not an error: most users will never have one.
func (l *Library) LoadDir(dir, urlPrefix string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pets dir: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(filepath.Join(full, "manifest.json"))
		if err != nil {
			// A stray directory in the pets folder is not worth an error.
			continue
		}
		p, err := parse(data, e.Name())
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		p.Dir = full
		p.Builtin = false
		p.BaseURL = strings.TrimRight(urlPrefix, "/") + "/" + p.ID + "/"
		l.pets[p.ID] = p
	}
	return errors.Join(errs...)
}

func (l *Library) Get(id string) (Pet, bool) {
	p, ok := l.pets[id]
	return p, ok
}

func (l *Library) List() []Pet {
	out := make([]Pet, 0, len(l.pets))
	for _, p := range l.pets {
		out = append(out, p)
	}
	// By the name the user sees, not by the id. Every surface that shows this
	// list — the Change Pet submenu, the panel in the window, `petctl pets` —
	// shows names, and an id may differ from its name by more than case. One
	// shipped pack did: `momo` displayed as Sanmao, and sorted by id the menu
	// read Juanmao, Sanmao, Peach — alphabetical in a column nobody can see,
	// and therefore random in the one they can. That pack has since been
	// renamed, but nothing stops a user's own pack from doing the same.
	//
	// Id breaks the tie, so the order cannot depend on map iteration.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		if !strings.EqualFold(out[i].Name, out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (l *Library) Len() int { return len(l.pets) }

// Any returns a usable pet: the requested one, or the first available.
func (l *Library) Any(preferred string) (Pet, bool) {
	if p, ok := l.Get(preferred); ok {
		return p, true
	}
	list := l.List()
	if len(list) == 0 {
		return Pet{}, false
	}
	return list[0], true
}

func parse(data []byte, dirName string) (Pet, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Pet{}, fmt.Errorf("manifest: %w", err)
	}
	if m.ID == "" {
		m.ID = dirName
	}
	if err := validateID(m.ID); err != nil {
		return Pet{}, err
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	if m.FrameWidth <= 0 {
		m.FrameWidth = 48
	}
	if m.FrameHeight <= 0 {
		m.FrameHeight = 48
	}
	if m.Scale <= 0 {
		m.Scale = 3
	}
	if len(m.Animations) == 0 {
		return Pet{}, errors.New("manifest declares no animations")
	}
	for s, a := range m.Animations {
		// A manifest is user-supplied data. A file name that escapes the pack
		// directory must not be reachable (§26 treats external input as untrusted).
		if err := validateFile(a.File); err != nil {
			return Pet{}, fmt.Errorf("animation %q: %w", s, err)
		}
	}
	return Pet{Manifest: m}, nil
}

func validateID(id string) error {
	if id == "" || len(id) > 48 {
		return errors.New("pet id must be 1-48 characters")
	}
	for _, r := range id {
		ok := r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("pet id %q contains %q; use letters, digits, - and _ only", id, r)
		}
	}
	return nil
}

func validateFile(name string) error {
	if name == "" {
		return errors.New("empty file")
	}
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("file %q must be a plain name inside the pack directory", name)
	}
	return nil
}

func path(parts ...string) string { return strings.Join(parts, "/") }
