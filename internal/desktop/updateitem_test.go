package desktop

import (
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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

	// A new process, as far as the file is concerned.
	got, ok := LoadUpdate()
	if !ok {
		t.Fatal("nothing was restored")
	}
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

	got, ok := LoadUpdate()
	if !ok {
		t.Fatal("nothing was restored")
	}
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

// A result that names no version is not worth carrying across a restart: it
// says only "we looked and found nothing", which is the least useful answer and
// the most likely to be stale by the next launch. It is also exactly what a
// bare {"available":false} produces — the fixture the desktop suite posts — so
// remembering it turned a test artefact into durable state.
func TestAResultWithNoVersionIsNotRemembered(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	defer stubVersion("0.2.0")()

	a := save(t)
	a.SetUpdate(update.Status{Channel: "dev", Current: "0.2.0", Latest: "0.2.0", CheckedAt: time.Now()})
	// Then a check that found nothing published. It must not overwrite what we
	// know, and it must not be restored as an answer of its own.
	a.SetUpdate(update.Status{Channel: "dev", Current: "0.2.0", CheckedAt: time.Now()})

	got, ok := LoadUpdate()
	if !ok {
		t.Fatal("the earlier result should still be on disk")
	}
	if got.Latest != "0.2.0" {
		t.Errorf("latest = %q, want the last result that named a version", got.Latest)
	}
}

func save(t *testing.T) *App {
	t.Helper()
	return &App{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// The Report a Bug window has to answer the question somebody opened it with:
// where does this go, and what do I put in it. A window that only said "please
// file an issue" would leave them exactly where they started.
func TestBugReportTextSaysWhereAndWhat(t *testing.T) {
	in := Info{
		AppName: "Agent Pet (dev)", Version: "0.2.0-dev.3", Channel: "dev",
		Addr: "127.0.0.1:9877", ConfigPath: "/c/config.yaml",
	}
	title, body := bugReportText(in, "Version 14.5 (Build 23F79)", "/d/logs/petd.log")
	if !strings.Contains(title, "Agent Pet (dev)") {
		t.Errorf("title = %q, want the application named — two of these run at once", title)
	}
	for _, want := range []string{
		update.IssuesURL,     // where it goes
		"/d/logs/petd.log",   // what to attach
		"0.2.0-dev.3", "dev", // which build
		"Version 14.5 (Build 23F79)", // which system
		"/c/config.yaml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the bug report is missing %q:\n%s", want, body)
		}
	}
	// Steps, not a link on its own: an issue saying "the pet froze" costs a
	// round trip that these lines are here to save.
	if !strings.Contains(body, "what you expected") {
		t.Errorf("the window should ask for what was expected:\n%s", body)
	}
}

// Copy Details puts a block on the clipboard, and the window shows one. If
// those two could differ, the details somebody pasted into an issue would not
// be the details they were looking at.
func TestCopiedDetailsAreTheOnesOnShow(t *testing.T) {
	in := Info{AppName: "Agent Pet", Version: "0.2.1", Channel: "release"}
	_, body := bugReportText(in, "Version 14.5", "/d/logs/petd.log")
	details := bugReportDetails(in, "Version 14.5", "/d/logs/petd.log")
	if !strings.Contains(body, details) {
		t.Errorf("the copied details are not what the window shows:\ncopied:\n%s\n\nshown:\n%s",
			details, body)
	}
}

// An empty field must say so rather than leaving a blank where a value goes.
// Off macOS there is no OS version to report, and "macOS" followed by nothing
// reads as a broken window rather than a missing fact.
func TestBugReportMarksEmptyFields(t *testing.T) {
	title, body := bugReportText(Info{}, "", "")
	if !strings.Contains(title, "Agent Pet") {
		t.Errorf("title = %q, want a fallback name", title)
	}
	if !strings.Contains(body, "macOS      —") {
		t.Errorf("an unknown OS version should be marked:\n%s", body)
	}
	// Counted in the details alone: the instructions above them have an em
	// dash of their own, so counting the whole window would pass on the wrong
	// six.
	details := bugReportDetails(Info{}, "", "")
	// Everything but the issue URL, which is a constant and cannot be empty.
	if got := strings.Count(details, "—"); got != 6 {
		t.Errorf("every empty field should be marked, got %d:\n%s", got, details)
	}
}

// Report on GitHub opens the new-issue form with the report already in it. The
// details have to survive the trip: an issue that arrives without the version
// costs the round trip this window exists to save.
func TestPrefilledIssueCarriesTheDetails(t *testing.T) {
	in := Info{
		AppName: "Agent Pet", Version: "0.3.0", Channel: "release",
		Addr: "127.0.0.1:9876", ConfigPath: "/c/config.yaml",
	}
	raw := bugReportIssueURL(in, "Version 26.5.2", "/d/logs/petd.log")
	// Whatever else it is, it is a URL the pet is allowed to open. openURL puts
	// it through this again and drops it silently if it fails, so a URL that
	// does not pass here is a button that does nothing.
	if err := update.ValidateNotesURL(raw); err != nil {
		t.Fatalf("the pet would refuse to open its own URL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Path != "/lijuno/agent-pet/issues/new" {
		t.Errorf("path = %q, want the new-issue form", u.Path)
	}
	body := u.Query().Get("body")
	for _, want := range []string{
		"0.3.0", "release", "Version 26.5.2", "/c/config.yaml", "/d/logs/petd.log",
		"What happened", "What I expected",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the prefilled issue is missing %q:\n%s", want, body)
		}
	}
	// The same block the window shows and the clipboard gets. Three copies of
	// one thing is three chances for the pasted details to be the wrong ones.
	if !strings.Contains(body, bugReportDetails(in, "Version 26.5.2", "/d/logs/petd.log")) {
		t.Errorf("the issue body is not the details on show:\n%s", body)
	}
}

// A path is somebody's to choose, and a raw one carrying & or # would end the
// query and start something else. Everything goes through url.Values, so the
// value comes back out of the query exactly as it went in.
func TestPrefilledIssueSurvivesAnAwkwardPath(t *testing.T) {
	odd := "/Users/a&b/pets#1/config.yaml?x=1"
	raw := bugReportIssueURL(Info{Version: "0.3.0", ConfigPath: odd}, "", "/d/petd.log")
	if err := update.ValidateNotesURL(raw); err != nil {
		t.Fatalf("an awkward path should not make the URL unopenable: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q := u.Query(); len(q) != 1 || q.Get("body") == "" {
		t.Errorf("the path escaped its parameter: %v", q)
	}
	if !strings.Contains(u.Query().Get("body"), odd) {
		t.Errorf("the path did not survive encoding:\n%s", u.Query().Get("body"))
	}
}

// The prefilled issue carries the config as well as the details. It is the
// small file of the two and the one that answers "what was it set to"; the log
// is a megabyte before it rotates and cannot go in a URL at all.
func TestPrefilledIssueCarriesTheConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("pet:\n  scale: 1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := bugReportIssueURL(Info{Version: "0.3.0", ConfigPath: cfg}, "", "/d/petd.log")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := u.Query().Get("body")
	if !strings.Contains(body, "scale: 1.5") {
		t.Errorf("the config did not reach the issue:\n%s", body)
	}
	// And says where the log is instead of pretending it is in there.
	if !strings.Contains(body, "Save Report") {
		t.Errorf("the issue should say how to attach the log:\n%s", body)
	}
}

// A config big enough to burst the URL comes out of it, because GitHub refuses
// an over-long request outright: a form with one section missing beats an
// error page with none of it.
//
// The config here is comfortably small as a file and still too big as a URL,
// which is the case worth holding: every non-ASCII byte becomes three
// characters once encoded, and the characters that ship with this program are
// named 三毛 and 桃桃. Sizing the check on the file would have missed it.
func TestAnOversizedConfigLeavesTheURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(strings.Repeat("# 桃桃的设置\n", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(cfg); len(b) > maxReportConfig {
		t.Fatalf("this test needs a config small enough to quote, got %d bytes", len(b))
	}
	raw := bugReportIssueURL(Info{Version: "0.3.0", ConfigPath: cfg}, "", "/d/petd.log")
	if len(raw) > maxIssueURL {
		t.Errorf("the URL is %d bytes, past the %d it may be", len(raw), maxIssueURL)
	}
	if err := update.ValidateNotesURL(raw); err != nil {
		t.Fatalf("the fallback is not openable: %v", err)
	}
	u, _ := url.Parse(raw)
	body := u.Query().Get("body")
	if !strings.Contains(body, "too long for this form") {
		t.Errorf("a dropped config should say so:\n%s", body)
	}
	// The details are still there. Dropping the config is the small loss; a
	// form with nothing in it would be the big one.
	if !strings.Contains(body, "0.3.0") {
		t.Errorf("the details went with it:\n%s", body)
	}
}

// A config too big to quote at all says so where it would have been, in the
// window and the file as well as the issue. Nothing here is silent.
func TestAHugeConfigIsNotQuoted(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(strings.Repeat("x", maxReportConfig+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readConfigSection(cfg)
	if !strings.Contains(got, "too big to quote") {
		t.Errorf("an unquotable config should say so, got %q", got)
	}
	if strings.Contains(got, strings.Repeat("x", 100)) {
		t.Errorf("it should not be quoted anyway")
	}
}

// Save Report writes the file that gets dragged onto the issue — a URL can
// prefill a form and nothing else, so this is the only route an attachment
// has. It holds all three parts, and the log by its tail.
func TestSavedReportHoldsTheFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("pet:\n  scale: 1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "petd.log")
	var lines []string
	for i := 0; i < reportLogLines+50; i++ {
		lines = append(lines, fmt.Sprintf("t=00:00:%02d level=INFO msg=event n=%d", i%60, i))
	}
	if err := os.WriteFile(log, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := saveBugReport(Info{Version: "0.3.0", ConfigPath: cfg}, "", log)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the saved report is not there: %v", err)
	}
	got := string(b)
	for _, want := range []string{"0.3.0", "config.yaml", "scale: 1.5", "petd.log", "n=249"} {
		if !strings.Contains(got, want) {
			t.Errorf("the saved report is missing %q:\n%s", want, got)
		}
	}
	// The tail, not the whole file: the last 200 of 250 lines, so the first
	// line is gone and the last is there.
	if strings.Contains(got, "n=0 ") || strings.Contains(got, "n=49\n") {
		t.Errorf("the log should be quoted by its tail, not whole:\n%s", got)
	}
	// The clipboard gets the same text. Two builders would be two chances for
	// the file somebody attached to disagree with the text they pasted.
	if bundle := bugReportBundle(Info{Version: "0.3.0", ConfigPath: cfg}, "", log); bundle != got {
		t.Errorf("the saved file and the clipboard differ")
	}
}

// A file that cannot be read is a fact about a broken install, not a reason to
// say nothing: a report that quietly omitted the config would read as one
// written by somebody who could not be bothered.
func TestAMissingFileIsReportedRatherThanOmitted(t *testing.T) {
	got := bugReportBundle(Info{Version: "0.3.0", ConfigPath: "/nope/config.yaml"}, "", "/nope/petd.log")
	for _, want := range []string{"could not read /nope/config.yaml", "could not read /nope/petd.log"} {
		if !strings.Contains(got, want) {
			t.Errorf("the report should say what it could not read, want %q:\n%s", want, got)
		}
	}
}
