// Command petd is the digital pet: event API, state machine and desktop window
// in one process (ADR 0001).
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"github.com/lijuno/agent-digital-pet/internal/config"
	"github.com/lijuno/agent-digital-pet/internal/engine"
	"github.com/lijuno/agent-digital-pet/internal/petassets"
	"github.com/lijuno/agent-digital-pet/internal/server"
)

// The built-in pet packs ship inside the binary under ui/dist/pets/ and are
// served by the Wails asset server at /pets/<id>/<file> (ADR 0003).
//
//go:embed all:ui/dist
var assets embed.FS

var version = "dev"

func main() {
	var (
		headless   = flag.Bool("headless", false, "run the daemon without the window (useful for testing adapters)")
		addrFlag   = flag.String("addr", "", "override the event API address")
		showVer    = flag.Bool("version", false, "print version and exit")
		configFlag = flag.String("config", "", "path to config.yaml")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("petd", version)
		return
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

	eng := engine.New(cfg, lib, log)
	server.Version = version
	srv := server.New(eng, log)
	// Wails runs this binary to generate bindings; that pass must not take the
	// port, or building while the pet is running fails.
	if !generatingBindings {
		if err := srv.Listen(cfg.Server); err != nil {
			fmt.Fprintln(os.Stderr, "petd:", err)
			fmt.Fprintln(os.Stderr, "  (another petd may already be running — try `petctl status`)")
			os.Exit(1)
		}
		go func() {
			if err := srv.Serve(); err != nil {
				log.Error("event api stopped", "err", err)
			}
		}()
		log.Info("petd started", "version", version, "addr", srv.Addr(), "pets", lib.Len())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)

	if *headless {
		runHeadless(ctx, cancel, srv, log)
		return
	}

	app := NewApp(eng, log, cfgPath, srv.Addr())
	// Let `petctl doctor` see the window and the menu bar. The server has no
	// idea either exists; this is the only wire between them.
	srv.Desktop = app.DesktopDiagnostics
	srv.Panel = app.OpenPanel
	srv.MoveWindow = app.MoveWindow
	srv.StatusItem = app.ClickStatusItem
	srv.StatusMenu = app.StatusMenu
	srv.SetShown = app.SetShown
	// Only as big as the character. The window grows when a panel opens and
	// shrinks again when it closes — see App.OpenOverlay for why it must not
	// simply stay large.
	winW, winH := WindowSize(cfg.Pet.Scale)
	err := wails.Run(&options.App{
		Title:  "Digital Pet",
		Width:  winW,
		Height: winH,

		Frameless:     true,
		DisableResize: true,
		AlwaysOnTop:   cfg.Pet.AlwaysOnTop,
		StartHidden:   cfg.Window.StartHidden,

		// A fully transparent window: the webview paints nothing but the pet,
		// and the window itself has no background to paint.
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
			// Leave WindowIsTranslucent false: it adds an NSVisualEffectView
			// blur behind the webview, which would put a frosted square around
			// the pet instead of leaving the desktop visible.
			WindowIsTranslucent: false,
			DisableZoom:         true,
			About: &mac.AboutInfo{
				Title:   "Digital Pet",
				Message: "An ambient companion for Claude Code and Codex.\nVersion " + version,
			},
		},

		AssetServer: &assetserver.Options{
			Assets: assets,
			// Pets the user added on disk are not in the binary, so they are
			// served here. Everything else 404s.
			Handler: userPetHandler(config.PetsDir()),
		},
		Menu:       app.appMenu(),
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []any{app},

		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.digitalpet.petd",
		},
	})
	if err != nil {
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

// userPetHandler serves pet packs from the user's data directory.
//
// The path is validated rather than trusted: only /userpets/<id>/<file> is
// reachable, both segments must be simple names, and the result must still be
// inside the pets directory after cleaning (§26 — external input is untrusted).
func userPetHandler(petsDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/userpets/")
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		id, file, ok := strings.Cut(rest, "/")
		if !ok || !safeSegment(id) || !safeSegment(file) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		full := filepath.Join(petsDir, id, file)
		if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(petsDir)+string(os.PathSeparator)) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, full)
	})
}

func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 64 {
		return false
	}
	return !strings.ContainsAny(s, `/\`) && !strings.Contains(s, "..")
}

// newLogger writes concise lines to both stderr and a log file. §32: event
// categories only, no prompts, no source, no command arguments.
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
