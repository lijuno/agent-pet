package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
