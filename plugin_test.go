package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/lijuno/agent-pet/adapters/claude"
)

// The plugin ships its own hooks/hooks.json, generated from claude.Hooks by
// scripts/gen-plugin-hooks.py. Nothing at run time reconciles the two: a hook
// the plugin declares but the adapter cannot translate burns a subprocess on
// every occurrence, and one the adapter knows but the plugin omits means the
// pet quietly stops reacting to it. Neither shows up as an error anywhere, so
// the only place the drift can be caught is here.
//
// Run `make plugin-hooks` after changing claude.Hooks.

type pluginHooks struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func loadPluginHooks(t *testing.T) pluginHooks {
	t.Helper()
	b, err := os.ReadFile("plugin/hooks/hooks.json")
	if err != nil {
		t.Fatalf("reading the plugin hooks: %v", err)
	}
	var got pluginHooks
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("plugin hooks are not valid JSON: %v", err)
	}
	return got
}

func TestPluginHooksMatchAdapter(t *testing.T) {
	got := loadPluginHooks(t)

	for _, name := range claude.Hooks {
		if _, ok := got.Hooks[name]; !ok {
			t.Errorf("adapter handles %s but the plugin does not declare it — run `make plugin-hooks`", name)
		}
	}
	for name := range got.Hooks {
		found := false
		for _, h := range claude.Hooks {
			if h == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plugin declares %s but the adapter cannot translate it — run `make plugin-hooks`", name)
		}
	}
}

func TestPluginHookEntries(t *testing.T) {
	got := loadPluginHooks(t)

	for name, groups := range got.Hooks {
		if len(groups) != 1 {
			t.Errorf("%s: want 1 group, got %d", name, len(groups))
			continue
		}
		inner := groups[0].Hooks
		if len(inner) != 1 {
			t.Errorf("%s: want 1 hook, got %d", name, len(inner))
			continue
		}
		h := inner[0]
		if h.Type != "command" {
			t.Errorf("%s: type is %q, want \"command\"", name, h.Type)
		}
		// The marker `petctl uninstall` matches on, and what makes an entry
		// recognisably this adapter's.
		if !strings.Contains(h.Command, "hook claude") {
			t.Errorf("%s: command %q does not carry the adapter marker", name, h.Command)
		}
		// Must go through ${CLAUDE_PLUGIN_ROOT}: the bare name would resolve
		// off $PATH, which is only populated while the plugin is enabled and
		// is not guaranteed for a hook subprocess at all.
		if !strings.Contains(h.Command, "${CLAUDE_PLUGIN_ROOT}") {
			t.Errorf("%s: command %q does not resolve through ${CLAUDE_PLUGIN_ROOT}", name, h.Command)
		}
		if h.Timeout != claude.HookTimeout {
			t.Errorf("%s: timeout is %d, want claude.HookTimeout (%d)", name, h.Timeout, claude.HookTimeout)
		}
	}
}
