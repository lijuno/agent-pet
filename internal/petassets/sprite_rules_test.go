package petassets

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The sprite rules.
//
// Five characters in, every one has arrived with a fault that only a pair of
// eyes caught, and two of them shipped: a blush drawn with an alpha punched
// translucent holes in every face for months, and a running character's hands
// were never drawn at all because they sat in a branch that skips them for
// every `working` state. Neither is a matter of taste. Both are properties of
// the pixels, and anything that is a property of the pixels can be checked
// here instead of by looking.
//
// These run against ui/dist/pets, which is what genpets writes and what the
// binary embeds — so a rule broken by a change to the generator fails at
// `make test` rather than in the window.

// propX is the left edge of the column the props live in. Anything drawn there
// belongs to the state rather than to the character: the Z of `sleeping` rises
// out of the top of the frame on purpose, and a rule about the character being
// cut off must not fire on it.
const propX = 27

type strip struct {
	pet, state string
	img        image.Image
	w, h, n    int
}

// loadStrips reads every frame of every state of every shipped pack.
func loadStrips(t *testing.T) []strip {
	t.Helper()
	dirs, err := os.ReadDir(builtinDir)
	if err != nil {
		t.Fatalf("read %s: %v", builtinDir, err)
	}
	var out []strip
	for _, e := range dirs {
		if !e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(builtinDir, e.Name(), "manifest.json"))
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		for st, a := range m.Animations {
			f, err := os.Open(filepath.Join(builtinDir, e.Name(), a.File))
			if err != nil {
				t.Fatalf("%s/%s: %v", e.Name(), a.File, err)
			}
			img, err := png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatalf("%s/%s: %v", e.Name(), a.File, err)
			}
			out = append(out, strip{e.Name(), string(st), img, m.FrameWidth, m.FrameHeight, a.Frames})
		}
	}
	if len(out) == 0 {
		t.Fatal("no packs to check; run `make pets`")
	}
	return out
}

// alphaAt returns the alpha of one pixel of one frame, 0-255.
func (s strip) alphaAt(frame, x, y int) int {
	_, _, _, a := s.img.At(frame*s.w+x, y).RGBA()
	return int(a >> 8)
}

// TestSpritesHaveNoHoles is the blush rule.
//
// ImageDraw replaces pixels rather than compositing, so a colour carrying an
// alpha does not tint what is under it — it makes the sprite that transparent
// in that shape. The window is transparent, so what shows through is the
// wallpaper: a cheek-shaped piece of desktop, brown on a dark one. It looked
// enough like shading on four faces to survive for months, and like tears on
// the fifth.
//
// A partial pixel is legitimate where there is nothing behind it but the
// desktop — the shadow on the floor, a deliberately faint prop. It is a hole
// when the character is behind it, which is what having opaque pixels both
// above and below in the same column means.
func TestSpritesHaveNoHoles(t *testing.T) {
	for _, s := range loadStrips(t) {
		for i := 0; i < s.n; i++ {
			for x := 0; x < s.w; x++ {
				for y := 0; y < s.h; y++ {
					a := s.alphaAt(i, x, y)
					if a == 0 || a == 255 {
						continue
					}
					above, below := false, false
					for yy := 0; yy < y; yy++ {
						above = above || s.alphaAt(i, x, yy) == 255
					}
					for yy := y + 1; yy < s.h; yy++ {
						below = below || s.alphaAt(i, x, yy) == 255
					}
					if above && below {
						t.Errorf("%s/%s frame %d: hole in the sprite at %d,%d (alpha %d) — "+
							"a colour with an alpha does not tint what is under it, it makes "+
							"the character that transparent and the desktop shows through",
							s.pet, s.state, i, x, y, a)
						return
					}
				}
			}
		}
	}
}

// TestSpritesAnimate catches a strip whose frames are all the same image: a
// state that claims four frames and moves in none of them.
func TestSpritesAnimate(t *testing.T) {
	for _, s := range loadStrips(t) {
		if s.n < 2 {
			continue
		}
		same := true
		for i := 1; i < s.n && same; i++ {
			for x := 0; x < s.w && same; x++ {
				for y := 0; y < s.h && same; y++ {
					r1, g1, b1, a1 := s.img.At(x, y).RGBA()
					r2, g2, b2, a2 := s.img.At(i*s.w+x, y).RGBA()
					if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
						same = false
					}
				}
			}
		}
		if same {
			t.Errorf("%s/%s: every one of its %d frames is the same image", s.pet, s.state, s.n)
		}
	}
}

// TestSpritesAreNotEmpty catches a state that failed to draw a character at
// all — a branch that fell through, a species field spelled wrong. The floor
// is well under the smallest real frame and is only there to catch nothing.
func TestSpritesAreNotEmpty(t *testing.T) {
	const floor = 200
	for _, s := range loadStrips(t) {
		for i := 0; i < s.n; i++ {
			drawn := 0
			for x := 0; x < s.w; x++ {
				for y := 0; y < s.h; y++ {
					if s.alphaAt(i, x, y) > 0 {
						drawn++
					}
				}
			}
			if drawn < floor {
				t.Errorf("%s/%s frame %d: only %d pixels drawn, want at least %d",
					s.pet, s.state, i, drawn, floor)
			}
		}
	}
}

// clippedAtTop is what each state is currently allowed to lose off the top of
// the window, counted in character pixels on row 0 of its worst frame.
//
// None of these is deliberate. They are hair, sheared flat by the top of the
// frame when a bob lifts the character, and they were found by writing this
// rule rather than by looking — Peach and Juanmao have shipped like this since
// they were drawn. The rule earns its place by stopping the list growing: a
// new character starts at zero, which is how the same fault was caught in
// Damao's running state the day it was written.
var clippedAtTop = map[string]int{
	"damao/attention":   9,
	"damao/celebrate":   18,
	"juanmao/attention": 15,
	"juanmao/celebrate": 17,
	"juanmao/happy":     9,
	"juanmao/working":   15,
	"peach/attention":   13,
	"peach/celebrate":   15,
	"peach/happy":       7,
}

func TestSpritesStayInTheWindow(t *testing.T) {
	for _, s := range loadStrips(t) {
		worst := 0
		for i := 0; i < s.n; i++ {
			n := 0
			for x := 0; x < propX; x++ {
				if s.alphaAt(i, x, 0) > 0 {
					n++
				}
			}
			if n > worst {
				worst = n
			}
		}
		allowed := clippedAtTop[s.pet+"/"+s.state]
		if worst > allowed {
			t.Errorf("%s/%s: %d character pixels on the top row of the window, allowed %d — "+
				"something is being cut off. Lower the character or make the hair shorter; "+
				"do not raise the allowance.", s.pet, s.state, worst, allowed)
		}
		if worst < allowed {
			t.Errorf("%s/%s: clips %d now, and the allowance still says %d. "+
				"It got better: lower the allowance to %d.", s.pet, s.state, worst, allowed, worst)
		}
	}
}
