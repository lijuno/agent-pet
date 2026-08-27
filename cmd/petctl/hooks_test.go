package main

import (
	"path/filepath"
	"testing"
)

// The three scopes must resolve to three different files. They did not always:
// the project scope was the only one a repository could receive, and because
// the hook it writes names an absolute path to petctl on this machine, a
// repository that tracks .claude/settings.json got a dirty tree every time
// somebody installed the pet. --local is the answer, and it is only an answer
// if it is a different file.
func TestEachScopeIsItsOwnFile(t *testing.T) {
	t.Chdir(t.TempDir())

	seen := map[string]installScope{}
	for _, s := range []installScope{scopeProject, scopeLocal, scopeGlobal} {
		path, err := settingsPath(s)
		if err != nil {
			t.Fatalf("%s: %v", s.label(), err)
		}
		if other, dup := seen[path]; dup {
			t.Fatalf("%s and %s both write %s", other.label(), s.label(), path)
		}
		seen[path] = s
		if filepath.Base(filepath.Dir(path)) != ".claude" {
			t.Errorf("%s writes %s, which is not in a .claude directory", s.label(), path)
		}
	}

	local, _ := settingsPath(scopeLocal)
	if filepath.Base(local) != "settings.local.json" {
		t.Errorf("--local writes %s; git ignores settings.local.json, not that", filepath.Base(local))
	}
}

// A scope flag is not an adapter name. Reading one as the other would install
// the hooks and report success while writing them nowhere the user asked.
func TestScopeFlagsAreNotMistakenForTheAdapter(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		scope installScope
	}{
		{[]string{"claude"}, scopeProject},
		{[]string{"claude", "--project"}, scopeProject},
		{[]string{"claude", "--local"}, scopeLocal},
		{[]string{"--local", "claude"}, scopeLocal},
		{[]string{"claude", "--global"}, scopeGlobal},
	} {
		scope, target, err := parseInstallArgs("install", tc.args)
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
			continue
		}
		if target != "claude" {
			t.Errorf("%v: adapter is %q, not \"claude\"", tc.args, target)
		}
		if scope != tc.scope {
			t.Errorf("%v: scope is %s, want %s", tc.args, scope.label(), tc.scope.label())
		}
	}

	// No adapter is an error, not a silent install of the default one.
	if _, _, err := parseInstallArgs("install", []string{"--local"}); err == nil {
		t.Error("`petctl install --local` with no adapter was accepted")
	}
}
