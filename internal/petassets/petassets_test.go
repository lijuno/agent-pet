package petassets

import (
	"testing"
	"testing/fstest"

	"github.com/lijuno/agent-pet/internal/state"
)

const fullManifest = `{
  "id": "sanmao", "name": "Sanmao", "version": 1,
  "frame_width": 40, "frame_height": 40, "scale": 3, "pixelated": true,
  "animations": {
    "idle":      {"file": "idle.png", "frames": 4, "fps": 3, "loop": true},
    "working":   {"file": "working.png", "frames": 4, "fps": 8, "loop": true},
    "celebrate": {"file": "celebrate.png", "frames": 6, "fps": 10, "loop": true}
  }
}`

// The manifest shape in §11 of the requirements uses bare filenames. A pack
// written against the document must load without modification.
const specStyleManifest = `{
  "id": "specpet", "name": "Spec Pet", "version": 1,
  "animations": { "idle": "idle.webp", "happy": "happy.webp" }
}`

func loadOne(t *testing.T, manifest string) Pet {
	t.Helper()
	fsys := fstest.MapFS{
		"pets/thepet/manifest.json": {Data: []byte(manifest)},
	}
	l := NewLibrary()
	if err := l.LoadBuiltin(fsys, "pets", "/pets"); err != nil {
		t.Fatalf("load: %v", err)
	}
	list := l.List()
	if len(list) != 1 {
		t.Fatalf("want 1 pet, got %d", len(list))
	}
	return list[0]
}

func TestLoadObjectManifest(t *testing.T) {
	p := loadOne(t, fullManifest)
	if p.ID != "sanmao" || p.Name != "Sanmao" || !p.Builtin {
		t.Fatalf("unexpected pet: %+v", p.Manifest)
	}
	if p.BaseURL != "/pets/sanmao/" {
		t.Fatalf("base url %q", p.BaseURL)
	}
	a := p.Animations[state.Working]
	if a.Frames != 4 || a.FPS != 8 || !a.Loop {
		t.Fatalf("animation not parsed: %+v", a)
	}
}

func TestLoadSpecStyleManifest(t *testing.T) {
	p := loadOne(t, specStyleManifest)
	a, ok := p.Animations[state.Idle]
	if !ok {
		t.Fatal("idle missing")
	}
	if a.File != "idle.webp" || a.Frames != 1 {
		t.Fatalf("bare filename should mean a single frame, got %+v", a)
	}
	if p.FrameWidth == 0 || p.Scale == 0 {
		t.Fatalf("defaults should be filled in, got %+v", p.Manifest)
	}
}

func TestFallbackChain(t *testing.T) {
	p := loadOne(t, fullManifest)

	// thinking is absent; the chain is thinking -> working -> idle.
	resolved, anim, ok := p.Resolve(state.Thinking)
	if !ok || resolved != state.Working || anim.File != "working.png" {
		t.Fatalf("want working as the fallback for thinking, got %s/%v", resolved, anim)
	}

	// heart -> happy -> idle; happy is absent too, so it lands on idle.
	resolved, _, ok = p.Resolve(state.Heart)
	if !ok || resolved != state.Idle {
		t.Fatalf("want idle for heart, got %s", resolved)
	}

	// a state the pack does provide resolves to itself
	resolved, _, _ = p.Resolve(state.Celebrate)
	if resolved != state.Celebrate {
		t.Fatalf("celebrate should resolve to itself, got %s", resolved)
	}
}

func TestMissingLists(t *testing.T) {
	p := loadOne(t, fullManifest)
	missing := p.Missing()
	if len(missing) != len(state.All())-3 {
		t.Fatalf("want %d missing states, got %d (%v)", len(state.All())-3, len(missing), missing)
	}
}

// A manifest is user-supplied. A filename that escapes the pack directory must
// be refused at load time, not at serve time.
func TestPathTraversalRejected(t *testing.T) {
	for _, bad := range []string{
		`{"id":"x","animations":{"idle":"../../../etc/passwd"}}`,
		`{"id":"x","animations":{"idle":"/etc/passwd"}}`,
		`{"id":"x","animations":{"idle":"sub/dir.png"}}`,
		`{"id":"x","animations":{"idle":"..\\windows\\system32"}}`,
	} {
		fsys := fstest.MapFS{"pets/x/manifest.json": {Data: []byte(bad)}}
		l := NewLibrary()
		if err := l.LoadBuiltin(fsys, "pets", "/pets"); err == nil {
			t.Fatalf("expected rejection of %s", bad)
		}
		if l.Len() != 0 {
			t.Fatalf("the pack should not have loaded: %s", bad)
		}
	}
}

func TestBadIDRejected(t *testing.T) {
	fsys := fstest.MapFS{
		"pets/x/manifest.json": {Data: []byte(`{"id":"../evil","animations":{"idle":"a.png"}}`)},
	}
	l := NewLibrary()
	if err := l.LoadBuiltin(fsys, "pets", "/pets"); err == nil {
		t.Fatal("expected a bad pet id to be rejected")
	}
}

func TestEmptyAnimationsRejected(t *testing.T) {
	fsys := fstest.MapFS{"pets/x/manifest.json": {Data: []byte(`{"id":"x","animations":{}}`)}}
	l := NewLibrary()
	if err := l.LoadBuiltin(fsys, "pets", "/pets"); err == nil {
		t.Fatal("a pack with no animations is not usable and should be rejected")
	}
}

func TestOneBadPackDoesNotBlockTheOthers(t *testing.T) {
	fsys := fstest.MapFS{
		"pets/good/manifest.json": {Data: []byte(fullManifest)},
		"pets/bad/manifest.json":  {Data: []byte(`{ not json`)},
	}
	l := NewLibrary()
	err := l.LoadBuiltin(fsys, "pets", "/pets")
	if err == nil {
		t.Fatal("the broken pack should be reported")
	}
	if l.Len() != 1 {
		t.Fatalf("the good pack should still load, got %d", l.Len())
	}
}

func TestAnyFallsBackToFirstAvailable(t *testing.T) {
	fsys := fstest.MapFS{"pets/good/manifest.json": {Data: []byte(fullManifest)}}
	l := NewLibrary()
	_ = l.LoadBuiltin(fsys, "pets", "/pets")

	p, ok := l.Any("does-not-exist")
	if !ok || p.ID != "sanmao" {
		t.Fatalf("a missing configured pet should fall back to an available one, got %v/%v", p.ID, ok)
	}
}

func TestMissingUserPetsDirIsNotAnError(t *testing.T) {
	l := NewLibrary()
	if err := l.LoadDir("/nonexistent/path/to/pets", "/userpets"); err != nil {
		t.Fatalf("most users have no pets directory; that must not be an error: %v", err)
	}
}

// TestListOrdersByDisplayedName pins the order of the Change Pet submenu and
// the panel beside it. Both show names; the ids they sort by are invisible, so
// an order that follows the id looks random on screen. Sanmao is where that was
// noticed — her id was `momo` — and the fixture here keeps a pack whose id and
// name disagree even though no shipped pack does any more, because nothing
// stops a user's own pack from disagreeing.
func TestListOrdersByDisplayedName(t *testing.T) {
	pack := func(id, name string) string {
		return `{"id": "` + id + `", "name": "` + name + `",
		         "animations": {"idle": "idle.png"}}`
	}
	fsys := fstest.MapFS{
		"pets/a/manifest.json": {Data: []byte(pack("zulu", "Alpha"))},
		"pets/b/manifest.json": {Data: []byte(pack("mike", "Mike"))},
		"pets/c/manifest.json": {Data: []byte(pack("alpha", "Zulu"))},
	}
	l := NewLibrary()
	if err := l.LoadBuiltin(fsys, "pets", "/pets"); err != nil {
		t.Fatalf("load: %v", err)
	}

	var got []string
	for _, p := range l.List() {
		got = append(got, p.ID)
	}
	// Exactly the reverse of the id order, so a sort that slipped back to the
	// id could not pass by coincidence.
	want := []string{"zulu", "mike", "alpha"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("order by name: got %v, want %v", got, want)
		}
	}
}
