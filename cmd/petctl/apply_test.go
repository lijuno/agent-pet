package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lijuno/agent-pet/internal/update"
)

// app writes a fake bundle whose Info.plist says which one it is, so a test can
// tell the installed app from the downloaded one after a swap.
func app(t *testing.T, path, marker string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Contents", "Info.plist"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func marker(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(path, "Contents", "Info.plist"))
	if err != nil {
		t.Fatalf("no bundle at %s: %v", path, err)
	}
	return string(b)
}

func TestSwapInstallsTheNewBundle(t *testing.T) {
	dir := t.TempDir()
	target := app(t, filepath.Join(dir, "agent-pet.app"), "installed")
	fresh := app(t, filepath.Join(dir, "stage", "agent-pet.app"), "downloaded")

	if err := swap(fresh, target); err != nil {
		t.Fatal(err)
	}
	if got := marker(t, target); got != "downloaded" {
		t.Errorf("the installed app is %q, not the one just downloaded", got)
	}
	if _, err := os.Stat(target + ".previous"); !os.IsNotExist(err) {
		t.Error("a successful update left the old bundle beside the new one")
	}
}

func TestSwapPutsTheOldAppBackWhenTheInstallFails(t *testing.T) {
	dir := t.TempDir()
	target := app(t, filepath.Join(dir, "agent-pet.app"), "installed")

	err := swap(filepath.Join(dir, "nothing-here.app"), target)
	if err == nil {
		t.Fatal("installing a bundle that is not there succeeded")
	}
	if got := marker(t, target); got != "installed" {
		t.Errorf("the app in place is %q; the one that was working should be back", got)
	}
	if _, err := os.Stat(target + ".previous"); !os.IsNotExist(err) {
		t.Error("the restored app was also left lying beside itself")
	}
}

// The regression this file exists for. When the install fails *and* the restore
// fails, what was moved aside is the only copy of the app left on the machine.
// It used to be moved into the staging directory, which apply deletes on its
// way out however it ends — so the error told the user where their app was and
// then it was deleted.
func TestSwapKeepsTheOnlyRemainingAppOutOfTheStagingDirectory(t *testing.T) {
	dir := t.TempDir()
	target := app(t, filepath.Join(dir, "agent-pet.app"), "installed")
	fresh := app(t, filepath.Join(dir, "stage", "agent-pet.app"), "downloaded")
	stage := filepath.Join(dir, "stage")

	// Fail every rename but the first, which is the one that takes the app away.
	calls := 0
	old := rename
	t.Cleanup(func() { rename = old })
	rename = func(from, to string) error {
		calls++
		if calls == 1 {
			return old(from, to)
		}
		return errors.New("no")
	}

	err := swap(fresh, target)
	if err == nil {
		t.Fatal("two failed renames reported success")
	}

	// apply always does this, whatever swap returned.
	if rmErr := os.RemoveAll(stage); rmErr != nil {
		t.Fatal(rmErr)
	}

	aside := target + ".previous"
	if got := marker(t, aside); got != "installed" {
		t.Fatalf("the app that was working is %q, if it is there at all", got)
	}
	if !strings.Contains(err.Error(), aside) {
		t.Errorf("the error does not say where the app went:\n%s", err)
	}
}

// A run that died between the two renames leaves one of these behind, and a
// rename onto an existing directory fails. The next update must not be stuck
// because of it.
func TestSwapClearsWhatACrashedRunLeftBehind(t *testing.T) {
	dir := t.TempDir()
	target := app(t, filepath.Join(dir, "agent-pet.app"), "installed")
	app(t, target+".previous", "from a run that died")
	fresh := app(t, filepath.Join(dir, "stage", "agent-pet.app"), "downloaded")

	if err := swap(fresh, target); err != nil {
		t.Fatalf("a leftover copy blocked the update: %v", err)
	}
	if got := marker(t, target); got != "downloaded" {
		t.Errorf("the installed app is %q", got)
	}
}

// healthd answers /healthz the way a running petd does, so awaitVersion can be
// asked what it makes of a given answer.
func healthd(t *testing.T, version, exe string) *client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": version, "exe": exe})
	}))
	t.Cleanup(ts.Close)
	return &client{base: ts.URL}
}

// impatient shortens the wait for the two outcomes the deadline decides.
func impatient(t *testing.T) {
	t.Helper()
	old := launchWait
	launchWait = 300 * time.Millisecond
	t.Cleanup(func() { launchWait = old })
}

func TestAwaitVersionAcceptsTheAppItJustInstalled(t *testing.T) {
	bundle := "/Applications/agent-pet.app"
	c := healthd(t, "1.2.3", bundle+"/Contents/MacOS/petd")
	got, err := awaitVersion(c, "1.2.3", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.2.3" {
		t.Errorf("reported %q", got)
	}
}

// The failure this exists for: a second copy of the same build answers the
// port, so the version matches, while the app just installed exited on the
// single-instance lock without a word. The update used to report success.
func TestAwaitVersionRejectsAnotherCopyHoldingThePort(t *testing.T) {
	impatient(t)
	stray := "/Users/someone/agent-pet/build/bin/agent-pet.app/Contents/MacOS/petd"
	c := healthd(t, "1.2.3", stray)
	_, err := awaitVersion(c, "1.2.3", "/Applications/agent-pet.app")
	if err == nil {
		t.Fatal("a copy outside the installed bundle was accepted")
	}
	if !strings.Contains(err.Error(), stray) {
		t.Errorf("the error does not name the copy that is answering:\n%s", err)
	}
}

// An older build answers without an exe, and then the version is all there is.
func TestAwaitVersionTrustsAnOlderBuildThatCannotSayWhereItIs(t *testing.T) {
	c := healthd(t, "1.2.3", "")
	if _, err := awaitVersion(c, "1.2.3", "/Applications/agent-pet.app"); err != nil {
		t.Fatal(err)
	}
}

func TestAwaitVersionSaysWhatIsActuallyAnswering(t *testing.T) {
	impatient(t)
	c := healthd(t, "0.9.0", "/Applications/agent-pet.app/Contents/MacOS/petd")
	_, err := awaitVersion(c, "1.2.3", "/Applications/agent-pet.app")
	if err == nil {
		t.Fatal("the old version was accepted as the new one")
	}
	if !strings.Contains(err.Error(), "0.9.0") {
		t.Errorf("the error does not say which version answered:\n%s", err)
	}
}

// Nothing on the port means the app let go of it, which is all quit waits for.
func TestQuitReturnsOnceThePortIsFree(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- quit(&client{base: "http://" + addr}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("quit waited for a port nobody holds: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quit did not notice the port was free")
	}
}

func TestTargetBundleChecksWhatItWasPointedAt(t *testing.T) {
	if _, err := targetBundle("/tmp/not-a-bundle"); err == nil {
		t.Error("--app accepted something that is not a bundle")
	}
	abs, err := targetBundle("agent-pet.app")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("--app %q stayed relative; the swap happens somewhere else entirely", abs)
	}
}

// Both branches that answer before sw_vers runs, which is the only part of
// this a machine without macOS can reach.
func TestCheckMacOSReadsTheManifestBeforeTheMachine(t *testing.T) {
	if err := checkMacOS(update.Manifest{}); err != nil {
		t.Errorf("a manifest asking for no particular macOS was refused: %v", err)
	}
	err := checkMacOS(update.Manifest{MinMacOS: "sequoia"})
	if err == nil {
		t.Fatal("a manifest asking for macOS \"sequoia\" was accepted")
	}
	if !strings.Contains(err.Error(), "sequoia") {
		t.Errorf("the error does not quote what the manifest said:\n%s", err)
	}
}

func TestDownloadReportsAServerThatSaidNo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer ts.Close()
	_, err := download(update.Manifest{URL: ts.URL, Size: 10}, t.TempDir())
	if err == nil {
		t.Fatal("a 404 was treated as an update")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("the error does not say what the server answered:\n%s", err)
	}
}

// The copy that runs from outside the bundle has to be executable, or the
// re-exec fails on a file that is right there.
func TestCopyFileKeepsTheModeItIsGiven(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "petctl")
	if err := os.WriteFile(src, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy")
	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("the copy is %v, so it cannot be exec'd", st.Mode().Perm())
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "binary" {
		t.Errorf("the copy holds %q (%v)", b, err)
	}
}
