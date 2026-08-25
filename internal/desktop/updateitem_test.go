package desktop

import (
	"testing"
	"time"

	"github.com/lijuno/agent-pet/internal/update"
)

// The update item is always in the menu, so every state the updater can be in
// has to have something true to say. "No update" and "could not find out" are
// different answers, and so are "nothing has been published" and "you are ahead
// of what has" — reporting any of them as another is the pet guessing, which it
// does not do (§ adapters/README.md, "degrading honestly").
func TestUpdateItemTitle(t *testing.T) {
	checked := time.Date(2026, 8, 25, 9, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		name string
		st   update.Status
		want string
	}{{
		name: "an update is offered",
		st:   update.Status{Current: "0.1.0", Latest: "0.2.0", Available: true, CheckedAt: checked},
		want: "Update to 0.2.0…",
	}, {
		name: "nothing has been checked yet",
		st:   update.Status{Current: "0.1.0"},
		want: "No update check yet",
	}, {
		name: "checked, and the channel has nothing on it",
		st:   update.Status{Current: "0.1.0", CheckedAt: checked},
		want: "Nothing published yet",
	}, {
		name: "checked, and this is the newest there is",
		st:   update.Status{Current: "0.2.0", Latest: "0.2.0", CheckedAt: checked},
		want: "Up to date",
	}, {
		name: "running a prerelease newer than the channel",
		st:   update.Status{Current: "0.3.0-dev.1", Latest: "0.2.0", CheckedAt: checked},
		want: "Ahead of the channel",
	}, {
		name: "the check itself failed",
		st:   update.Status{Current: "0.1.0", Error: "dial tcp: no route to host", CheckedAt: checked},
		want: "Update check failed",
	}, {
		// An error must win over a stale success, or a check that could not run
		// leaves the previous answer on screen looking current.
		name: "an error outranks an earlier result",
		st:   update.Status{Current: "0.1.0", Latest: "0.1.0", Error: "timeout", CheckedAt: checked},
		want: "Update check failed",
	}} {
		t.Run(c.name, func(t *testing.T) {
			if got := updateItemTitle(c.st); got != c.want {
				t.Errorf("updateItemTitle() = %q, want %q", got, c.want)
			}
		})
	}
}

// Whatever an agent reports, nothing it controls becomes menu markup (§26).
// The version is validated on the way in, but the title is built by string
// concatenation and that is the line worth pinning.
func TestUpdateItemTitleCannotBeInjected(t *testing.T) {
	st := update.Status{
		Current: "0.1.0", Available: true, Latest: "0.2.0", CheckedAt: time.Now(),
	}
	if got := updateItemTitle(st); got != "Update to 0.2.0…" {
		t.Fatalf("unexpected title %q", got)
	}
	if err := st.Validate(); err != nil {
		t.Fatalf("a status this ordinary should validate: %v", err)
	}
	st.Latest = "0.2.0|9:Quit Pet"
	if err := st.Validate(); err == nil {
		t.Error("a version carrying menu-dump punctuation should not validate")
	}
}
