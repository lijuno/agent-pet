package flavor

import (
	"reflect"
	"strings"
	"testing"
)

// The test this package exists for.
//
// Two apps installed side by side collide on anything they share, and each
// collision fails in a way that looks like something else: a shared port means
// the second app exits at startup and the user's rebuild appears to do nothing,
// a shared data directory means two pets writing one config, a shared bundle
// identifier means macOS treats them as the same application.
//
// Every field here must be unique across flavors. Adding a flavor without
// giving it its own values fails here rather than in somebody's menu bar.
func TestFlavoursDoNotCollide(t *testing.T) {
	fields := []struct {
		name string
		get  func(Flavor) string
	}{
		{"ID", func(f Flavor) string { return f.ID }},
		{"Slug", func(f Flavor) string { return f.Slug }},
		{"AppName", func(f Flavor) string { return f.AppName }},
		{"BundleID", func(f Flavor) string { return f.BundleID }},
		{"Addr", func(f Flavor) string { return f.Addr }},
		{"Channel", func(f Flavor) string { return string(f.Channel) }},
	}
	for _, field := range fields {
		seen := map[string]string{}
		for _, f := range All() {
			v := field.get(f)
			if v == "" {
				t.Errorf("%s has an empty %s", f.ID, field.name)
				continue
			}
			if other, dup := seen[v]; dup {
				t.Errorf("%s and %s share %s %q", other, f.ID, field.name, v)
			}
			seen[v] = f.ID
		}
	}
}

// The slug is the bundle name, the config directory and the data directory at
// once. A slug that is a prefix of another would still be distinct, but
// `bundleFor` and the plugin's shim both match on paths, and a path prefix is
// the classic way a lookalike gets accepted.
func TestNoSlugIsAPrefixOfAnother(t *testing.T) {
	for _, a := range All() {
		for _, b := range All() {
			if a.ID == b.ID {
				continue
			}
			if strings.HasPrefix(b.Slug, a.Slug+".") || b.Slug == a.Slug {
				t.Errorf("%s's slug %q collides with %s's %q", b.ID, b.Slug, a.ID, a.Slug)
			}
		}
	}
}

// The release build takes no special invocation, so an ordinary `go build`
// cannot accidentally produce something that identifies as dev.
func TestUnstampedBuildIsRelease(t *testing.T) {
	withName(t, "", func() {
		if got := Current(); !reflect.DeepEqual(got, Release) {
			t.Errorf("an unstamped build is %+v, want release", got)
		}
		if err := Check(); err != nil {
			t.Errorf("an unstamped build reported %v", err)
		}
	})
}

func TestStampedDevBuild(t *testing.T) {
	withName(t, "dev", func() {
		f := Current()
		if f.ID != "dev" || f.Addr != Dev.Addr || f.Slug != Dev.Slug {
			t.Errorf("a dev-stamped build is %+v", f)
		}
		if err := Check(); err != nil {
			t.Errorf("a dev build reported %v", err)
		}
	})
}

// A typo in the build flags must stop the program, not quietly produce a
// second app wearing the first one's port and data directory.
func TestAnUnknownFlavourIsRefused(t *testing.T) {
	withName(t, "nightly", func() {
		err := Check()
		if err == nil {
			t.Fatal("a build stamped 'nightly' was accepted")
		}
		if !strings.Contains(err.Error(), "nightly") {
			t.Errorf("the error does not name the flavour: %v", err)
		}
	})
}

// withName runs f as though this binary had been built with that build stamp.
func withName(t *testing.T, name string, f func()) {
	t.Helper()
	old := Name
	t.Cleanup(func() { Name = old })
	Name = name
	f()
}
