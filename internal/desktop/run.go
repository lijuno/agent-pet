package desktop

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// Version is the build version, injected into main by ldflags and handed
// down from there — the same wire as server.Version.
var Version = "dev"

// Options is what the window needs from the process around it.
type Options struct {
	// Assets is the embedded ui/dist tree. The built-in pet packs ride along
	// inside it and are served at /pets/<id>/<file> (ADR 0003).
	Assets fs.FS
	// PetsDir holds pet packs the user dropped on disk. They are not in the
	// binary, so they are served from there instead.
	PetsDir string
}

// Run opens the window and blocks until it closes. Everything Wails-shaped
// lives behind this call: main only wires the engine, the server and the log.
func Run(app *App, opts Options) error {
	cfg := app.eng.Config()

	// Only as big as the character. The window grows when a panel opens and
	// shrinks again when it closes — see App.OpenOverlay for why it must not
	// simply stay large.
	winW, winH := WindowSize(cfg.Pet.Scale)
	return wails.Run(&options.App{
		Title:  "Agent Pet",
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
				Title:   "Agent Pet",
				Message: "An ambient companion for Claude Code and Codex.\nVersion " + Version,
			},
		},

		AssetServer: &assetserver.Options{
			Assets: opts.Assets,
			// Pets the user added on disk are not in the binary, so they are
			// served here. Everything else 404s.
			Handler: userPetHandler(opts.PetsDir),
		},
		Menu:       app.appMenu(),
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []any{app},

		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.agentpet.petd",
		},
	})
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
