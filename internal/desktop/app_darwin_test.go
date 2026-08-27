package desktop

import (
	"strings"
	"testing"

	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/state"
)

// Darwin-only because menuStateLine lives in statusitem_darwin.go, next to the
// Objective-C it feeds. Keeping this test beside it is what lets the rest of
// the package's tests compile on Linux, where a cloud session runs them.

// Two menu-bar-only apps have no Dock icon and no app-switcher entry, so the
// menu's first line is one of only two things that can say which is which.
func TestTheDevMenuSaysWhichAppItIs(t *testing.T) {
	oldName, oldVersion := flavor.Name, Version
	t.Cleanup(func() { flavor.Name, Version = oldName, oldVersion })
	Version = "0.3.0-dev.1"

	flavor.Name = ""
	if got := menuStateLine(state.Idle); got != "Idle" {
		t.Errorf("the release menu says %q; it should not announce that it is ordinary", got)
	}

	flavor.Name = "dev"
	if got := menuStateLine(state.Idle); !strings.Contains(got, "dev") || !strings.Contains(got, "0.3.0-dev.1") {
		t.Errorf("the dev menu says %q, which does not identify the app or its version", got)
	}
}
