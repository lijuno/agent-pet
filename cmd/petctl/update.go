package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/flavor"
	"github.com/lijuno/agent-pet/internal/update"
)

// The updater lives here, in petctl, and not in petd.
//
// That is the point of putting it here: petd holds no HTTP client and runs no
// subprocess, and SECURITY.md says so. This program is where the network call,
// the signature checks and the bundle swap happen, and it is a separate binary
// that petd cannot import. See docs/adr/0008-over-the-air-updates.md.
//
// This is also the one place petctl reads config.yaml. Everywhere else it
// drives the pet over HTTP and shares nothing with the engine, deliberately —
// but which channel to follow and where the manifest lives are settings, not
// state, and they have to be readable when petd is not running at all.

// manifestTimeout bounds the check. The manifest is a few hundred bytes; if it
// has not arrived by now the network is not going to produce it.
const manifestTimeout = 10 * time.Second

// maxManifest bounds what will be read from the manifest URL, which is a server
// nobody in this process controls.
const maxManifest = 64 << 10

type updateOpts struct {
	checkOnly bool
	jsonOut   bool
	quiet     bool
	channel   update.Channel
	app       string
}

func cmdUpdate(c *client, args []string) error {
	var o updateOpts
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--check":
			o.checkOnly = true
		case a == "--json":
			o.jsonOut = true
		case a == "--quiet":
			// The background check started by a session hook. It says nothing
			// unless asked: the pet is ambient and this runs while somebody is
			// working.
			o.quiet, o.checkOnly = true, true
		case a == "--channel" && i+1 < len(args):
			i++
			ch, ok := update.ParseChannel(args[i])
			if !ok {
				return fmt.Errorf("unknown channel %q (release or dev)", args[i])
			}
			o.channel = ch
		case strings.HasPrefix(a, "--channel="):
			ch, ok := update.ParseChannel(strings.TrimPrefix(a, "--channel="))
			if !ok {
				return fmt.Errorf("unknown channel %q (release or dev)", a)
			}
			o.channel = ch
		case a == "--app" && i+1 < len(args):
			i++
			o.app = args[i]
		default:
			return fmt.Errorf("unknown option %q for `petctl update`", a)
		}
	}
	return runUpdate(c, o)
}

func runUpdate(c *client, o updateOpts) error {
	cfg, _ := config.Load(config.Path())
	mine := flavor.Current().Channel

	// The channel is this build's, not a setting. --channel looks at the other
	// one, and can only look: installing it would mean replacing this app with
	// a different application, which is a thing to do by installing that
	// application (ADR 0008).
	ch := o.channel
	if ch == "" {
		ch = mine
	}
	foreign := ch != mine
	if foreign {
		o.checkOnly = true
	}
	murl, err := update.ManifestURL(cfg.Update.ManifestURL, ch)
	if err != nil {
		return err
	}

	m, found, err := fetchManifest(murl, ch)
	if err != nil {
		// Tell petd the check failed rather than leaving yesterday's answer
		// standing. "I could not find out" and "there is nothing new" are
		// different, and the pet does not blur them.
		if !foreign {
			report(c, update.Status{Channel: ch, Current: version, Error: err.Error()})
		}
		if o.quiet {
			return nil
		}
		return err
	}

	st := update.Status{Channel: ch, Current: version, CheckedAt: time.Now()}
	if found {
		st.Latest = m.Version
		st.NotesURL = m.NotesURL
		st.Available = update.Newer(version, m.Version)
	}
	// Only this build's own channel is worth telling the pet about. It would
	// refuse a result for the other one anyway — a build cannot report a
	// version it could never install.
	if !foreign {
		report(c, st)
	}

	if o.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(st); err != nil {
			return err
		}
	} else if !o.quiet {
		printStatus(st, found, m)
		if foreign && found {
			fmt.Printf("  That is the %s app, a separate install. This is %s.\n", ch, flavor.Current().AppName)
		}
	}

	if o.checkOnly || !st.Available {
		return nil
	}
	return apply(c, m, o)
}

func printStatus(st update.Status, found bool, m update.Manifest) {
	switch {
	case !found:
		fmt.Printf("No %s build has been published yet.\n", st.Channel)
	case st.Available:
		fmt.Printf("Update available on the %s channel: %s (you have %s)\n", st.Channel, st.Latest, st.Current)
		if m.Published != "" {
			fmt.Printf("  published %s, %s\n", m.Published, humanSize(m.Size))
		}
		if st.NotesURL != "" {
			fmt.Printf("  %s\n", st.NotesURL)
		}
	case st.Current == update.DevBuild:
		// Not "you are up to date": this build did not come from a release and
		// is not comparable to one. Saying otherwise would be a guess.
		fmt.Printf("This is a dev build. The %s channel has %s; nothing will be installed over a build you made yourself.\n", st.Channel, m.Version)
	case update.Compare(st.Current, m.Version) > 0:
		// Not "up to date" — that would be a lie in the one direction it
		// matters. This is what somebody sees after switching from dev back to
		// release: they are ahead of the channel they just chose, and nothing
		// will happen until it passes them.
		fmt.Printf("You have %s; the %s channel is on %s.\n", st.Current, st.Channel, m.Version)
		fmt.Printf("  Nothing will be installed until the %s channel goes past what you have.\n", st.Channel)
	default:
		fmt.Printf("Up to date: %s is the newest on the %s channel.\n", st.Current, st.Channel)
	}
}

// report tells petd what the check found, so the menu bar can say so. Failure
// is ignored: petd not running is a completely ordinary state of affairs, and
// the check is still worth printing to whoever asked for it.
func report(c *client, st update.Status) {
	_ = c.do(http.MethodPost, "/update", st, nil)
}

// fetchManifest returns the manifest for a channel. A 404 is not an error: it
// means nothing has been published on that channel yet, which is the state this
// project was in on the day the updater was written.
func fetchManifest(rawURL string, ch update.Channel) (update.Manifest, bool, error) {
	client := &http.Client{Timeout: manifestTimeout, CheckRedirect: checkRedirect}
	resp, err := client.Get(rawURL)
	if err != nil {
		return update.Manifest{}, false, fmt.Errorf("cannot reach the update manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return update.Manifest{}, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return update.Manifest{}, false, fmt.Errorf("update manifest returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest))
	if err != nil {
		return update.Manifest{}, false, fmt.Errorf("reading the update manifest: %w", err)
	}
	m, err := update.ParseManifest(body, ch)
	if err != nil {
		return update.Manifest{}, false, err
	}
	return m, true, nil
}

// checkRedirect keeps a redirect chain inside GitHub. A release asset on
// github.com redirects to its storage host, which is expected; a redirect
// anywhere else means the download is no longer the one the manifest named.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing a redirect to %s", req.URL.Scheme)
	}
	if !allowedDownloadHost(req.URL.Host) {
		return fmt.Errorf("refusing a redirect to %s", req.URL.Host)
	}
	return nil
}

func allowedDownloadHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	return host == "github.com" ||
		host == "githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com")
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// bundleFor finds the .app a path sits inside. petctl ships in
// <bundle>/Contents/MacOS/petctl, so walking up from the running executable
// finds the bundle to replace — rather than guessing at /Applications, which
// would update a copy other than the one in use.
func bundleFor(exe string) (string, bool) {
	dir := filepath.Clean(exe)
	for i := 0; i < 6; i++ {
		dir = filepath.Dir(dir)
		if dir == "/" || dir == "." {
			return "", false
		}
		if strings.HasSuffix(dir, ".app") {
			return dir, true
		}
	}
	return "", false
}
