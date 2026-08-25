package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/update"
)

// detachedEnv marks the copy of petctl that runs from outside the bundle it is
// replacing. See reexecOutside.
const detachedEnv = "AGENT_PET_UPDATE_DETACHED"

// quitWait is how long the running app is given to let go of the event port.
// Replacing the bundle while the old daemon still holds it produces the exact
// failure CLAUDE.md warns about: the update appears to do nothing, because the
// old process is still answering.
const quitWait = 15 * time.Second

// launchWait is how long the new app is given to come up and report its
// version. Notarized first launches are slower than later ones — Gatekeeper is
// checking the signature.
const launchWait = 30 * time.Second

func apply(c *client, m update.Manifest, o updateOpts) error {
	if version == update.DevBuild {
		return fmt.Errorf("this is a dev build; refusing to replace it with %s", m.Version)
	}

	target, err := targetBundle(o.app)
	if err != nil {
		return err
	}
	// Before anything else: get out of the bundle we are about to delete.
	if err := reexecOutside(target); err != nil {
		return err
	}
	if err := checkMacOS(m); err != nil {
		return err
	}

	// Staged beside the bundle, not in /tmp. Two reasons: a rename across
	// volumes is not atomic and /tmp may be one, and failing here — rather
	// than after a 30 MB download — is how somebody without write access to
	// /Applications finds out.
	parent := filepath.Dir(target)
	stage, err := os.MkdirTemp(parent, ".agent-pet-update-")
	if err != nil {
		return fmt.Errorf("cannot stage an update next to %s: %w\n  (that directory is not writable by you)", target, err)
	}
	defer os.RemoveAll(stage)

	fmt.Printf("Downloading %s (%s)\n", m.Version, humanSize(m.Size))
	zip, err := download(m, stage)
	if err != nil {
		return err
	}

	fmt.Println("Verifying the signature")
	fresh, err := unpack(zip, stage)
	if err != nil {
		return err
	}
	if err := verify(fresh, target, m); err != nil {
		return err
	}

	running := reachable(c)
	if running {
		fmt.Println("Quitting the running pet")
		if err := quit(c); err != nil {
			return err
		}
	}

	fmt.Printf("Installing into %s\n", target)
	if err := swap(fresh, target, stage); err != nil {
		return err
	}

	if !running {
		fmt.Printf("Updated to %s. The pet was not running, so it has been left closed.\n", m.Version)
		return nil
	}
	fmt.Println("Starting the new version")
	if err := launch(target); err != nil {
		return err
	}
	got, err := awaitVersion(c, m.Version, target)
	if err != nil {
		return err
	}
	// Tell the new daemon what it is running before anybody looks at the menu.
	//
	// petd holds a result it was told and nothing else — it opens no
	// connections of its own (ADR 0008) — and a daemon that has just started
	// has been told nothing, so its menu says "No update check yet". Which is
	// true of that process and useless to the person who watched an update
	// finish thirty seconds ago. We know the answer here: the manifest we just
	// installed is the newest the channel has, and the version answering the
	// port is that one.
	report(c, installedStatus(m, got))
	fmt.Printf("Updated to %s.\n", got)
	return nil
}

// installedStatus is what to tell the daemon after an update has landed.
//
// Current and Latest are equal, Available is false and CheckedAt is now: those
// three together are what the menu bar reads as "Up to date". Getting any of
// them wrong leaves the menu saying something else — CheckedAt in particular,
// because a zero time there means "nobody has ever checked", which is what the
// menu said after an update until this existed.
func installedStatus(m update.Manifest, installed string) update.Status {
	return update.Status{
		Channel:   update.Channel(m.Channel),
		Current:   installed,
		Latest:    m.Version,
		Available: false,
		NotesURL:  m.NotesURL,
		CheckedAt: time.Now(),
	}
}

// targetBundle is the .app this petctl belongs to. It is found from the running
// executable rather than assumed to be in /Applications: updating a copy other
// than the one in use is how you get an update that appears to do nothing.
func targetBundle(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(abs, ".app") {
			return "", fmt.Errorf("--app %s is not a bundle", abs)
		}
		return abs, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	app, ok := bundleFor(exe)
	if !ok {
		return "", fmt.Errorf("this petctl is not inside an app bundle (%s)\n"+
			"  Only the petctl that ships inside agent-pet.app can update it; a copy built\n"+
			"  with `make petctl` has no bundle to replace.", exe)
	}
	return app, nil
}

// reexecOutside copies this program out of the bundle and re-runs it from
// there, when the bundle it is about to replace is the one it is running from.
//
// A process keeps running after its executable is unlinked, but the dynamic
// linker has not necessarily finished with the file, and the bundle's other
// contents are certainly still needed. Sparkle solves this by copying its
// helper to a temporary directory first; so does this.
func reexecOutside(target string) error {
	if os.Getenv(detachedEnv) != "" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if !within(exe, target) {
		return nil
	}
	dir, err := os.MkdirTemp("", "petctl-update-")
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, "petctl")
	if err := copyFile(exe, dst, 0o755); err != nil {
		return err
	}
	env := append(os.Environ(), detachedEnv+"="+dir)
	// Exec, not Start: the copy inherits the terminal and the exit code, so
	// this stays one command from the caller's point of view.
	return syscall.Exec(dst, os.Args, env)
}

// cleanupDetached removes the copy made by reexecOutside. Called at startup by
// the copy itself: unlinking a running executable is fine, and doing it now
// means no temporary directory outlives the command however it ends.
func cleanupDetached() {
	dir := os.Getenv(detachedEnv)
	if dir == "" {
		return
	}
	_ = os.RemoveAll(dir)
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// download fetches the asset and proves it is the one the manifest described.
// The size cap is enforced while reading, not after: a manifest that lies about
// the size must not be able to fill the disk before being caught.
func download(m update.Manifest, dir string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: checkRedirect}
	resp, err := client.Get(m.URL)
	if err != nil {
		return "", fmt.Errorf("downloading the update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading the update: %s", resp.Status)
	}

	path := filepath.Join(dir, "update.zip")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, m.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("downloading the update: %w", err)
	}
	if n != m.Size {
		return "", fmt.Errorf("the download is %d bytes, the manifest says %d", n, m.Size)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != m.SHA256 {
		return "", fmt.Errorf("the download does not match the manifest\n  expected sha256 %s\n  got            %s", m.SHA256, got)
	}
	return path, nil
}

// unpack extracts the bundle with ditto. Not unzip, and not archive/zip: a
// bundle carries symlinks and extended attributes that both of those drop, and
// the result fails to launch in a way that looks exactly like a signing fault.
func unpack(zip, dir string) (string, error) {
	out := filepath.Join(dir, "unpacked")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	if _, err := run("/usr/bin/ditto", "-x", "-k", zip, out); err != nil {
		return "", fmt.Errorf("unpacking the update: %w", err)
	}
	apps, err := filepath.Glob(filepath.Join(out, "*.app"))
	if err != nil {
		return "", err
	}
	if len(apps) != 1 {
		return "", fmt.Errorf("the download holds %d app bundles, expected exactly one", len(apps))
	}
	return apps[0], nil
}

// verify is the step this whole command exists for.
//
// The hash proves the download is the file the manifest described. It does not
// prove the manifest is honest, so the signature is checked too — and not only
// that it is valid, but that it belongs to the same team as the app being
// replaced. spctl passing means somebody Apple knows notarized this; the team
// check is what makes it *us*.
func verify(fresh, target string, m update.Manifest) error {
	if out, err := run("/usr/bin/codesign", "--verify", "--strict", "--verbose=2", fresh); err != nil {
		return fmt.Errorf("the downloaded app is not intact:\n%s", indent(out))
	}
	if out, err := run("/usr/sbin/spctl", "-a", "-t", "exec", "-vv", fresh); err != nil {
		return fmt.Errorf("macOS will not accept the downloaded app:\n%s", indent(out))
	}
	freshTeam, err := teamID(fresh)
	if err != nil {
		return err
	}
	current, err := teamID(target)
	if err != nil {
		// An unsigned bundle in place is a build from source. There is nothing
		// to compare against, and quietly skipping the comparison would defeat
		// it — so say what happened instead of assuming.
		return fmt.Errorf("cannot read the signature of the installed app (%w)\n"+
			"  A build from source is not updated over the air; reinstall from a release first.", err)
	}
	if freshTeam != current {
		return fmt.Errorf("the update is signed by team %s, the installed app by %s — refusing", freshTeam, current)
	}
	// The two apps ship side by side and each follows its own channel, so a
	// manifest naming the wrong zip would otherwise turn one into the other at
	// the other's path — an "Agent Pet (dev)" that is really the release build,
	// with the release build's port and data directory. Cheap to check, and
	// exactly the mistake a release cut by hand can make.
	freshID, err := bundleID(fresh)
	if err != nil {
		return err
	}
	installedID, err := bundleID(target)
	if err != nil {
		return err
	}
	if freshID != installedID {
		return fmt.Errorf("the update is %s and the installed app is %s — that is the other application, not an update", freshID, installedID)
	}
	if installedID != flavor.Current().BundleID {
		return fmt.Errorf("this petctl belongs to %s but the app it sits in is %s — the bundle was branded wrong",
			flavor.Current().BundleID, installedID)
	}

	got, err := bundleVersion(fresh)
	if err != nil {
		return err
	}
	if got != m.Version {
		return fmt.Errorf("the manifest offers %s but the download contains %s", m.Version, got)
	}
	return nil
}

func bundleID(app string) (string, error) {
	return plistString(app, "CFBundleIdentifier")
}

// teamID is the Apple Developer team the bundle was signed by. codesign writes
// this to stderr, which is why the output is read combined.
func teamID(app string) (string, error) {
	out, err := run("/usr/bin/codesign", "-dv", "--verbose=4", app)
	if err != nil {
		return "", fmt.Errorf("codesign: %s", strings.TrimSpace(firstLine(out)))
	}
	id := parseTeamID(out)
	if id == "" {
		return "", fmt.Errorf("no team identifier in the signature of %s", app)
	}
	return id, nil
}

func parseTeamID(codesignOutput string) string {
	for _, line := range strings.Split(codesignOutput, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "TeamIdentifier="); ok {
			v = strings.TrimSpace(v)
			if v == "" || v == "not set" {
				return ""
			}
			return v
		}
	}
	return ""
}

func bundleVersion(app string) (string, error) {
	return plistString(app, "CFBundleShortVersionString")
}

func plistString(app, key string) (string, error) {
	out, err := run("/usr/libexec/PlistBuddy", "-c", "Print :"+key,
		filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		return "", fmt.Errorf("cannot read %s of %s: %s", key, app, strings.TrimSpace(firstLine(out)))
	}
	return strings.TrimSpace(out), nil
}

// checkMacOS refuses an update this machine cannot run. Installing it would
// replace a working app with one that will not launch.
func checkMacOS(m update.Manifest) error {
	if m.MinMacOS == "" {
		return nil
	}
	min, ok := update.Normalize(m.MinMacOS)
	if !ok {
		return fmt.Errorf("the manifest asks for macOS %q, which is not a version", m.MinMacOS)
	}
	out, err := run("/usr/bin/sw_vers", "-productVersion")
	if err != nil {
		return fmt.Errorf("cannot read the macOS version: %w", err)
	}
	have, ok := update.Normalize(strings.TrimSpace(out))
	if !ok {
		return fmt.Errorf("cannot read the macOS version (%q)", strings.TrimSpace(out))
	}
	if update.Compare(have, min) < 0 {
		return fmt.Errorf("%s needs macOS %s and this machine runs %s", m.Version, m.MinMacOS, strings.TrimSpace(out))
	}
	return nil
}

func reachable(c *client) bool {
	var h struct {
		Version string `json:"version"`
	}
	return c.do(http.MethodGet, "/healthz", nil, &h) == nil
}

// quit asks the app to quit through its own menu item, and then waits for the
// event port to actually close. The wait is the point: a stale petd holding
// the port is what makes a rebuild look like it did nothing.
func quit(c *client) error {
	if err := c.do(http.MethodPost, "/window", map[string]string{"status_item": "quit"}, nil); err != nil {
		// The request not returning cleanly is expected — the process is on
		// its way out. Whether it worked is decided by the port, below.
		_ = err
	}
	addr := strings.TrimPrefix(c.base, "http://")
	deadline := time.Now().Add(quitWait)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err != nil {
			return nil
		}
		conn.Close()
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("the pet is still holding %s after %s — not replacing the app underneath it\n"+
		"  Quit it from the menu bar and run this again.", addr, quitWait)
}

// swap puts the new bundle in place without ever leaving the user with no app.
//
// The old one is moved aside first and only deleted once the new one has
// landed, so a failure halfway through is recoverable — and recovered here
// rather than left for somebody to find.
func swap(fresh, target, stage string) error {
	aside := filepath.Join(stage, "previous.app")
	if err := os.Rename(target, aside); err != nil {
		return fmt.Errorf("cannot move the installed app aside: %w", err)
	}
	if err := os.Rename(fresh, target); err != nil {
		if back := os.Rename(aside, target); back != nil {
			return fmt.Errorf("could not install the update (%w) and could not put the old app back (%v)\n"+
				"  The previous version is at %s", err, back, aside)
		}
		return fmt.Errorf("could not install the update, the previous version is back in place: %w", err)
	}
	return nil
}

func launch(app string) error {
	if out, err := run("/usr/bin/open", app); err != nil {
		return fmt.Errorf("could not start %s: %s", app, strings.TrimSpace(firstLine(out)))
	}
	return nil
}

// awaitVersion waits for the new app to answer, and checks that what answered
// is the app that was just installed. Without that this would report success
// for an update that silently left another daemon running.
//
// Both halves are needed. The version alone was not enough: two copies of the
// same build answer identically, so a stray one — say a bundle left in
// build/bin by a release — passes the version check while the app that was
// actually just installed exits on the single-instance lock without a word.
// That happened, and the update said it had worked. So the daemon is asked
// which binary it is, and it has to be inside the bundle just written.
func awaitVersion(c *client, want, bundle string) (string, error) {
	deadline := time.Now().Add(launchWait)
	last, strayExe := "", ""
	for time.Now().Before(deadline) {
		var h struct {
			Version string `json:"version"`
			Exe     string `json:"exe"`
		}
		if err := c.do(http.MethodGet, "/healthz", nil, &h); err == nil {
			last = h.Version
			// An older build answers without an exe. Nothing can be checked
			// there, so the version stands on its own as it used to.
			if h.Version == want && (h.Exe == "" || within(h.Exe, bundle)) {
				return h.Version, nil
			}
			if h.Version == want && h.Exe != "" {
				strayExe = h.Exe
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if strayExe != "" {
		return "", fmt.Errorf("%s was installed, but the pet answering on the event port is a different copy:\n"+
			"  %s\n"+
			"  It holds the port, so the one just installed exited on the single-instance lock.\n"+
			"  Quit that copy and open %s.", want, strayExe, bundle)
	}
	if last != "" {
		return "", fmt.Errorf("the app was replaced but %s is answering on the event port, not %s\n"+
			"  Another petd may still be running: pkill -9 -f 'MacOS/petd' and open the app again.", last, want)
	}
	return "", fmt.Errorf("the app was replaced with %s but has not come back up — open it from /Applications", want)
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
