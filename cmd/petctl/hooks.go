package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/lijunix/agent-digital-pet/adapters/claude"
)

// maxHookInput bounds what a hook will read from stdin. Hook payloads are a few
// hundred bytes; this only exists so a pathological transcript cannot make the
// hook the slow part of somebody's tool call.
const maxHookInput = 1 << 20

// hookTimeout is deliberately short. A pet is ambient: if petd is not running,
// or is wedged, the correct behaviour is to give up immediately and let the
// agent get on with its work.
const hookTimeout = 750 * time.Millisecond

// cmdHook is the adapter entry point, run by Claude Code with the hook payload
// on stdin.
//
// It returns nil in every circumstance and writes nothing to stdout. Both are
// load-bearing: a non-zero exit would show the user an error about a cartoon
// cat in the middle of their work, and stdout beginning with `{` is parsed by
// Claude Code as a decision object, which is emphatically not ours to send.
func cmdHook(c *client, args []string) error {
	if len(args) == 0 || args[0] != "claude" {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookInput))
	if err != nil {
		return nil
	}
	ev, ok := claude.Translate(data)
	if !ok {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return nil
	}
	req, err := http.NewRequest(http.MethodPost, c.base+"/event", bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: hookTimeout}).Do(req)
	if err != nil {
		// petd is not running. That is a completely normal state of affairs.
		return nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return nil
}

// settingsPath resolves which settings.json to edit.
func settingsPath(global bool) (string, error) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, ".claude", "settings.json"), nil
}

func cmdInstall(args []string) error {
	verb := args[0]
	rest := args[1:]

	global := false
	target := ""
	for _, a := range rest {
		switch a {
		case "--global":
			global = true
		case "--project":
			global = false
		default:
			if target != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			target = a
		}
	}
	switch target {
	case "claude":
	case "":
		return fmt.Errorf("usage: petctl %s claude [--project|--global]", verb)
	case "codex":
		// §29: say what is true rather than pretend.
		return fmt.Errorf("the Codex adapter is Milestone 3 and is not built yet")
	default:
		return fmt.Errorf("unknown adapter %q (only `claude` exists today)", target)
	}

	path, err := settingsPath(global)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if verb == "uninstall" && os.IsNotExist(err) {
		fmt.Printf("nothing to remove: %s does not exist\n", path)
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine the path to petctl: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}

	var patched []byte
	if verb == "install" {
		patched, err = claude.Install(current, self)
	} else {
		patched, err = claude.Uninstall(current)
	}
	if err != nil {
		return err
	}

	// Keep a copy of anything we are about to change. Editing somebody's
	// configuration file is the most destructive thing petctl does.
	if len(current) > 0 && !bytes.Equal(current, patched) {
		if err := os.WriteFile(path+".bak", current, 0o600); err != nil {
			return fmt.Errorf("cannot write a backup next to %s: %w", path, err)
		}
	}
	if err := writeFileAtomic(path, patched); err != nil {
		return err
	}

	n, _ := claude.Installed(patched)
	if verb == "install" {
		fmt.Printf("installed %d Claude Code hooks in %s\n", n, path)
		if len(current) > 0 {
			fmt.Printf("previous file kept at %s.bak\n", path)
		}
		fmt.Println()
		fmt.Println("Start a new Claude Code session for them to take effect.")
		fmt.Println("Watch it work with:  petctl watch")
		return nil
	}
	fmt.Printf("removed the Claude Code hooks from %s\n", path)
	return nil
}

// writeFileAtomic writes through a temporary file in the same directory, so an
// interrupted write cannot leave a half-written settings.json behind.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, path)
}
