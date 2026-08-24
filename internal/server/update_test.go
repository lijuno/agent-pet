package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/lijuno/agent-pet/internal/config"
	"github.com/lijuno/agent-pet/internal/engine"
	"github.com/lijuno/agent-pet/internal/petassets"
	"github.com/lijuno/agent-pet/internal/update"
)

// newUpdateServer hands back the Server as well as the test server: the channel
// is a build fact, and a test asserting what happens to a result for the *other*
// channel needs to know which one this build is.
func newUpdateServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	lib := petassets.NewLibrary()
	fsys := fstest.MapFS{"pets/test/manifest.json": {Data: []byte(manifest)}}
	if err := lib.LoadBuiltin(fsys, "pets", "/pets"); err != nil {
		t.Fatalf("pets: %v", err)
	}
	cfg := config.Default()
	cfg.Pet.Active = "test"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, lib, log)
	s := New(eng, log)
	ts := httptest.NewServer(s.withGuards(s.mux))
	t.Cleanup(ts.Close)
	return ts, s
}

func getUpdate(t *testing.T, ts *httptest.Server) update.Status {
	t.Helper()
	resp, err := http.Get(ts.URL + "/update")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st update.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

const found = `{"latest":"9.9.9","available":true,
 "notes_url":"https://github.com/lijuno/agent-pet/releases/tag/v9.9.9"}`

func TestUpdateStartsEmptyAndRemembersWhatItIsTold(t *testing.T) {
	ts, _ := newUpdateServer(t)

	if st := getUpdate(t, ts); st.Available || st.Latest != "" {
		t.Fatalf("petd invented an update before anyone told it one: %+v", st)
	}
	if st := getUpdate(t, ts); st.Channel != update.Release {
		t.Errorf("channel = %q, want release", st.Channel)
	}

	resp, _ := post(t, ts, "/update", found)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post: %s", resp.Status)
	}
	st := getUpdate(t, ts)
	if !st.Available || st.Latest != "9.9.9" {
		t.Fatalf("the posted result was not kept: %+v", st)
	}
	if st.CheckedAt.IsZero() {
		t.Error("no time was recorded for the check")
	}
}

// A build follows one channel and cannot be moved to the other — the other
// channel is a different application. So a result for it is refused outright
// rather than stored: there is no later moment at which it becomes true.
func TestAResultForTheOtherChannelIsRefused(t *testing.T) {
	ts, s := newUpdateServer(t)
	other := update.Dev
	if s.channel() == update.Dev {
		other = update.Release
	}
	resp, body := post(t, ts, "/update",
		`{"channel":"`+string(other)+`","latest":"9.9.9","available":true}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a %s result was accepted by the %s build: %s %s", other, s.channel(), resp.Status, body)
	}
	if st := getUpdate(t, ts); st.Available {
		t.Fatal("it was stored anyway")
	}
}

// Every other route on this listener treats its input as hostile and so does
// this one. What is posted here reaches a menu-bar title and a browser.
func TestUpdateRejectsHostileInput(t *testing.T) {
	ts, _ := newUpdateServer(t)
	bad := []string{
		`{"latest":"<script>alert(1)</script>","available":true}`,
		`{"latest":"9.9.9","available":true,"notes_url":"javascript:alert(1)"}`,
		`{"latest":"9.9.9","available":true,"notes_url":"https://evil.test/x"}`,
		`{"latest":"9.9.9","available":true,"channel":"nightly"}`,
		`{"available":true}`,
		`{"latest":"9.9.9","available":true,"install":"/bin/sh"}`,
	}
	for _, body := range bad {
		resp, _ := post(t, ts, "/update", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s -> %s, want 400", body, resp.Status)
		}
	}
	if st := getUpdate(t, ts); st.Available {
		t.Fatalf("a rejected post still changed what petd reports: %+v", st)
	}
}

// The daemon reports its own version rather than echoing whatever the caller
// claimed it was running.
func TestPostedCurrentVersionIsIgnored(t *testing.T) {
	ts, _ := newUpdateServer(t)
	post(t, ts, "/update", `{"current":"0.0.1","latest":"9.9.9","available":true}`)
	if st := getUpdate(t, ts); st.Current != Version {
		t.Errorf("current = %q, want %q", st.Current, Version)
	}
}

func TestUpdateNotifiesTheDesktop(t *testing.T) {
	ts, s := newUpdateServer(t)
	got := make(chan update.Status, 1)
	s.OnUpdate = func(st update.Status) { got <- st }

	post(t, ts, "/update", found)
	select {
	case st := <-got:
		if st.Latest != "9.9.9" {
			t.Errorf("the menu bar was told %q", st.Latest)
		}
	default:
		t.Fatal("the menu bar was never told")
	}
}

func TestUpdateAppearsInDiagnostics(t *testing.T) {
	ts, _ := newUpdateServer(t)
	post(t, ts, "/update", found)

	resp, err := http.Get(ts.URL + "/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var d Diagnostics
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if !d.Update.Available || d.Update.Latest != "9.9.9" {
		t.Fatalf("doctor would not see the update: %+v", d.Update)
	}
}

func TestUpdateRejectsOtherMethods(t *testing.T) {
	ts, _ := newUpdateServer(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/update", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /update -> %s, want 405", resp.Status)
	}
}
