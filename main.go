// Command petd is the agent pet: event API, state machine and desktop window
// in one process (ADR 0001).
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/desktop"
	"github.com/lijuno/agent-pet/internal/engine"
	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/petassets"
	"github.com/lijuno/agent-pet/internal/server"
)

// The built-in pet packs ship inside the binary under ui/dist/pets/ and are
// served by the Wails asset server at /pets/<id>/<file> (ADR 0003).
//
//go:embed all:ui/dist
var assets embed.FS

var version = "dev"

// startUpdateCheck asks petctl to look for a newer version, once, in the
// background.
//
// petd does not do the looking. It has no HTTP client and never gains one: the
// code that reaches the network lives in cmd/petctl, a main package this
// program cannot import even by accident, and that is what keeps "the daemon
// opens no outbound connections" a fact about the build rather than a habit
// (ADR 0008).
//
// What it does now is spawn — one known binary, from inside its own bundle,
// with fixed arguments. That claim is the one being spent here, and it buys the
// answer to "am I up to date?" being right the first time somebody looks
// instead of after the next Claude Code session. petctl decides whether a check
// is due at all: --if-due honours update.check and the same daily throttle the
// session hook uses, so opening the app five times is not five checks.
//
// Detached and never waited for. A network call must not hold up a pet.
func startUpdateCheck(log *slog.Logger) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	petctl := filepath.Join(filepath.Dir(exe), "petctl")
	if _, err := os.Stat(petctl); err != nil {
		// A build run from `go run` has no petctl beside it. Nothing to do.
		return
	}
	cmd := exec.Command(petctl, "update", "--if-due")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		log.Warn("could not start the update check", "err", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

func main() {
	var (
		headless   = flag.Bool("headless", false, "run the daemon without the window (useful for testing adapters)")
		addrFlag   = flag.String("addr", "", "override the event API address")
		showVer    = flag.Bool("version", false, "print version and exit")
		showFlavor = flag.Bool("flavor", false, "print this build's identity as JSON and exit")
		configFlag = flag.String("config", "", "path to config.yaml")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("petd", version)
		return
	}
	// How scripts/brand.sh learns the bundle identifier and name to stamp into
	// Info.plist. Asking the binary rather than repeating the values in shell
	// keeps internal/flavor the only place they exist — and proves the build
	// flags actually took effect, which a second copy of the values could not.
	if *showFlavor {
		f := flavor.Current()
		out, err := json.Marshal(map[string]string{
			"id": f.ID, "slug": f.Slug, "app_name": f.AppName,
			"bundle_id": f.BundleID, "addr": f.Addr,
			"channel": string(f.Channel), "version": version,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "petd:", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
		return
	}

	// A build stamped with a flavor that does not exist would take the release
	// app's port, config and data directory. Refuse rather than collide.
	if err := flavor.Check(); err != nil {
		fmt.Fprintln(os.Stderr, "petd:", err)
		os.Exit(1)
	}

	cfgPath := *configFlag
	if cfgPath == "" {
		cfgPath = config.Path()
	}
	cfg, cfgErr := config.Load(cfgPath)
	if *addrFlag != "" {
		cfg.Server.Addr = *addrFlag
	}

	log, closeLog := newLogger(cfg)
	defer closeLog()
	if cfgErr != nil {
		log.Warn("config", "err", cfgErr)
	}

	lib := petassets.NewLibrary()
	if err := lib.LoadBuiltin(assets, "ui/dist/pets", "/pets"); err != nil {
		log.Error("loading built-in pets", "err", err)
	}
	if err := lib.LoadDir(config.PetsDir(), "/userpets"); err != nil {
		log.Warn("loading user pets", "err", err)
	}
	if lib.Len() == 0 {
		fmt.Fprintln(os.Stderr, "petd: no pet assets found — the binary was built without ui/dist/pets")
		os.Exit(1)
	}
	// pet.disabled keeps packs out of the pickers. Two things it is not
	// allowed to do, both of them for the same reason — a menu has to agree
	// with the window:
	//
	// It cannot hide the character on screen. Whatever `active` names stays in
	// the list, ticked, however the config lists it; taking that one out of
	// the menu means changing `active` as well.
	//
	// And it cannot hide all of them. A Change Pet submenu with no entries is
	// a bug report rather than a preference, so a list that empties the
	// library is dropped and said out loud.
	lib.Disable(exceptActive(cfg.Pet.Disabled, cfg.Pet.Active))
	if len(lib.List()) == 0 {
		log.Warn("pet.disabled would hide every character; ignoring it",
			"disabled", cfg.Pet.Disabled)
		lib.Disable(nil)
	}

	eng := engine.New(cfg, lib, log)
	server.Version = version
	desktop.Version = version
	srv := server.New(eng, log)
	// Wails runs this binary to generate bindings; that pass must not take the
	// port, or building while the pet is running fails.
	if !desktop.GeneratingBindings {
		if err := srv.Listen(cfg.Server); err != nil {
			fmt.Fprintln(os.Stderr, "petd:", err)
			fmt.Fprintln(os.Stderr, "  (another petd may already be running — try `petctl status`)")
			// Say it where it can be seen. Launched from the Finder there is no
			// stderr anywhere, so this failure looked exactly like an app that
			// declined to open for no reason — and the usual cause is a copy of
			// the pet still running from somewhere else, which is precisely
			// what happens when somebody replaces the bundle by hand without
			// quitting it first.
			if !*headless {
				desktop.Alert(flavor.Current().AppName+" is already running",
					"Another copy has the event port ("+cfg.Server.Addr+"), so this one cannot start.\n\n"+
						"Quit it from the menu bar and open this one again. `petctl doctor` says which copy is running.")
			}
			os.Exit(1)
		}
		go func() {
			if err := srv.Serve(); err != nil {
				log.Error("event api stopped", "err", err)
			}
		}()
		log.Info("petd started", "version", version, "addr", srv.Addr(), "pets", lib.Len())
		startUpdateCheck(log)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	if *headless {
		runHeadless(ctx, cancel, srv, log)
		return
	}

	app := desktop.NewApp(eng, log, cfgPath, srv.Addr())
	// Let `petctl doctor` see the window and the menu bar. The server has no
	// idea either exists; this is the only wire between them.
	srv.Desktop = app.DesktopDiagnostics
	srv.Panel = app.OpenPanel
	srv.MoveWindow = app.MoveWindow
	srv.StatusItem = app.ClickStatusItem
	srv.StatusMenu = app.StatusMenu
	srv.SetShown = app.SetShown
	// How a check reaches the menu bar. petd is told; it never asks.
	srv.OnUpdate = app.SetUpdate
	// What the last check found, from before this process existed. petd is told
	// results and holds them in memory, so without this the menu says "no
	// update check yet" after every restart — and an update is a restart.
	//
	// Through the server, so /update and the menu bar cannot disagree.
	if st, ok := desktop.LoadUpdate(); ok {
		srv.RestoreUpdate(st)
	}

	if err := desktop.Run(app, desktop.Options{
		Assets:  assets,
		PetsDir: config.PetsDir(),
	}); err != nil {
		log.Error("window", "err", err)
		fmt.Fprintln(os.Stderr, "petd:", err)
		os.Exit(1)
	}
	_ = srv.Close()
}

func runHeadless(ctx context.Context, cancel context.CancelFunc, srv *server.Server, log *slog.Logger) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	fmt.Println("petd running headless on", srv.Addr(), "— Ctrl-C to stop")
	select {
	case <-sig:
	case <-ctx.Done():
	}
	cancel()
	_ = srv.Close()
	log.Info("petd stopped")
}

// newLogger writes concise lines to both stderr and a log file. §32: event
// categories only, no prompts, no source, no command arguments.

// exceptActive is pet.disabled with the character on screen taken out of it.
func exceptActive(disabled []string, active string) []string {
	out := make([]string, 0, len(disabled))
	for _, id := range disabled {
		if id != active {
			out = append(out, id)
		}
	}
	return out
}

func newLogger(cfg config.Config) (*slog.Logger, func()) {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Logging.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var w io.Writer = os.Stderr
	closer := func() {}
	if err := os.MkdirAll(config.LogsDir(), 0o755); err == nil {
		path := filepath.Join(config.LogsDir(), "petd.log")
		// Keep the log small; this is a pet, not an audit trail.
		if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
			_ = os.Rename(path, path+".1")
		}
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			w = io.MultiWriter(os.Stderr, f)
			closer = func() { _ = f.Close() }
		}
	}

	h := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.String("t", a.Value.Time().Format("15:04:05"))
			}
			return a
		},
	})
	return slog.New(h), closer
}
