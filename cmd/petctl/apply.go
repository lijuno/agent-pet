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

// targetEnv carries the bundle being replaced across the re-exec.
//
// The copy runs from a temporary directory, so it cannot work the target out
// the way the original did — os.Executable no longer points inside any bundle.
// os.Args does not carry it either: the target is usually derived rather than
// named, so there is no --app in there to pass along. Without this the copy
// gets as far as the download and then refuses its own work, saying the very
// thing that is true of it and beside the point: that it is not inside an app.
const targetEnv = "AGENT_PET_UPDATE_TARGET"

// detachedGrace is how long a copy is left alone before the sweep will take it.
// Longer than an update takes, so a second update running concurrently is never
// the thing that gets swept.
const detachedGrace = 30 * time.Minute

// quitWait is how long the running app is given to let go of the event port.
// Replacing the bundle while the old daemon still holds it produces the exact
// failure CLAUDE.md warns about: the update appears to do nothing, because the
// old process is still answering.
const quitWait = 15 * time.Second

// launchWait is how long the new app is given to come up and report its
// version. Notarized first launches are slower than later ones — Gatekeeper is
// checking the signature.
//
// A var only so the tests can shorten it: two of the three ways this can end
// are decided by the deadline expiring, and thirty seconds each is not a test
// anybody runs.
var launchWait = 30 * time.Second

func apply(c *client, m update.Manifest, o updateOpts) error {
	// The copy this may be running from is needed until the work is done — see
	// cleanupDetached for what deleting it early cost.
	defer releaseDetached()

	if version == update.DevBuild {
		return fmt.Errorf("this is a dev build; refusing to replace it with %s", m.Version)
	}

	target, err := resolveTarget(o.app)
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
	if err := swap(fresh, target); err != nil {
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
// three together are what the Pet Status panel reads as "up to date", and what
// keeps the menu bar from offering the version that was just installed. Getting
// any of them wrong puts a phantom update in the menu — CheckedAt in
// particular, because a zero time there means "nobody has ever checked", which
// is what the panel said after an update until this existed.
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
// resolveTarget names the bundle to replace: what was asked for, or what an
// earlier pass through reexecOutside carried across for the copy, which cannot
// see it from where it now stands.
func resolveTarget(app string) (string, error) {
	if app == "" {
		app = os.Getenv(targetEnv)
	}
	return targetBundle(app)
}

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
	env := append(os.Environ(), detachedEnv+"="+dir, targetEnv+"="+target)
	// Exec, not Start: the copy inherits the terminal and the exit code, so
	// this stays one command from the caller's point of view.
	return syscall.Exec(dst, os.Args, env)
}

// cleanupDetached sweeps copies left behind by earlier runs of `petctl update`.
// Called at startup, and deliberately not on this run's own copy.
//
// It used to delete exactly that, on the reasoning that unlinking a running
// executable is fine. It is fine for running the program and fatal for talking
// to the network: on macOS the TLS verifier asks Security.framework to build an
// SSL policy, that call verifies the calling process's code signature, and it
// cannot verify an image that has been unlinked. The manifest fetch then fails
// with `tls: ... SecPolicyCreateSSL error: 0`, which broke over-the-air updates
// for every installation whose petctl lives inside the bundle it is replacing —
// that is, all of them.
//
// So this run's copy goes at the end of the run instead, in releaseDetached,
// and anything a crash left behind is swept here on the next one.
func cleanupDetached() {
	sweepDetached(os.TempDir(), os.Getenv(detachedEnv), time.Now())
}

// sweepDetached removes stale copies under dir, never `mine`, and never one
// young enough to belong to an update running right now — two of these can be
// in flight if somebody starts a second one, and taking the executable out from
// under it would be the very bug this function exists because of.
func sweepDetached(dir, mine string, now time.Time) {
	matches, err := filepath.Glob(filepath.Join(dir, "petctl-update-*"))
	if err != nil {
		return
	}
	for _, d := range matches {
		if mine != "" && filepath.Clean(d) == filepath.Clean(mine) {
			continue
		}
		st, err := os.Stat(d)
		if err != nil || now.Sub(st.ModTime()) < detachedGrace {
			continue
		}
		_ = os.RemoveAll(d)
	}
}

// releaseDetached removes this run's own copy, once nothing needs it any more.
func releaseDetached() {
	if dir := os.Getenv(detachedEnv); dir != "" {
		_ = os.RemoveAll(dir)
	}
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

// rename is a seam, and the only reason it exists is the path below where two
// renames fail in a row. That path decides whether the user still has an app,
// and there is no way to make a second rename fail from outside this function.
var rename = os.Rename

// swap puts the new bundle in place without ever leaving the user with no app.
//
// The old one is moved aside first and only deleted once the new one has
// landed, so a failure halfway through is recoverable — and recovered here
// rather than left for somebody to find.
//
// Beside the target rather than inside the staging directory, which is where it
// used to go. apply removes that directory on the way out whatever happened, so
// when both the install and the restore failed — the one case where the copy
// moved aside is the only copy left — the error named a path that was deleted
// a moment later, and the user was left with no app at all and instructions
// pointing at nothing.
func swap(fresh, target string) error {
	aside := target + ".previous"
	// An earlier run that died between the two renames leaves one of these, and
	// a rename onto an existing directory fails.
	_ = os.RemoveAll(aside)
	if err := rename(target, aside); err != nil {
		return fmt.Errorf("cannot move the installed app aside: %w", err)
	}
	if err := rename(fresh, target); err != nil {
		if back := rename(aside, target); back != nil {
			return fmt.Errorf("could not install the update (%w) and could not put the old app back (%v)\n"+
				"  Your app is at %s — rename it back to %s", err, back, aside, target)
		}
		return fmt.Errorf("could not install the update, the previous version is back in place: %w", err)
	}
	_ = os.RemoveAll(aside)
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
