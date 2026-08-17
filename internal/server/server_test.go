package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lijuno/agent-digital-pet/internal/config"
	"github.com/lijuno/agent-digital-pet/internal/engine"
	"github.com/lijuno/agent-digital-pet/internal/petassets"
)

const manifest = `{"id":"test","name":"Test","frame_width":40,"frame_height":40,
 "animations":{"idle":{"file":"idle.png","frames":4,"fps":3,"loop":true}}}`

func newTestServer(t *testing.T) *httptest.Server {
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
	return ts
}

func post(t *testing.T, ts *httptest.Server, path, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestPostEventDrivesState(t *testing.T) {
	ts := newTestServer(t)
	resp, body := post(t, ts, "/event", `{"source":"claude","event":"tool_started","metadata":{"tool":"bash"}}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Known bool   `json:"known"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Known || out.State != "working" {
		t.Fatalf("want a known event and the working state, got %+v", out)
	}
}

func TestUnknownEventIsAcceptedNotRejected(t *testing.T) {
	ts := newTestServer(t)
	resp, body := post(t, ts, "/event", `{"source":"future","event":"warp_engaged"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("an unknown event type must still be accepted (§6), got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"known":false`) {
		t.Fatalf("the response should say the event was not understood: %s", body)
	}
}

func TestUnknownFieldsRejected(t *testing.T) {
	ts := newTestServer(t)
	resp, body := post(t, ts, "/event", `{"source":"claude","event":"working","comand":"rm -rf /"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a typo'd field should be reported, not ignored; got %d: %s", resp.StatusCode, body)
	}
}

func TestMissingEventRejected(t *testing.T) {
	ts := newTestServer(t)
	if resp, _ := post(t, ts, "/event", `{"source":"claude"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestOversizedBodyRejected(t *testing.T) {
	ts := newTestServer(t)
	big := `{"source":"claude","event":"working","metadata":{"x":"` + strings.Repeat("A", MaxBody*2) + `"}}`
	resp, _ := post(t, ts, "/event", big)
	if resp.StatusCode < 400 {
		t.Fatalf("an oversized body should be refused, got %d", resp.StatusCode)
	}
}

func TestForeignOriginRejected(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/event", strings.NewReader(`{"source":"x","event":"working"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a page on another origin must not be able to drive the pet, got %d", resp.StatusCode)
	}
}

// The guard compares hosts, not prefixes. Every rejected case below starts with
// a string the pet legitimately accepts, and each one is a name an attacker can
// register and resolve to 127.0.0.1.
func TestOriginHostIsMatchedExactly(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:9876",
		"http://127.0.0.1",
		"http://localhost:9876",
		"http://[::1]:9876",
		"wails://wails",
		"WAILS://WAILS",
	}
	refused := []string{
		"http://localhost.evil.com",
		"http://127.0.0.1.evil.com",
		"http://localhost@evil.com",
		"https://localhost",
		"http://evil.com",
		"null",
		"",
	}
	for _, o := range allowed {
		if !isLocalOrigin(o) {
			t.Errorf("%q is this machine and must be allowed", o)
		}
	}
	for _, o := range refused {
		if isLocalOrigin(o) {
			t.Errorf("%q is not this machine and must be refused", o)
		}
	}
}

func TestLookalikeOriginRejected(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/event", strings.NewReader(`{"source":"x","event":"working"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost.evil.com")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a host that merely begins with localhost must not be able to drive the pet, got %d", resp.StatusCode)
	}
}

func TestWrongMethodRejected(t *testing.T) {
	ts := newTestServer(t)
	resp, err := ts.Client().Get(ts.URL + "/event")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("want 405, got %d", resp.StatusCode)
	}
}

func TestNonJSONContentTypeRejected(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/event", strings.NewReader("source=claude&event=working"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestMetadataScalarsAreStringified(t *testing.T) {
	ts := newTestServer(t)
	resp, body := post(t, ts, "/event",
		`{"source":"claude","event":"tool_started","metadata":{"tool":"bash","exit":0,"ok":true,"nested":{"a":1}}}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	r, err := ts.Client().Get(ts.URL + "/state")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	snap := got["snapshot"].(map[string]any)
	meta, _ := snap["meta"].(map[string]any)
	if meta["tool"] != "bash" || meta["exit"] != "0" || meta["ok"] != "true" {
		t.Fatalf("scalars should be flattened to strings, got %v", meta)
	}
	if _, ok := meta["nested"]; ok {
		t.Fatalf("structured metadata should be dropped, got %v", meta)
	}
}

func TestTestEndpointForcesAndClears(t *testing.T) {
	ts := newTestServer(t)
	resp, body := post(t, ts, "/test", `{"state":"celebrate","duration":"5s"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "celebrate") {
		t.Fatalf("body %s", body)
	}
	if resp, body := post(t, ts, "/test", `{"state":"banana"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an invalid state should be rejected with the valid list, got %d %s", resp.StatusCode, body)
	}
	if resp, _ := post(t, ts, "/test", `{"clear":true}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("clear should succeed, got %d", resp.StatusCode)
	}
}

func TestTestEndpointRejectsAbsurdDuration(t *testing.T) {
	ts := newTestServer(t)
	if resp, _ := post(t, ts, "/test", `{"state":"happy","duration":"400h"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestDiagnosticsReportsHonestIntegrationStatus(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client().Get(ts.URL + "/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var d Diagnostics
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		t.Fatal(err)
	}
	if d.ActivePet != "test" || d.Animations != 1 {
		t.Fatalf("unexpected pet diagnostics: %+v", d)
	}
	if len(d.MissingAnimations) == 0 {
		t.Fatal("a pack with one animation should report the missing ones")
	}
	// Having received nothing, the only honest answer is that nothing has
	// arrived — never a green tick inferred from an adapter being installed (§29).
	if got := d.Integrations["claude"]; !strings.Contains(got, "no events yet") {
		t.Fatalf("claude integration status should be honest, got %q", got)
	}
}

func TestSwitchPet(t *testing.T) {
	ts := newTestServer(t)
	if resp, body := post(t, ts, "/pet", `{"id":"nope"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown pet should 400, got %d %s", resp.StatusCode, body)
	}
	if resp, body := post(t, ts, "/pet", `{"id":"test"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
}

func TestStreamEmitsCurrentStateImmediately(t *testing.T) {
	ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/stream", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	if !bytes.Contains(buf[:n], []byte("event: state")) {
		t.Fatalf("a new subscriber should get the current state at once, got %q", buf[:n])
	}
}

func TestListenRefusesNonLoopback(t *testing.T) {
	lib := petassets.NewLibrary()
	_ = lib.LoadBuiltin(fstest.MapFS{"pets/test/manifest.json": {Data: []byte(manifest)}}, "pets", "/pets")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(engine.New(config.Default(), lib, log), log)

	err := s.Listen(config.Server{Addr: "0.0.0.0:9999"})
	if err == nil {
		t.Fatal("binding a non-loopback address must be refused by default (§26)")
	}
	if !strings.Contains(err.Error(), "allow_non_loopback") {
		t.Fatalf("the error should say how to override it: %v", err)
	}
}
