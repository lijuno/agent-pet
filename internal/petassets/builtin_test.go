package petassets

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/lijuno/agent-pet/internal/state"
)

// builtinDir is the directory the binary embeds. Reading it from disk rather
// than through the embed is deliberate: the embed is what ships, but a pack
// half-written by tools/genpets fails here first, at `make test`, instead of
// as a pet that renders one frame of a ten-frame strip.
const builtinDir = "../../ui/dist/pets"

// TestBuiltinPacksAreComplete guards the generated art, not the loader. Every
// pack that ships has to answer for all ten states on its own: Resolve falls
// back so an incomplete third-party pack still renders (ADR 0003), and that
// fallback would quietly hide a state genpets stopped emitting.
func TestBuiltinPacksAreComplete(t *testing.T) {
	l := NewLibrary()
	if err := l.LoadDir(builtinDir, "/pets"); err != nil {
		t.Fatalf("load %s: %v", builtinDir, err)
	}
	if l.Len() == 0 {
		t.Fatalf("no built-in packs in %s; run `make pets`", builtinDir)
	}

	for _, p := range l.List() {
		t.Run(p.ID, func(t *testing.T) {
			if missing := p.Missing(); len(missing) > 0 {
				t.Errorf("missing states %v", missing)
			}
			for _, s := range state.All() {
				a, ok := p.Animations[s]
				if !ok {
					continue // already reported by Missing
				}
				w, h, err := pngSize(filepath.Join(p.Dir, a.File))
				if err != nil {
					t.Errorf("%s: %v", s, err)
					continue
				}
				// A strip is N frames laid out left to right. Get this wrong
				// and the UI shows a sliver of each frame rather than nothing,
				// which is much harder to spot than a missing file.
				if want := p.FrameWidth * a.Frames; w != want {
					t.Errorf("%s: strip is %dpx wide, want %d (%d frames x %d)",
						s, w, want, a.Frames, p.FrameWidth)
				}
				if h != p.FrameHeight {
					t.Errorf("%s: strip is %dpx tall, want %d", s, h, p.FrameHeight)
				}
			}
		})
	}
}

// pngSize reads the IHDR of a PNG. Decoding the pixels would pull image/png in
// for two integers.
func pngSize(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var hdr [24]byte
	if _, err := f.Read(hdr[:]); err != nil {
		return 0, 0, err
	}
	if string(hdr[1:4]) != "PNG" {
		return 0, 0, os.ErrInvalid
	}
	return int(binary.BigEndian.Uint32(hdr[16:20])), int(binary.BigEndian.Uint32(hdr[20:24])), nil
}
