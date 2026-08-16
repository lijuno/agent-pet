package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// marker identifies the hook entries this adapter owns. Uninstall matches on
// it, so removal never touches a hook the user wrote. It is part of the command
// itself rather than a side-car field because Claude Code owns the schema of a
// hook entry and an unrecognised key there is not ours to invent.
const marker = "hook claude"

// HookTimeout bounds the hook from Claude Code's side. The hook already gives
// up on its own in well under a second; this is the backstop that guarantees a
// wedged process cannot hold up the agent.
const HookTimeout = 5

// Command is the shell command written into settings.json for a given petctl.
func Command(petctlPath string) string {
	return quoteIfNeeded(petctlPath) + " " + marker
}

func quoteIfNeeded(p string) string {
	for _, r := range p {
		if r == ' ' || r == '\t' || r == '"' {
			b, _ := json.Marshal(p)
			return string(b)
		}
	}
	return p
}

// Install returns settings.json with this adapter's hooks added, preserving
// every other key and every hook the user configured themselves (§28).
//
// It works on the decoded JSON tree rather than a typed struct on purpose: a
// struct round-trip would silently drop any setting this build has never heard
// of, which is a destructive way to edit somebody else's configuration file.
//
// Install is idempotent — it removes its own entries first, so running it twice
// leaves one copy, and re-running after moving the binary updates the path.
func Install(settings []byte, petctlPath string) ([]byte, error) {
	root, err := parse(settings)
	if err != nil {
		return nil, err
	}
	removeOurs(root)

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	entry := map[string]any{
		"type":    "command",
		"command": Command(petctlPath),
		"timeout": HookTimeout,
	}
	for _, name := range Hooks {
		// No matcher: these fire for every tool and every notification type.
		// A matcher would be a filter the pet has no reason to apply.
		group := map[string]any{"hooks": []any{entry}}
		existing, _ := hooks[name].([]any)
		hooks[name] = append(existing, group)
	}
	root["hooks"] = hooks
	return render(root)
}

// Uninstall removes this adapter's hooks and nothing else. A settings file that
// held only these hooks comes back byte-identical to one that never had them:
// empty groups, empty event lists and an empty hooks object are all pruned, so
// uninstalling does not leave a husk behind.
func Uninstall(settings []byte) ([]byte, error) {
	root, err := parse(settings)
	if err != nil {
		return nil, err
	}
	removeOurs(root)
	return render(root)
}

// Installed reports how many of this adapter's hooks are present, which is what
// `petctl doctor` reports rather than a bare yes/no: a half-installed file
// (hand-edited, or a failed write) should look wrong, not fine.
func Installed(settings []byte) (int, error) {
	root, err := parse(settings)
	if err != nil {
		return 0, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	n := 0
	for _, name := range Hooks {
		list, _ := hooks[name].([]any)
		for _, g := range list {
			group, _ := g.(map[string]any)
			inner, _ := group["hooks"].([]any)
			for _, h := range inner {
				if isOurs(h) {
					n++
				}
			}
		}
	}
	return n, nil
}

func parse(settings []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(settings))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(settings, &root); err != nil {
		// Refusing here is the point: silently replacing a file we failed to
		// understand would destroy configuration the user cares about.
		return nil, fmt.Errorf("settings file is not valid JSON (%w) — fix or move it, this will not overwrite it", err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func render(root map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// removeOurs strips every hook entry carrying the marker, then prunes whatever
// that leaves empty.
func removeOurs(root map[string]any) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for name, v := range hooks {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		keptGroups := make([]any, 0, len(list))
		for _, g := range list {
			group, ok := g.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			inner, ok := group["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, g)
				continue
			}
			kept := make([]any, 0, len(inner))
			for _, h := range inner {
				if !isOurs(h) {
					kept = append(kept, h)
				}
			}
			if len(kept) == 0 {
				// The whole group was ours; drop it rather than leave an empty
				// matcher sitting in the file.
				continue
			}
			group["hooks"] = kept
			keptGroups = append(keptGroups, group)
		}
		if len(keptGroups) == 0 {
			delete(hooks, name)
			continue
		}
		hooks[name] = keptGroups
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
}

func isOurs(h any) bool {
	entry, ok := h.(map[string]any)
	if !ok {
		return false
	}
	cmd, _ := entry["command"].(string)
	return strings.Contains(cmd, marker)
}
