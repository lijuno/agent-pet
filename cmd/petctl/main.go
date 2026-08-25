// Command petctl talks to a running petd over the loopback event API.
//
// It deliberately shares no code with the engine: if petctl can drive the pet,
// so can a shell script, a git hook, or a future adapter.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lijuno/agent-pet/adapters/claude"
	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/update"
)

var version = "dev"

func main() {
	// If this process is the copy that `petctl update` made of itself to get
	// out of the bundle it is replacing, tidy that copy away now. See
	// reexecOutside.
	cleanupDetached()

	if err := flavor.Check(); err != nil {
		fmt.Fprintln(os.Stderr, "petctl:", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	addr := os.Getenv("AGENT_PET_ADDR")
	// Whether the target was named matters as much as what it is: an unnamed
	// target means "whichever pets are installed", and a named one means
	// exactly that one. See eventTargets.
	pinned := addr != ""
	if addr == "" {
		addr = flavor.Current().Addr
	}
	// A global --addr may appear anywhere.
	args, addr, flagged := extractAddr(args, addr)
	pinned = pinned || flagged

	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	c := &client{base: "http://" + addr}
	var err error
	switch args[0] {
	case "event":
		err = cmdEvent(eventTargets(c, pinned), args[1:])
	case "test":
		err = cmdTest(c, args[1:])
	case "status":
		err = cmdStatus(c, args[1:])
	case "doctor":
		err = cmdDoctor(c)
	case "pets":
		err = cmdPets(c)
	case "pet":
		err = cmdPet(c, args[1:])
	case "watch":
		err = cmdWatch(c)
	case "update":
		err = cmdUpdate(c, args[1:])
	case "install", "uninstall":
		err = cmdInstall(args)
	case "hook":
		err = cmdHook(eventTargets(c, pinned), args[1:])
	case "version", "--version", "-v":
		fmt.Println("petctl", version)
	case "help", "--help", "-h":
		usage()
	default:
		err = fmt.Errorf("unknown command %q (try `petctl help`)", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "petctl:", err)
		os.Exit(1)
	}
}

func extractAddr(args []string, addr string) ([]string, string, bool) {
	out := args[:0:0]
	found := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--addr" && i+1 < len(args):
			addr = args[i+1]
			found = true
			i++
		case strings.HasPrefix(args[i], "--addr="):
			addr = strings.TrimPrefix(args[i], "--addr=")
			found = true
		default:
			out = append(out, args[i])
		}
	}
	return out, addr, found
}

// eventTargets is every pet an event should reach.
//
// Both apps can be installed and running at once — that is the whole point of
// shipping them separately (ADR 0008) — and both are watching the same agent,
// so both should react. Comparing a dev build against the release one while
// working is the reason somebody installs the dev build at all, and it needs no
// configuration to happen.
//
// The one that is not running costs nothing: a refused connection on loopback
// returns immediately, and every caller here already treats failure as normal.
//
// A named target overrides all of it. Asking for one pet gets one pet.
func eventTargets(c *client, pinned bool) []*client {
	if pinned {
		return []*client{c}
	}
	out := make([]*client, 0, len(flavor.All()))
	for _, f := range flavor.All() {
		out = append(out, &client{base: "http://" + f.Addr})
	}
	return out
}

func usage() {
	fmt.Print(`petctl — control the desktop pet

Usage:
  petctl event <source> <event> [--session ID] [--meta k=v ...]
  petctl test <state|clear> [--for DURATION]
  petctl status [--json]
  petctl doctor
  petctl pets
  petctl pet <id>
  petctl watch
  petctl update [--check] [--json]
  petctl install claude [--project|--global]
  petctl uninstall claude [--project|--global]
  petctl version

Options:
  --addr HOST:PORT   one petd to talk to ($AGENT_PET_ADDR). Without it, events
                     go to every installed pet: Agent Pet listens on 9876 and
                     Agent Pet (dev) on 9877, and both watch the same agent.

Updating:
  update            install a newer version of this app, if there is one
  update --check    only say whether one exists
  --channel dev     look at what the other app is on. It cannot install it:
                    Agent Pet and Agent Pet (dev) are two applications, and
                    they update from their own channels. See "petctl doctor".

  Updates are verified against Apple's notarization, must be signed by the same
  team as the app they replace, and must carry the same bundle identifier. A
  build from source is never replaced.

States:
  idle thinking working attention confused worried happy celebrate sleeping heart

Events:
  session_started session_ended thinking_started working idle
  tool_started tool_finished tool_failed permission_requested user_input_requested
  task_completed task_failed tests_started tests_passed tests_failed
  git_commit error heartbeat

Adapters:
  install writes hooks into .claude/settings.json (--global for ~/.claude).
  "petctl hook claude" is what those hooks run; it is not meant to be typed.

Examples:
  petctl install claude
  petctl event claude permission_requested
  petctl event codex task_completed --session abc123
  petctl event claude tool_started --meta tool=bash
  petctl test celebrate --for 10s
  petctl update --check
`)
}

// ---- client ----

type client struct {
	base string
}

func (c *client) do(method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach petd at %s — is it running? (%w)", c.base, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, e.Error)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ---- commands ----

func cmdEvent(targets []*client, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: petctl event <source> <event> [--session ID] [--meta k=v ...]")
	}
	body := map[string]any{"source": args[0], "event": args[1]}
	meta := map[string]any{}
	rest := args[2:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--session":
			if i+1 >= len(rest) {
				return fmt.Errorf("--session needs a value")
			}
			body["session_id"] = rest[i+1]
			i++
		case "--meta":
			if i+1 >= len(rest) {
				return fmt.Errorf("--meta needs k=v")
			}
			k, v, ok := strings.Cut(rest[i+1], "=")
			if !ok {
				return fmt.Errorf("--meta expects k=v, got %q", rest[i+1])
			}
			meta[k] = v
			i++
		default:
			return fmt.Errorf("unexpected argument %q", rest[i])
		}
	}
	if len(meta) > 0 {
		body["metadata"] = meta
	}
	// Delivered to every pet that is listening, not just the first. Both apps
	// watch the same agent, so an event belongs to both of them; the ones that
	// are not running refuse the connection immediately and cost nothing.
	delivered := 0
	var firstErr error
	for _, t := range targets {
		var out struct {
			Known bool   `json:"known"`
			State string `json:"state"`
		}
		if err := t.do(http.MethodPost, "/event", body, &out); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delivered++
		who := ""
		if len(targets) > 1 {
			who = "  (" + t.base + ")"
		}
		if !out.Known {
			fmt.Printf("accepted (unknown event type %q — recorded as activity, no state change) → %s%s\n", args[1], out.State, who)
			continue
		}
		fmt.Printf("%s %s → %s%s\n", args[0], args[1], out.State, who)
	}
	if delivered == 0 {
		return firstErr
	}
	return nil
}

func cmdTest(c *client, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: petctl test <state|clear> [--for DURATION]")
	}
	if args[0] == "clear" {
		if err := c.do(http.MethodPost, "/test", map[string]any{"clear": true}, nil); err != nil {
			return err
		}
		fmt.Println("forced state cleared")
		return nil
	}
	body := map[string]any{"state": args[0]}
	for i := 1; i < len(args); i++ {
		if args[i] == "--for" && i+1 < len(args) {
			body["duration"] = args[i+1]
			i++
		}
	}
	var out struct {
		State string `json:"state"`
		For   string `json:"for"`
	}
	if err := c.do(http.MethodPost, "/test", body, &out); err != nil {
		return err
	}
	fmt.Printf("pet is %s for %s\n", out.State, out.For)
	return nil
}

type snapshot struct {
	Snapshot struct {
		State    string    `json:"state"`
		Since    time.Time `json:"since"`
		Reason   string    `json:"reason"`
		Source   string    `json:"source"`
		Forced   bool      `json:"forced"`
		Sessions []struct {
			Key struct {
				Source string `json:"source"`
				ID     string `json:"id"`
			} `json:"key"`
			State    string `json:"state"`
			Duration int64  `json:"duration_ns"`
			Idle     int64  `json:"idle_ns"`
			LastTool string `json:"last_tool"`
		} `json:"sessions"`
		Stats struct {
			SessionsStarted int `json:"sessions_started"`
			TasksCompleted  int `json:"tasks_completed"`
			TestsPassed     int `json:"tests_passed"`
			TestsFailed     int `json:"tests_failed"`
			Errors          int `json:"errors"`
			Commits         int `json:"commits"`
			Interactions    int `json:"interactions"`
			EventsSeen      int `json:"events_seen"`
		} `json:"stats"`
	} `json:"snapshot"`
	Pet    string `json:"pet"`
	Bubble *struct {
		Text string `json:"text"`
	} `json:"bubble"`
}

func cmdStatus(c *client, args []string) error {
	if len(args) > 0 && args[0] == "--json" {
		var raw json.RawMessage
		if err := c.do(http.MethodGet, "/state", nil, &raw); err != nil {
			return err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			return err
		}
		fmt.Println(pretty.String())
		return nil
	}
	var s snapshot
	if err := c.do(http.MethodGet, "/state", nil, &s); err != nil {
		return err
	}
	forced := ""
	if s.Snapshot.Forced {
		forced = "  (forced by petctl test)"
	}
	fmt.Printf("Pet:   %s\nState: %s%s\n", s.Pet, s.Snapshot.State, forced)
	if !s.Snapshot.Since.IsZero() {
		fmt.Printf("For:   %s\n", time.Since(s.Snapshot.Since).Round(time.Second))
	}
	if s.Bubble != nil && s.Bubble.Text != "" {
		fmt.Printf("Says:  %q\n", s.Bubble.Text)
	}
	fmt.Println()
	if len(s.Snapshot.Sessions) == 0 {
		fmt.Println("No active sessions.")
	} else {
		fmt.Println("Sessions:")
		for _, sess := range s.Snapshot.Sessions {
			tool := ""
			if sess.LastTool != "" {
				tool = "  last tool: " + sess.LastTool
			}
			fmt.Printf("  %-22s %-10s up %s, quiet %s%s\n",
				sess.Key.Source+"/"+sess.Key.ID,
				sess.State,
				time.Duration(sess.Duration).Round(time.Second),
				time.Duration(sess.Idle).Round(time.Second),
				tool)
		}
	}
	st := s.Snapshot.Stats
	fmt.Printf("\nThis run: %d events, %d sessions, %d tasks, %d tests passed, %d failed, %d commits, %d errors\n",
		st.EventsSeen, st.SessionsStarted, st.TasksCompleted, st.TestsPassed, st.TestsFailed, st.Commits, st.Errors)
	return nil
}

type diagnostics struct {
	Version           string            `json:"version"`
	Uptime            string            `json:"uptime"`
	Addr              string            `json:"addr"`
	ConfigPath        string            `json:"config_path"`
	DataDir           string            `json:"data_dir"`
	DataWrite         string            `json:"data_writable"`
	ActivePet         string            `json:"active_pet"`
	Animations        int               `json:"animations"`
	MissingAnimations []string          `json:"missing_animations"`
	Pets              []string          `json:"pets"`
	Sessions          int               `json:"sessions"`
	EventsSeen        int               `json:"events_seen"`
	State             string            `json:"state"`
	Integrations      map[string]string `json:"integrations"`
	Desktop           map[string]string `json:"desktop"`
	Update            update.Status     `json:"update"`
}

func cmdDoctor(c *client) error {
	fmt.Println("Agent Pet Doctor")
	fmt.Println()

	var d diagnostics
	if err := c.do(http.MethodGet, "/diagnostics", nil, &d); err != nil {
		fmt.Printf("  %s petd not reachable at %s\n", cross, strings.TrimPrefix(c.base, "http://"))
		// The apps section first, before giving up: "not reachable" and "you
		// are asking the wrong one of the two" look identical otherwise, and
		// the second is easy to arrive at once both are installed.
		reportApps()
		fmt.Println()
		fmt.Println("  Start it with:  petd")
		return fmt.Errorf("petd unreachable")
	}

	fmt.Printf("  %s petd running (%s, up %s) on %s\n", tick, d.Version, d.Uptime, d.Addr)
	if d.DataWrite == "yes" {
		fmt.Printf("  %s data directory writable: %s\n", tick, d.DataDir)
	} else {
		fmt.Printf("  %s data directory: %s\n", cross, d.DataWrite)
	}
	fmt.Printf("  %s config: %s\n", tick, d.ConfigPath)
	reportApps()
	fmt.Println()

	fmt.Println("  Pet")
	if d.ActivePet == "" {
		fmt.Printf("    %s no pet loaded\n", cross)
	} else {
		fmt.Printf("    %s %s loaded\n", tick, d.ActivePet)
		fmt.Printf("    %s %d animations available\n", tick, d.Animations)
		if len(d.MissingAnimations) > 0 {
			fmt.Printf("    %s missing: %s (falls back to a related state)\n", warn, strings.Join(d.MissingAnimations, ", "))
		}
		fmt.Printf("    · available packs: %s\n", strings.Join(d.Pets, ", "))
	}
	fmt.Println()

	names := make([]string, 0, len(d.Integrations))
	for k := range d.Integrations {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Println("  Integrations")
	for _, n := range names {
		status := d.Integrations[n]
		mark := warn
		switch {
		case strings.HasPrefix(status, "events received"):
			mark = tick
		case status == "disabled":
			mark = "·"
		}
		fmt.Printf("    %s %-8s %s\n", mark, n, status)
	}
	if len(d.Desktop) > 0 {
		fmt.Println()
		fmt.Println("  Window")
		keys := make([]string, 0, len(d.Desktop))
		for k := range d.Desktop {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mark := "·"
			if k == "menu_bar" {
				mark = tick
				if strings.Contains(d.Desktop[k], "not installed") || strings.Contains(d.Desktop[k], "hidden") {
					mark = cross
				}
			}
			fmt.Printf("    %s %-16s %s\n", mark, k, d.Desktop[k])
		}
	}

	reportAdapters()
	reportUpdates(d.Update)

	fmt.Println()
	fmt.Printf("  Current state: %s   sessions: %d   events this run: %d\n", d.State, d.Sessions, d.EventsSeen)
	return nil
}

// reportAdapters answers "are the hooks actually installed?", which petd cannot
// know: it is a fact about ~/.claude and .claude, not about the engine.
func reportAdapters() {
	fmt.Println()
	fmt.Println("  Claude Code hooks")
	for _, scope := range []struct {
		label  string
		global bool
	}{{"project", false}, {"global", true}} {
		path, err := settingsPath(scope.global)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Printf("    · %-8s not installed (%s)\n", scope.label, path)
			continue
		}
		if err != nil {
			fmt.Printf("    %s %-8s cannot read %s: %v\n", cross, scope.label, path, err)
			continue
		}
		n, err := claude.Installed(data)
		switch {
		case err != nil:
			fmt.Printf("    %s %-8s %s is not valid JSON\n", cross, scope.label, path)
		case n == 0:
			fmt.Printf("    · %-8s not installed (%s)\n", scope.label, path)
		case n == len(claude.Hooks):
			fmt.Printf("    %s %-8s %d hooks installed\n", tick, scope.label, n)
		default:
			// A partial install is the interesting failure: a hand-edited file
			// leaves the pet blind to whichever events went missing.
			fmt.Printf("    %s %-8s only %d of %d hooks installed — run `petctl install claude` again\n",
				warn, scope.label, n, len(claude.Hooks))
		}
	}
}

// reportApps lists both applications: which are installed, which are running.
//
// This exists because they are menu-bar-only. An app with no Dock icon and no
// entry in the app switcher is one you can forget you installed, and two of
// them show two identical icons in the menu bar. Something has to be able to
// answer "what have I actually got".
func reportApps() {
	fmt.Println()
	fmt.Println("  Apps")
	for _, f := range flavor.All() {
		here := ""
		if f.ID == flavor.Current().ID {
			here = "  ← this petctl"
		}
		path, installed := findApp(f)

		var running, version string
		var h struct {
			Version string `json:"version"`
			Exe     string `json:"exe"`
		}
		if err := (&client{base: "http://" + f.Addr}).do(http.MethodGet, "/healthz", nil, &h); err == nil {
			running, version = "running on "+f.Addr, h.Version
		}

		switch {
		case running != "":
			fmt.Printf("    %s %-18s %-12s %s%s\n", tick, f.AppName, version, running, here)
			if installed {
				fmt.Printf("      %-18s %s\n", "", path)
			}
			// Which copy is answering, when it is not the one just named. The
			// port is held by whichever started first, so a build left in a
			// worktree can be the pet you are looking at while doctor points
			// at /Applications — and with both at the same version there is
			// nothing else to tell them apart. Older builds do not report an
			// exe; nothing to compare is not a mismatch.
			if h.Exe != "" && installed && !within(h.Exe, path) {
				fmt.Printf("      %-18s %s answering from %s\n", "", warn, h.Exe)
				fmt.Printf("      %-18s %s\n", "", "quit it and open the installed copy")
			}
		case installed:
			fmt.Printf("    · %-18s %-12s installed, not running%s\n", f.AppName, "", here)
			fmt.Printf("      %-18s %s\n", "", path)
		default:
			fmt.Printf("    · %-18s %-12s not installed%s\n", f.AppName, "", here)
		}
	}
}

// findApp looks where an app is actually kept. Deliberately the same two
// locations, in the same order, as the plugin's petctl shim searches.
func findApp(f flavor.Flavor) (string, bool) {
	var candidates []string
	if p := os.Getenv("AGENT_PET_APP"); p != "" && strings.HasSuffix(p, f.AppFile()) {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, filepath.Join("/Applications", f.AppFile()))
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", f.AppFile()))
	}
	for _, p := range candidates {
		if st, err := os.Stat(filepath.Join(p, "Contents", "MacOS", "petctl")); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// reportUpdates says what the pet knows about newer versions, and — just as
// important — what it does not. petd never checks for itself, so an empty
// answer here means no check has run, not that there is nothing to find.
func reportUpdates(st update.Status) {
	cfg, _ := config.Load(config.Path())
	fmt.Println()
	fmt.Println("  Updates")

	f := flavor.Current()
	fmt.Printf("    · %-16s %s — this is %s, and the other channel is the other app\n",
		"channel", f.Channel, f.AppName)

	if cfg.Update.Check {
		fmt.Printf("    %s %-16s every %s, from the Claude Code session hook\n",
			tick, "automatic", cfg.Update.Interval.D())
	} else {
		fmt.Printf("    · %-16s off — nothing here contacts the network until you run\n", "automatic")
		fmt.Printf("      %-16s `petctl update --check`, or set update.check in %s\n", "", config.Path())
	}

	switch {
	case version == update.DevBuild:
		fmt.Printf("    · %-16s a dev build is never replaced over the air\n", "this build")
	case st.Error != "":
		fmt.Printf("    %s %-16s %s\n", cross, "last check", st.Error)
	case st.Available:
		fmt.Printf("    %s %-16s %s is available (you have %s)\n", warn, "update", st.Latest, st.Current)
		if st.NotesURL != "" {
			fmt.Printf("      %-16s %s\n", "", st.NotesURL)
		}
		fmt.Printf("      %-16s install it with `petctl update`\n", "")
	case st.Latest != "" && update.Compare(st.Current, st.Latest) > 0:
		fmt.Printf("    · %-16s you have %s; this channel is on %s\n", "ahead", st.Current, st.Latest)
	case st.Latest != "":
		fmt.Printf("    %s %-16s %s is the newest on this channel\n", tick, "up to date", st.Latest)
	default:
		fmt.Printf("    · %-16s no check has run yet\n", "latest")
	}
	if !st.CheckedAt.IsZero() {
		fmt.Printf("    · %-16s %s ago\n", "last checked", time.Since(st.CheckedAt).Round(time.Second))
	}
}

const (
	tick  = "✓"
	cross = "✗"
	warn  = "⚠"
)

func cmdPets(c *client) error {
	var out struct {
		Active string `json:"active"`
		Pets   []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Builtin bool   `json:"builtin"`
			Dir     string `json:"dir"`
		} `json:"pets"`
	}
	if err := c.do(http.MethodGet, "/pets", nil, &out); err != nil {
		return err
	}
	for _, p := range out.Pets {
		marker := " "
		if p.ID == out.Active {
			marker = "*"
		}
		origin := "built-in"
		if !p.Builtin {
			origin = p.Dir
		}
		fmt.Printf("%s %-14s %-16s %s\n", marker, p.ID, p.Name, origin)
	}
	return nil
}

func cmdPet(c *client, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: petctl pet <id>")
	}
	var out struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.do(http.MethodPost, "/pet", map[string]any{"id": args[0]}, &out); err != nil {
		return err
	}
	fmt.Printf("switched to %s (%s)\n", out.Name, out.ID)
	return nil
}

// cmdWatch tails the SSE stream. Useful when developing an adapter: run it in
// one pane and watch states change as hooks fire.
func cmdWatch(c *client) error {
	resp, err := http.Get(c.base + "/stream")
	if err != nil {
		return fmt.Errorf("cannot reach petd at %s — is it running? (%w)", c.base, err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var s snapshot
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &s); err != nil {
			continue
		}
		say := ""
		if s.Bubble != nil && s.Bubble.Text != "" {
			say = fmt.Sprintf("  %q", s.Bubble.Text)
		}
		reason := s.Snapshot.Reason
		if s.Snapshot.Source != "" {
			reason = s.Snapshot.Source + " " + reason
		}
		fmt.Printf("%s  %-10s %s%s\n", time.Now().Format("15:04:05"), s.Snapshot.State, reason, say)
	}
	return sc.Err()
}
