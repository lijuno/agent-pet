package desktop

import (
	"io"
	"log/slog"
	"strings"
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

// The About window names the application, not the character. Two of these run
// side by side with separate settings, ports and update channels (ADR 0008),
// and "which one am I looking at" is the question the window exists to answer.
func TestAboutTextNamesTheApplication(t *testing.T) {
	in := Info{
		AppName: "Agent Pet (dev)", Version: "0.2.0-dev.3", Channel: "dev",
		Addr: "127.0.0.1:9877", ConfigPath: "/c/config.yaml", PetsDir: "/d/pets",
	}
	title, body := aboutText(in)
	if title != "Agent Pet (dev)" {
		t.Errorf("title = %q, want the application name", title)
	}
	for _, want := range []string{"0.2.0-dev.3", "dev", "127.0.0.1:9877", "/c/config.yaml", "/d/pets"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
	// The labels are padded into a column, so every value starts at the same
	// offset. A value that runs into its label is what the padding prevents,
	// and it is invisible until somebody opens the window. Checked by offset
	// rather than by splitting on a space: "Event API" has one in it.
	for _, label := range []string{"Version", "Channel", "Event API", "Config", "Pets"} {
		var line string
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, label) {
				line = l
				break
			}
		}
		if line == "" {
			t.Errorf("no row for %q", label)
			continue
		}
		if len(line) < 12 || line[10] != ' ' || line[11] == ' ' {
			t.Errorf("value should start at column 12: %q", line)
		}
	}
}

// The active character is a preference the user changes from the menu at any
// time, not a fact about this build. In a window called About it reads as
// though the character were part of what the application is.
func TestAboutTextOmitsTheCharacter(t *testing.T) {
	_, body := aboutText(Info{AppName: "Agent Pet", Version: "0.2.0"})
	if strings.Contains(body, "Character") {
		t.Errorf("About should not carry the active character:\n%s", body)
	}
}

// An empty field must say so rather than leaving a blank where a value goes: a
// missing value and a broken layout look identical otherwise.
func TestAboutTextMarksEmptyFields(t *testing.T) {
	title, body := aboutText(Info{})
	if title != "Agent Pet" {
		t.Errorf("title = %q, want a fallback name", title)
	}
	if strings.Count(body, "—") != 5 {
		t.Errorf("every empty field should be marked:\n%s", body)
	}
}

// petd is told update results and holds them in memory, so every restart
// forgets — and an update *is* a restart, which made "no update check yet" the
// state at the one moment somebody is certain to look. The result is written
// beside the config now and read back at startup.
func TestUpdateResultSurvivesARestart(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	defer stubVersion("0.2.0")()

	save(t).SetUpdate(update.Status{
		Channel: "dev", Current: "0.2.0", Latest: "0.2.0",
		Available: false, CheckedAt: time.Now(),
	})

	// A different App: a new process, as far as the file is concerned.
	b := save(t)
	b.loadUpdate()
	got := b.GetUpdate()
	if got.Latest != "0.2.0" {
		t.Fatalf("latest = %q, want the saved 0.2.0", got.Latest)
	}
	if got.CheckedAt.IsZero() {
		t.Error("the check time did not survive, so the menu still says nobody looked")
	}
	if title := updateItemTitle(got); title != "Up to date" {
		t.Errorf("menu would say %q after a restart, want %q", title, "Up to date")
	}
}

// The case that telling the daemon cannot fix: the app was closed when it was
// updated, so there was nothing to tell. The stored result names the version
// that got replaced and its Available was computed against that, so both are
// re-derived from the binary actually running.
func TestLoadUpdateRederivesAgainstTheRunningBuild(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	defer stubVersion("0.2.0")()

	// Saved while 0.2.0 was on offer and 0.1.0 was running.
	save(t).SetUpdate(update.Status{
		Channel: "dev", Current: "0.1.0", Latest: "0.2.0",
		Available: true, CheckedAt: time.Now(),
	})

	b := save(t)
	b.loadUpdate()
	got := b.GetUpdate()
	if got.Current != "0.2.0" {
		t.Errorf("current = %q, want the running build 0.2.0", got.Current)
	}
	if got.Available {
		t.Error("the update was taken; it must not still be on offer")
	}
	if title := updateItemTitle(got); title != "Up to date" {
		t.Errorf("menu would say %q, want %q", title, "Up to date")
	}
}

// stubVersion pretends this binary was stamped by a release build. Tests run
// without ldflags, so Version is the "dev" sentinel — which Status.Validate
// rejects as a Latest, being no version at all.
func stubVersion(v string) func() {
	old := Version
	Version = v
	return func() { Version = old }
}

func save(t *testing.T) *App {
	t.Helper()
	return &App{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}
