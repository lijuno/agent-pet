// Package flavor is the identity of this build: which of the two apps it is.
//
// Agent Pet ships as two separate applications rather than one app with a
// channel setting — `Agent Pet` and `Agent Pet (dev)`, installed side by side,
// the way VS Code ships Stable and Insiders (ADR 0008). Everything that must
// differ between them is here and nowhere else.
//
// That last part is the point. Before this package the bundle identifier, the
// port, the config path and the data directory were literals scattered across
// five files, and a second app is exactly the thing that turns each of those
// into a collision: two pets sharing one config, or a second instance exiting
// because the first has the port. TestFlavoursDoNotCollide is the guard.
package flavor

import (
	"fmt"

	"github.com/lijuno/agent-pet/internal/update"
)

// Name is stamped at build time with -ldflags. Empty means the release build,
// which is deliberate: the ordinary build needs no special invocation and
// cannot accidentally identify as something else.
var Name = ""

// Flavor is everything about a build that the other build must not share.
type Flavor struct {
	// ID is "release" or "dev".
	ID string
	// Slug names the bundle on disk (agent-pet.app), the config directory and
	// the data directory. One string, so those three cannot drift apart.
	Slug string
	// AppName is what the Finder and Gatekeeper show.
	AppName string
	// BundleID is the CFBundleIdentifier. Different identifiers are what make
	// macOS treat these as two applications rather than two copies of one.
	BundleID string
	// Addr is the event API address. The whole reason two pets can run at once.
	Addr string
	// Channel is the update manifest this build follows. A build constant, not
	// a setting: an app cannot be moved between channels, because moving
	// between channels means running the other app.
	Channel update.Channel
	// Label is how the build says which one it is, in the menu bar and in
	// `petctl doctor`. Empty for release — the ordinary app does not need to
	// announce that it is ordinary.
	Label string
}

var (
	Release = Flavor{
		ID:       "release",
		Slug:     "agent-pet",
		AppName:  "Agent Pet",
		BundleID: "io.github.lijuno.agent-pet",
		Addr:     "127.0.0.1:9876",
		Channel:  update.Release,
		Label:    "",
	}
	Dev = Flavor{
		ID:       "dev",
		Slug:     "agent-pet-dev",
		AppName:  "Agent Pet (dev)",
		BundleID: "io.github.lijuno.agent-pet-dev",
		Addr:     "127.0.0.1:9877",
		Channel:  update.Dev,
		Label:    "dev",
	}
)

// All is every flavor, release first. Used by anything that has to consider
// both: broadcasting an event, and `petctl doctor` reporting what is installed.
func All() []Flavor { return []Flavor{Release, Dev} }

// resolve is deliberately not cached. It is a switch on a string set at link
// time, nothing is saved by remembering it, and a cache would mean tests in
// other packages could not exercise the dev build at all.
func resolve() (Flavor, error) {
	switch Name {
	case "", "release":
		return Release, nil
	case "dev":
		return Dev, nil
	}
	// Falling back to release silently would be worse than refusing. A build
	// stamped with a name nobody recognises would take the release app's port,
	// config and data directory — the exact collision this package exists to
	// prevent.
	return Release, fmt.Errorf("this build is stamped as flavor %q, which does not exist", Name)
}

// Current is the flavor this binary was built as.
func Current() Flavor {
	f, _ := resolve()
	return f
}

// Check reports a build stamped with a flavor that does not exist. main and
// petctl call it before doing anything else; nothing else needs to.
func Check() error {
	_, err := resolve()
	return err
}

// ByID finds a flavor by its ID.
func ByID(id string) (Flavor, bool) {
	for _, f := range All() {
		if f.ID == id {
			return f, true
		}
	}
	return Flavor{}, false
}

// AppFile is the bundle's name on disk.
func (f Flavor) AppFile() string { return f.Slug + ".app" }

// IsDev reports whether this is the prerelease app.
func (f Flavor) IsDev() bool { return f.ID == Dev.ID }
