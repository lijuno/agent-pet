package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lijuno/agent-pet/internal/update"
)

// The bundle to replace is found from the running executable. Guessing
// /Applications instead would update a copy other than the one in use, which
// looks exactly like an update that did nothing.
func TestBundleFor(t *testing.T) {
	cases := []struct {
		exe  string
		want string
	}{
		{"/Applications/agent-pet.app/Contents/MacOS/petctl", "/Applications/agent-pet.app"},
		{"/Users/x/build/bin/agent-pet.app/Contents/MacOS/petctl", "/Users/x/build/bin/agent-pet.app"},
		{"/usr/local/bin/petctl", ""},
		{"/Users/x/go/bin/petctl", ""},
		{"petctl", ""},
	}
	for _, c := range cases {
		got, ok := bundleFor(c.exe)
		if c.want == "" {
			if ok {
				t.Errorf("bundleFor(%q) = %q, want no bundle", c.exe, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("bundleFor(%q) = %q, %v; want %q", c.exe, got, ok, c.want)
		}
	}
}

// The team check is what turns "somebody Apple knows notarized this" into "we
// published this". Everything about reading it comes from this one line of
// codesign output.
func TestParseTeamID(t *testing.T) {
	const real = `Executable=/Applications/agent-pet.app/Contents/MacOS/petd
Identifier=com.agentpet.app
Format=app bundle with Mach-O universal (x86_64 arm64)
Signature size=9075
Authority=Developer ID Application: Some One (X85ZX835W9)
TeamIdentifier=X85ZX835W9
Timestamp=1 Jan 2026 at 00:00:00
`
	if got := parseTeamID(real); got != "X85ZX835W9" {
		t.Errorf("parseTeamID = %q, want X85ZX835W9", got)
	}
	// An ad-hoc or unsigned bundle says this, and it must not compare equal to
	// another unsigned bundle — that would let any unsigned app replace another.
	if got := parseTeamID("TeamIdentifier=not set\n"); got != "" {
		t.Errorf("parseTeamID(not set) = %q, want empty", got)
	}
	if got := parseTeamID("Identifier=com.example\n"); got != "" {
		t.Errorf("parseTeamID with no team = %q, want empty", got)
	}
}

// A GitHub release asset redirects to its storage host, which is fine. A
// redirect anywhere else means the download is no longer the file the manifest
// named, and the manifest is the only thing that was checked.
func TestRedirectsStayInsideGitHub(t *testing.T) {
	ok := []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "GitHub.com"}
	for _, h := range ok {
		if !allowedDownloadHost(h) {
			t.Errorf("%s was refused", h)
		}
	}
	bad := []string{"evil.test", "githubusercontent.com.evil.test", "github.com.evil.test", "notgithub.com", ""}
	for _, h := range bad {
		if allowedDownloadHost(h) {
			t.Errorf("%s was allowed", h)
		}
	}

	req, _ := http.NewRequest(http.MethodGet, "http://github.com/x", nil)
	if err := checkRedirect(req, nil); err == nil {
		t.Error("a redirect to plain http was allowed")
	}
	req, _ = http.NewRequest(http.MethodGet, "https://evil.test/x", nil)
	if err := checkRedirect(req, nil); err == nil {
		t.Error("a redirect off GitHub was allowed")
	}
	req, _ = http.NewRequest(http.MethodGet, "https://objects.githubusercontent.com/x", nil)
	if err := checkRedirect(req, nil); err != nil {
		t.Errorf("the ordinary asset redirect was refused: %v", err)
	}
}

func TestWithin(t *testing.T) {
	app := "/Applications/agent-pet.app"
	if !within(filepath.Join(app, "Contents/MacOS/petctl"), app) {
		t.Error("the bundled petctl was not recognised as being inside the bundle")
	}
	if within("/usr/local/bin/petctl", app) {
		t.Error("a petctl outside the bundle was treated as inside it")
	}
	// The one that matters: a bundle whose name merely starts the same.
	if within("/Applications/agent-pet.app.old/Contents/MacOS/petctl", app) {
		t.Error("a different bundle was treated as inside this one")
	}
}

func manifestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

const goodManifest = `{"channel":"release","version":"0.2.0",
 "url":"https://github.com/lijuno/agent-pet/releases/download/v0.2.0/agent-pet-0.2.0-universal.zip",
 "sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":100}`

// Nothing has been published on the dev channel on the day this was written,
// and a 404 has to read as "nothing yet" rather than as a broken updater.
func TestFetchManifestTreats404AsNothingPublished(t *testing.T) {
	ts := manifestServer(t, http.StatusNotFound, "Not Found")
	m, found, err := fetchManifest(ts.URL, update.Release)
	if err != nil {
		t.Fatalf("a 404 was reported as an error: %v", err)
	}
	if found || m.Version != "" {
		t.Fatalf("a 404 produced a manifest: %+v", m)
	}
}

func TestFetchManifest(t *testing.T) {
	ts := manifestServer(t, http.StatusOK, goodManifest)
	m, found, err := fetchManifest(ts.URL, update.Release)
	if err != nil || !found {
		t.Fatalf("fetch: %v", err)
	}
	if m.Version != "0.2.0" {
		t.Errorf("version = %q", m.Version)
	}

	for _, c := range []struct {
		name   string
		status int
		body   string
	}{
		{"a server error", http.StatusInternalServerError, "boom"},
		{"something that is not JSON", http.StatusOK, "<html>nope</html>"},
		{"a manifest for another channel", http.StatusOK, strings.Replace(goodManifest, `"release"`, `"dev"`, 1)},
		{"an asset URL off GitHub", http.StatusOK, strings.Replace(goodManifest, "https://github.com", "https://evil.test", 1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			ts := manifestServer(t, c.status, c.body)
			if _, _, err := fetchManifest(ts.URL, update.Release); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

// The hash is what proves the bytes are the ones the manifest described. A
// download that does not match it is not installed, whatever else is true of it.
func TestDownloadChecksTheHashAndSize(t *testing.T) {
	payload := []byte("not really a zip, but it has a hash")
	sum := sha256.Sum256(payload)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	t.Cleanup(ts.Close)

	base := update.Manifest{
		URL:    ts.URL,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(payload)),
	}
	if _, err := download(base, t.TempDir()); err != nil {
		t.Fatalf("a matching download was rejected: %v", err)
	}

	wrongHash := base
	wrongHash.SHA256 = strings.Repeat("b", 64)
	if _, err := download(wrongHash, t.TempDir()); err == nil {
		t.Error("a download with the wrong hash was accepted")
	}

	wrongSize := base
	wrongSize.Size = int64(len(payload)) + 10
	if _, err := download(wrongSize, t.TempDir()); err == nil {
		t.Error("a download shorter than the manifest said was accepted")
	}

	// A manifest that understates the size must not be able to stream more
	// than it declared onto the disk.
	short := base
	short.Size = 4
	if _, err := download(short, t.TempDir()); err == nil {
		t.Error("a download longer than the manifest said was accepted")
	}
}

// A build from source has no release to be replaced by. This is the check that
// stops `petctl update` throwing away whatever the developer is working on.
func TestApplyRefusesDevBuilds(t *testing.T) {
	if version != update.DevBuild {
		t.Skipf("this test binary reports version %q", version)
	}
	err := apply(&client{base: "http://127.0.0.1:1"}, update.Manifest{Version: "9.9.9"}, updateOpts{})
	if err == nil || !strings.Contains(err.Error(), "dev build") {
		t.Fatalf("apply on a dev build returned %v", err)
	}
}

// The automatic check runs from somebody else's tool call, so how often it is
// allowed to run is the whole of what makes it acceptable.
func TestDueForCheck(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	if !dueForCheck(day, time.Time{}, now) {
		t.Error("a check that has never run should be due")
	}
	if dueForCheck(day, now.Add(-time.Hour), now) {
		t.Error("a check an hour old was run again")
	}
	if !dueForCheck(day, now.Add(-25*time.Hour), now) {
		t.Error("a check older than the interval was not due")
	}
	if !dueForCheck(day, now.Add(-day), now) {
		t.Error("a check exactly one interval old was not due")
	}
	// Clocks move. Without this the check quietly stops running until the
	// calendar catches up with a stamp written in the future.
	if !dueForCheck(day, now.Add(48*time.Hour), now) {
		t.Error("a stamp in the future was treated as recent")
	}
}

// capture takes what printStatus wrote, because what it says is the whole
// point of it.
func capture(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	var b strings.Builder
	io.Copy(&b, r)
	return b.String()
}

// Switching from dev back to release leaves you ahead of the channel you just
// chose. Calling that "up to date" would be wrong in the one direction that
// matters: nothing will be installed, and the reason is not that you have the
// newest build — it is that the channel is behind you.
func TestAheadOfTheChannelIsNotUpToDate(t *testing.T) {
	st := update.Status{Channel: update.Release, Current: "0.3.0-dev.2", Latest: "0.2.0"}
	m := update.Manifest{Version: "0.2.0"}
	out := capture(func() { printStatus(st, true, m) })

	if strings.Contains(out, "Up to date") {
		t.Errorf("being ahead of the channel was reported as up to date:\n%s", out)
	}
	for _, want := range []string{"0.3.0-dev.2", "0.2.0", "release"} {
		if !strings.Contains(out, want) {
			t.Errorf("the message does not mention %q:\n%s", want, out)
		}
	}

	// And the ordinary case still reads the ordinary way.
	same := update.Status{Channel: update.Release, Current: "0.2.0", Latest: "0.2.0"}
	out = capture(func() { printStatus(same, true, update.Manifest{Version: "0.2.0"}) })
	if !strings.Contains(out, "Up to date") {
		t.Errorf("actually being up to date did not say so:\n%s", out)
	}
}

// Nothing published on a channel is a different answer from being up to date,
// and it is the answer both channels give today.
func TestNothingPublishedSaysSo(t *testing.T) {
	st := update.Status{Channel: update.Dev, Current: "0.2.0"}
	out := capture(func() { printStatus(st, false, update.Manifest{}) })
	if !strings.Contains(out, "No dev build has been published yet") {
		t.Errorf("unexpected message:\n%s", out)
	}
}

// After an update the menu bar used to say "No update check yet", because petd
// holds only what it has been told and a daemon that has just started has been
// told nothing. It is true of the process and useless to somebody who watched
// the update finish. petctl knows the answer at that moment and now says so.
//
// The three fields below are exactly what internal/desktop reads as "Up to
// date": equal versions, nothing available, and a check time that is not zero.
func TestInstalledStatusReadsAsUpToDate(t *testing.T) {
	m := update.Manifest{
		Channel: "dev", Version: "0.2.0-dev.3",
		NotesURL: "https://github.com/lijuno/agent-pet/releases/tag/v0.2.0-dev.3",
	}
	st := installedStatus(m, "0.2.0-dev.3")

	if st.Current != st.Latest {
		t.Errorf("current %q and latest %q should match after an update", st.Current, st.Latest)
	}
	if st.Available {
		t.Error("an update is not available immediately after taking it")
	}
	if st.CheckedAt.IsZero() {
		t.Error("a zero check time is what makes the menu say nobody has ever looked")
	}
	if st.Channel != update.Channel("dev") {
		t.Errorf("channel = %q, want the manifest's", st.Channel)
	}
	if st.NotesURL != m.NotesURL {
		t.Error("the notes URL should survive, so the item stays pressable")
	}
	if err := st.Validate(); err != nil {
		t.Errorf("the status petd is about to be told does not validate: %v", err)
	}
}

// The copy `petctl update` makes of itself must survive until the update is
// done. Deleting it at startup — which is what used to happen — unlinks the
// running executable, and macOS then cannot verify the process's code
// signature when TLS asks it to, so the manifest fetch fails with
// `SecPolicyCreateSSL error: 0`. Over-the-air updates were broken outright.
func TestSweepSparesTheRunningCopy(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * detachedGrace)

	mine := filepath.Join(dir, "petctl-update-mine")
	stale := filepath.Join(dir, "petctl-update-stale")
	fresh := filepath.Join(dir, "petctl-update-fresh")
	other := filepath.Join(dir, "something-else")
	for _, d := range []string{mine, stale, fresh, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{mine, stale, other} {
		if err := os.Chtimes(d, old, old); err != nil {
			t.Fatal(err)
		}
	}

	sweepDetached(dir, mine, time.Now())

	if _, err := os.Stat(mine); err != nil {
		t.Error("the sweep took the copy this process is running from")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("the sweep took a copy young enough to be another update in flight")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a copy left behind by an earlier run should be swept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("the sweep took a directory that is not ours")
	}
}

// With no marker set this is not the detached copy at all, and every stale copy
// is fair game.
func TestSweepWithNoCopyOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * detachedGrace)
	stale := filepath.Join(dir, "petctl-update-stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	sweepDetached(dir, "", time.Now())
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale copy should be swept when there is no run of our own")
	}
}

// TestTheCopyIsToldWhatItIsReplacing covers the second half of the re-exec,
// which had no cover at all and did not work.
//
// The copy runs from a temporary directory. os.Executable no longer points
// inside any bundle there, and os.Args does not name one either — the target
// is derived from the running binary rather than passed as a flag — so unless
// it is carried across explicitly the copy downloads the update and then
// refuses to install it, on the grounds that it is not inside an app. Which is
// true of it, and beside the point.
func TestTheCopyIsToldWhatItIsReplacing(t *testing.T) {
	const target = "/Applications/agent-pet-dev.app"
	t.Setenv(targetEnv, target)

	got, err := resolveTarget("")
	if err != nil {
		t.Fatalf("a copy that was told its target should resolve it: %v", err)
	}
	if got != target {
		t.Fatalf("resolved %q, want %q", got, target)
	}

	// An explicit --app still wins over what was carried.
	if got, err := resolveTarget("/Applications/agent-pet.app"); err != nil ||
		got != "/Applications/agent-pet.app" {
		t.Fatalf("--app should win over the carried target, got %q/%v", got, err)
	}
}
