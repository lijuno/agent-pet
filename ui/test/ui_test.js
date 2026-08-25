/*
 * UI tests for ui/dist/index.html.
 *
 * They run the real shipped file — loaded into an iframe, one fresh copy per
 * test — rather than a copy of its markup. A harness that restates the DOM is
 * a harness that drifts from the thing it is testing.
 *
 * No build step, no dependencies, no test runner to install: open
 * ui/test/index.html over http and the results are on the page. The UI is a
 * single static file and its tests should cost the same to run.
 *
 * The pet's script is a classic script, so its top-level `function`
 * declarations are properties of the iframe's window and its top-level
 * `const`/`let` live in the shared global lexical scope, reachable with
 * w.eval(). That is the whole seam these tests need: no source changes, no
 * exports, no module wrapper.
 */
"use strict";

const tests = [];
function test(name, fn) { tests.push({ name, fn }); }

function assert(cond, msg) {
  if (!cond) throw new Error(msg || "assertion failed");
}
function eq(got, want, msg) {
  if (got !== want) {
    throw new Error((msg ? msg + ": " : "") + `expected ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
  }
}

/* --- fixtures ------------------------------------------------------------ */

// A realistic AnimationView (app.go) — 40x40 strips are what every built-in
// pack ships.
function anim(over) {
  return Object.assign({
    resolved: "working",
    url: "/pets/sanmao/working.png",
    frames: 4,
    fps: 8,
    loop: true,
    frame_width: 40,
    frame_height: 40,
    pixelated: true,
  }, over || {});
}

function view(over) {
  return Object.assign({
    pet: "sanmao",
    pet_name: "Momo",
    scale: 1,
    on_top: true,
    animation: anim(),
    snapshot: {
      state: "working",
      since: new Date().toISOString(),
      forced: false,
      sessions: [],
      stats: {},
    },
  }, over || {});
}

function session(over) {
  return Object.assign({
    key: { source: "claude", id: "abc" },
    state: "working",
    duration_ns: 65e9,
    idle_ns: 0,
    last_tool: "Bash",
  }, over || {});
}

/* --- harness ------------------------------------------------------------- */

// withPet loads a fresh copy of the real UI and hands the test its window and
// document. A fresh frame per test keeps leaked state (an open panel, a size
// flag, a pending timer) from one test out of the next.
// Keep in step with WindowSize in app.go: the window is only as tall as the
// character until an overlay opens, because space above the pet is screen the
// pet can never be dragged to.
const WINDOW_W = 300;
const WINDOW_H = 184;
// What the window grows to with a full-height panel open.
const GROWN_H = WINDOW_H + 238;

let frameSeq = 0;

function withPet(fn, height) {
  return async function () {
    const frame = document.createElement("iframe");
    // Layout assertions are only meaningful at the size the pet ships at.
    frame.style.cssText = `width:${WINDOW_W}px;height:${height || WINDOW_H}px;border:0;position:absolute;left:-9999px;top:0`;
    // Cache-bust every load. Without this the browser happily serves a cached
    // index.html to each frame and the whole suite passes against a file that
    // is no longer on disk — which it did, until this line existed.
    frame.src = "../dist/index.html?cachebust=" + (++frameSeq) + "_" + Date.now();
    document.body.appendChild(frame);
    try {
      await new Promise((resolve, reject) => {
        frame.addEventListener("load", resolve, { once: true });
        frame.addEventListener("error", () => reject(new Error("iframe failed to load")), { once: true });
        setTimeout(() => reject(new Error("iframe load timed out")), 5000);
      });
      const w = frame.contentWindow;
      const d = frame.contentDocument;
      // boot() runs on load and awaits two backend calls that resolve to null
      // without a backend; let its microtasks drain before touching anything.
      await new Promise((r) => setTimeout(r, 0));
      await fn(w, d, frame);
    } finally {
      frame.remove();
    }
  };
}

// stubBackend installs a fake window.go and records every call. backend() reads
// window.go at call time, so installing it after load is enough.
function stubBackend(w, impl) {
  const calls = [];
  const app = new Proxy({}, {
    get(_, name) {
      if (typeof name !== "string") return undefined;
      return (...args) => {
        calls.push({ name, args });
        const f = impl && impl[name];
        return Promise.resolve(f ? f(...args) : null);
      };
    },
    has() { return true; },
  });
  w.go = { desktop: { App: app } };
  return calls;
}

function called(calls, name) {
  return calls.filter((c) => c.name === name);
}

const tick = (ms) => new Promise((r) => setTimeout(r, ms || 0));

/* --- formatting ---------------------------------------------------------- */

test("dur formats seconds, minutes and hours", withPet(async (w) => {
  eq(w.dur(0), "0s");
  eq(w.dur(45e9), "45s");
  eq(w.dur(59e9), "59s");
  eq(w.dur(60e9), "1m");
  eq(w.dur(90e9), "1m");
  eq(w.dur(3600e9), "1h 0m");
  eq(w.dur(3900e9), "1h 5m");
}));

test("dur survives missing and negative input", withPet(async (w) => {
  // Durations arrive from JSON and a nil or clock-skewed value must not render
  // as "NaNs" in the status panel.
  eq(w.dur(undefined), "0s");
  eq(w.dur(null), "0s");
  eq(w.dur(-5e9), "0s");
}));

test("describe names the source when exactly one session is running", withPet(async (w) => {
  eq(w.describe("working", [session()]), "claude is working.");
  eq(w.describe("thinking", [session()]), "claude is thinking.");
  eq(w.describe("working", []), "Working.");
}));

test("describe agrees with a plural subject", withPet(async (w) => {
  // "12 agents is working." was on screen until this test existed.
  eq(w.describe("working", [session(), session()]), "2 agents are working.");
  eq(w.describe("thinking", [session(), session(), session()]), "3 agents are thinking.");
}));

test("describe covers every state the machine can produce", withPet(async (w) => {
  // state.All() from internal/state/state.go. A state with no description
  // falls through to the idle wording, which would be a silent lie.
  const states = ["sleeping", "idle", "thinking", "working", "attention",
    "confused", "worried", "happy", "celebrate", "heart"];
  for (const s of states) {
    const got = w.describe(s, []);
    assert(typeof got === "string" && got.length > 0, `${s} has no description`);
  }
  eq(w.describe("attention", []), "Something needs you.");
  eq(w.describe("celebrate", []), "Tests passed.");
  eq(w.describe("worried", []), "Several failures in a row.");
}));

/* --- sprite rendering ---------------------------------------------------- */

test("apply sizes the sprite from the animation and scale", withPet(async (w, d) => {
  w.apply(view());
  const s = d.getElementById("sprite");
  // 40px frame at scale 1 renders at 3x (the UI's base pixel-art zoom).
  eq(s.style.width, "120px");
  eq(s.style.height, "120px");
  // The strip is one row of frames, so the background is frames-wide.
  eq(s.style.backgroundSize, "480px 120px");
  assert(s.style.backgroundImage.includes("/pets/sanmao/working.png"), "wrong sprite url");
}));

test("scale multiplies the rendered size", withPet(async (w, d) => {
  w.apply(view({ scale: 2 }));
  const s = d.getElementById("sprite");
  eq(s.style.width, "240px");
  eq(s.style.height, "240px");
}));

test("animation timing is frames over fps, stepped per frame", withPet(async (w, d) => {
  w.apply(view({ animation: anim({ frames: 6, fps: 12 }) }));
  const a = d.getElementById("sprite").style.animation;
  assert(a.includes("0.5s"), `6 frames at 12fps should last 0.5s, got ${a}`);
  assert(a.includes("steps(6)"), `should step once per frame, got ${a}`);
  assert(a.includes("infinite"), `a looping animation should repeat, got ${a}`);
}));

test("a non-looping animation plays once", withPet(async (w, d) => {
  w.apply(view({ animation: anim({ loop: false }) }));
  const a = d.getElementById("sprite").style.animation;
  assert(!a.includes("infinite"), `should not loop, got ${a}`);
}));

test("a zero-fps animation does not divide by zero", withPet(async (w, d) => {
  // fps comes from a pet pack's manifest.json, which anyone can write.
  w.apply(view({ animation: anim({ fps: 0 }) }));
  const a = d.getElementById("sprite").style.animation;
  assert(!a.includes("Infinity") && !a.includes("NaN"), `bad duration: ${a}`);
}));

test("changing state swaps the sprite", withPet(async (w, d) => {
  const s = d.getElementById("sprite");
  w.apply(view());
  const before = s.style.backgroundImage;
  w.apply(view({ animation: anim({ resolved: "celebrate", url: "/pets/sanmao/celebrate.png", frames: 6, fps: 10 }) }));
  assert(s.style.backgroundImage !== before, "sprite did not change with the state");
  assert(s.style.backgroundImage.includes("celebrate.png"), "wrong sprite after change");
}));

test("apply tolerates a view with no animation", withPet(async (w, d) => {
  // decorate() returns a View with a nil Animation when no pack is loaded.
  w.apply(view({ animation: null }));
  eq(d.getElementById("sprite").style.backgroundImage, "");
}));

test("apply ignores a null update", withPet(async (w) => {
  w.apply(null);
  w.apply(undefined);
}));

/* --- no agent connected --------------------------------------------------- */

// Claude Code exiting is the one quiet the character had no way to express:
// she looked exactly as alive as when something was running.
test("the pet greys out when no agent is connected", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "sleeping", forced: false, stats: {}, sessions: [] } }));
  assert(d.body.classList.contains("inactive"), "no sessions should read as inactive");
  const f = w.getComputedStyle(d.getElementById("sprite")).filter;
  assert(f.includes("grayscale"), `sprite should be grey, filter is ${f}`);
  // The shadow must survive: `filter` replaces, it does not add, so losing it
  // here would flatten her against the wallpaper at the same moment.
  assert(f.includes("drop-shadow"), `sprite should keep its shadow, filter is ${f}`);
}));

// --- the shadow ------------------------------------------------------------
// It lifts her off a busy wallpaper; on a light one the same shadow reads as a
// grey contour around every edge. Nothing here can see the wallpaper, so which
// of those it is doing is the user's call.
test("the shadow goes when drop_shadow is off", withPet(async (w, d) => {
  w.apply(view({ drop_shadow: false, snapshot: { state: "working", forced: false, stats: {}, sessions: [session()] } }));
  assert(d.body.classList.contains("no-shadow"), "drop_shadow: false should mark the body");
  const f = w.getComputedStyle(d.getElementById("sprite")).filter;
  assert(!f.includes("drop-shadow"), `sprite should have no shadow, filter is ${f}`);
}));

// `filter` replaces rather than adds, so the rule that drops the shadow while
// she is greyed out has to restate grayscale. Leaving it out hands her colour
// back at the exact moment she is meant to lose it.
test("a shadowless pet still greys out", withPet(async (w, d) => {
  w.apply(view({ drop_shadow: false, snapshot: { state: "sleeping", forced: false, stats: {}, sessions: [] } }));
  const f = w.getComputedStyle(d.getElementById("sprite")).filter;
  assert(f.includes("grayscale"), `sprite should still be grey, filter is ${f}`);
  assert(!f.includes("drop-shadow"), `sprite should have no shadow, filter is ${f}`);
}));

// A payload from a build that predates the setting has no such field, and an
// absent field is not the same answer as "off".
test("a view without the field keeps the shadow", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "working", forced: false, stats: {}, sessions: [session()] } }));
  assert(!d.body.classList.contains("no-shadow"), "an absent drop_shadow should not turn it off");
  const f = w.getComputedStyle(d.getElementById("sprite")).filter;
  assert(f.includes("drop-shadow"), `sprite should keep its shadow, filter is ${f}`);
}));

test("the pet has its colour while an agent is connected", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "working", forced: false, stats: {}, sessions: [session()] } }));
  assert(!d.body.classList.contains("inactive"), "a live session is not inactive");
  const f = w.getComputedStyle(d.getElementById("sprite")).filter;
  assert(!f.includes("grayscale"), `sprite should keep its colour, filter is ${f}`);
}));

test("colour comes back when an agent connects again", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "sleeping", forced: false, stats: {}, sessions: [] } }));
  assert(d.body.classList.contains("inactive"), "precondition: grey");
  w.apply(view({ snapshot: { state: "working", forced: false, stats: {}, sessions: [session()] } }));
  assert(!d.body.classList.contains("inactive"), "she should colour up when work starts");
}));

// `petctl test celebrate` exists to look at an animation. Greying out the
// thing somebody asked to look at helps nobody.
test("a forced state keeps its colour with no sessions", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "celebrate", forced: true, stats: {}, sessions: [] } }));
  assert(!d.body.classList.contains("inactive"), "a forced state should not grey out");
}));

/* --- untrusted content (§26) --------------------------------------------- */

const XSS = '<img src=x onerror="window.__pwned=true">';

test("a hostile session name cannot become markup", withPet(async (w, d) => {
  w.apply(view({
    snapshot: {
      state: "working", forced: false, stats: {},
      sessions: [session({ key: { source: XSS, id: "</span><b>bold</b>" } })],
    },
  }));
  w.togglePanel("status");
  const panel = d.getElementById("panel");
  eq(panel.querySelectorAll("img").length, 0, "an agent injected an element");
  eq(panel.querySelectorAll("b").length, 0, "an agent injected markup");
  assert(!w.__pwned, "injected script executed");
  assert(panel.textContent.includes("<img src=x"), "the name should render as literal text");
}));

test("a hostile tool name cannot become markup", withPet(async (w, d) => {
  w.apply(view({
    snapshot: {
      state: "working", forced: false, stats: {},
      sessions: [session({ last_tool: XSS })],
    },
  }));
  w.togglePanel("status");
  eq(d.getElementById("panel").querySelectorAll("img").length, 0, "tool name became an element");
  assert(!w.__pwned, "injected script executed");
}));

test("a hostile bubble cannot become markup", withPet(async (w, d) => {
  w.apply(view({ bubble: { text: XSS, ttl_ns: 5e9 } }));
  const bubble = d.getElementById("bubble");
  eq(bubble.querySelectorAll("img").length, 0, "bubble text became an element");
  assert(!w.__pwned, "injected script executed");
  eq(bubble.textContent, XSS, "bubble should show the text literally");
}));

test("a hostile pet name cannot become markup", withPet(async (w, d) => {
  // A pet pack's manifest.json is a local file, but a downloaded pack is no
  // more trusted than an agent's metadata.
  w.apply(view({ pet_name: XSS }));
  w.togglePanel("status");
  eq(d.getElementById("panel").querySelectorAll("img").length, 0, "pet name became an element");
  assert(!w.__pwned, "injected script executed");
}));

/* --- speech bubble ------------------------------------------------------- */

test("a bubble shows when there is something to say", withPet(async (w, d) => {
  w.apply(view({ bubble: { text: "Tests passed!", ttl_ns: 5e9 } }));
  const bubble = d.getElementById("bubble");
  assert(bubble.classList.contains("show"), "bubble should be visible");
  eq(bubble.textContent, "Tests passed!");
}));

test("an open panel suppresses the bubble", withPet(async (w, d) => {
  // A bubble underneath a panel is unreadable and reads as a rendering bug.
  w.togglePanel("status");
  w.apply(view({ bubble: { text: "hello", ttl_ns: 5e9 } }));
  assert(!d.getElementById("bubble").classList.contains("show"), "bubble should stay hidden behind a panel");
}));

test("opening a panel hides a bubble already on screen", withPet(async (w, d) => {
  w.apply(view({ bubble: { text: "hello", ttl_ns: 5e9 } }));
  assert(d.getElementById("bubble").classList.contains("show"), "precondition: bubble visible");
  w.togglePanel("status");
  assert(!d.getElementById("bubble").classList.contains("show"), "bubble should be dismissed by the panel");
}));

/* --- panels -------------------------------------------------------------- */

test("the status panel lists running sessions", withPet(async (w, d) => {
  w.apply(view({
    snapshot: {
      state: "working", forced: false, stats: {},
      sessions: [
        session({ key: { source: "claude", id: "one" }, duration_ns: 90e9 }),
        session({ key: { source: "codex", id: "two" }, state: "thinking" }),
      ],
    },
  }));
  w.togglePanel("status");
  const text = d.getElementById("panel").textContent;
  assert(text.includes("claude/one"), "first session missing");
  assert(text.includes("codex/two"), "second session missing");
  assert(text.includes("1m"), "session duration missing");
  assert(text.includes("thinking"), "session state missing");
}));

test("the status panel says so when nothing is running", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "sleeping", forced: false, sessions: [], stats: {} } }));
  w.togglePanel("status");
  assert(d.getElementById("panel").textContent.includes("Nothing running."), "empty state missing");
}));

test("updateLine reports every answer the updater can give", withPet(async (w) => {
  const at = new Date(Date.now() - 2 * 3600e3).toISOString();
  // Never checked is an answer, not a blank: the pet does not check for itself,
  // so "nobody has looked" is a real state somebody can sit in for days.
  eq(w.updateLine(null), "never checked");
  eq(w.updateLine({ current: "1.0.0" }), "never checked");
  eq(w.updateLine({ current: "1.0.0", latest: "1.0.0", checked_at: at }), "up to date · 2h 0m ago");
  eq(w.updateLine({ current: "1.0.0", latest: "1.1.0", available: true, checked_at: at }),
     "1.1.0 available · 2h 0m ago");
  eq(w.updateLine({ current: "1.0.0", checked_at: at }), "nothing published · 2h 0m ago");
  eq(w.updateLine({ current: "1.2.0-dev.1", latest: "1.1.0", checked_at: at }),
     "ahead of the channel · 2h 0m ago");
  eq(w.updateLine({ current: "1.0.0", error: "timeout", checked_at: at }), "check failed · 2h 0m ago");
  // An error outranks a stale success, or a check that could not run leaves the
  // previous answer on screen looking current.
  eq(w.updateLine({ current: "1.0.0", latest: "1.0.0", error: "timeout", checked_at: at }),
     "check failed · 2h 0m ago");
}));

test("the status panel says when the last update check ran", withPet(async (w, d) => {
  w.apply(view());
  stubBackend(w, {
    GetUpdate: () => ({
      current: "1.0.0", latest: "1.0.0", available: false,
      checked_at: new Date(Date.now() - 90 * 60e3).toISOString(),
    }),
  });
  w.togglePanel("status");
  await tick(); // the row is filled from GetUpdate
  const text = d.getElementById("panel").textContent;
  assert(text.includes("Update"), "the status panel should carry an Update row");
  assert(text.includes("up to date"), `wanted the last result, got: ${text}`);
  assert(text.includes("1h 30m ago"), `wanted the elapsed time, got: ${text}`);
}));

test("the panel says so rather than lying when no backend answers", withPet(async (w, d) => {
  w.apply(view());
  // No stub at all: backend() is null, call() resolves null, and the row must
  // still end up with something true in it.
  w.togglePanel("status");
  await tick();
  const text = d.getElementById("panel").textContent;
  assert(text.includes("never checked"), `wanted "never checked", got: ${text}`);
}));

test("the status panel discloses a forced state", withPet(async (w, d) => {
  // Otherwise `petctl test` looks like the pet is broken.
  w.apply(view({ snapshot: { state: "celebrate", forced: true, sessions: [], stats: {} } }));
  w.togglePanel("status");
  assert(d.getElementById("panel").textContent.includes("forced"), "a forced state should be disclosed");
}));

test("About opens the window rather than a panel", withPet(async (w, d) => {
  w.apply(view());
  const calls = stubBackend(w, {});
  w.openMenu();
  const items = [...d.getElementById("menu").querySelectorAll(".mi")];
  const about = items.find((r) => r.textContent.trim() === "About");
  assert(about, `the menu should offer About, got: ${items.map((r) => r.textContent.trim())}`);
  about.dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  await tick();
  // A real NSWindow, centred, with a close button — the frontend's whole part
  // in it is asking for it. If this ever went back to being a panel it would
  // open inside the pet's own window, wherever that had been dragged to.
  eq(called(calls, "ShowAbout").length, 1, "About should ask the backend to open the window");
  assert(d.getElementById("panel").classList.contains("hidden"), "no panel should open");
}));

test("clicking the same panel twice closes it", withPet(async (w, d) => {
  const panel = d.getElementById("panel");
  w.apply(view());
  w.togglePanel("status");
  assert(!panel.classList.contains("hidden"), "panel should open");
  w.togglePanel("status");
  assert(panel.classList.contains("hidden"), "panel should close on a second toggle");
}));

test("switching panel kinds keeps the panel open", withPet(async (w, d) => {
  const panel = d.getElementById("panel");
  w.apply(view());
  w.togglePanel("status");
  w.togglePanel("pets");
  await tick(); // buildPets awaits ListPets
  assert(!panel.classList.contains("hidden"), "panel should stay open when switching kinds");
  eq(panel.dataset.kind, "pets");
}));

test("an open panel refreshes when state arrives", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "idle", forced: false, sessions: [], stats: {} } }));
  w.togglePanel("status");
  assert(d.getElementById("panel").textContent.includes("Nothing running."), "precondition");
  w.apply(view({ snapshot: { state: "working", forced: false, stats: {}, sessions: [session()] } }));
  assert(d.getElementById("panel").textContent.includes("claude/abc"), "an open panel should follow the state");
}));

function crowdedView() {
  const sessions = [];
  for (let i = 0; i < 20; i++) {
    sessions.push(session({ key: { source: "claude", id: "session-" + i } }));
  }
  return view({ snapshot: { state: "working", forced: false, stats: {}, sessions } });
}

// Whichever way the window grew, the panel must not end up on top of the
// character — the entire reason the pet and the panel are separated at all.
test("a full panel never covers the pet, opening upward", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(crowdedView());
  w.togglePanel("status");
  await tick();

  const panel = d.getElementById("panel").getBoundingClientRect();
  const pet = d.getElementById("pet").getBoundingClientRect();
  assert(panel.height > 0, "precondition: the panel should be open");
  assert(panel.bottom <= pet.top,
    `the panel covers the pet: panel ends at ${Math.round(panel.bottom)}, pet starts at ${Math.round(pet.top)}`);
}, GROWN_H));

test("a full panel never covers the pet, opening downward", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "below", pet_x: WINDOW_W / 2 }) });
  w.apply(crowdedView());
  w.togglePanel("status");
  await tick();

  assert(d.body.classList.contains("overlay-below"), "the frontend should flip when told to");
  const panel = d.getElementById("panel").getBoundingClientRect();
  const pet = d.getElementById("pet").getBoundingClientRect();
  assert(panel.height > 0, "precondition: the panel should be open");
  assert(panel.top >= pet.bottom,
    `the panel covers the pet: panel starts at ${Math.round(panel.top)}, pet ends at ${Math.round(pet.bottom)}`);
}, GROWN_H));

test("the menu also stays clear of the pet", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  w.openMenu();
  await tick();
  const menu = d.getElementById("menu").getBoundingClientRect();
  const pet = d.getElementById("pet").getBoundingClientRect();
  assert(menu.bottom <= pet.top,
    `the menu covers the pet: menu ends at ${Math.round(menu.bottom)}, pet starts at ${Math.round(pet.top)}`);
}, GROWN_H));

// The pet must not jump when a panel opens. Growing downward leaves the
// window's top edge alone, so the character has to be measured from the top
// instead of the bottom to stay put.
test("flipping the overlay does not move the pet", withPet(async (w, d, frame) => {
  // The frame has to grow the way the real window does, or this proves
  // nothing: with a fixed-height frame the pet cannot move whether the flip
  // works or not, and the test passes for the wrong reason. Growing downward
  // keeps the window's top edge and extends the bottom, which is exactly what
  // changing the frame's height does.
  stubBackend(w, {
    OpenOverlay: (w, h) => {
      frame.style.height = (WINDOW_H + h) + "px";
      return { side: "below", pet_x: WINDOW_W / 2 };
    },
  });
  w.apply(view());
  const before = d.getElementById("pet").getBoundingClientRect().top;
  w.togglePanel("status");
  await tick();
  const after = d.getElementById("pet").getBoundingClientRect().top;
  assert(Math.abs(after - before) < 1,
    `the pet moved ${Math.round(after - before)}px when the panel opened`);
}, WINDOW_H));

test("opening an overlay asks the window for exactly the room it needs", withPet(async (w, d) => {
  const calls = stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(crowdedView());
  w.togglePanel("status");
  await tick();

  const asked = called(calls, "OpenOverlay");
  assert(asked.length === 1, `want one request for room, got ${asked.length}`);
  const wanted = asked[0].args[1];
  const panel = d.getElementById("panel").getBoundingClientRect();
  assert(wanted >= panel.height,
    `asked for ${wanted} but the panel is ${Math.round(panel.height)} tall`);
  assert(wanted <= panel.height + 24,
    `asked for ${wanted} for a ${Math.round(panel.height)} panel — the surplus is dead space`);
}, GROWN_H));

test("closing an overlay gives the room back", withPet(async (w, d) => {
  const calls = stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  w.togglePanel("status");
  await tick();
  w.togglePanel("status");
  await tick();
  assert(called(calls, "CloseOverlay").length === 1,
    "the window must shrink again, or the dead space comes straight back");
  assert(!d.body.classList.contains("overlay-below"), "the flip should be cleared on close");
}, GROWN_H));

// An open panel is rebuilt on every state change. Asking the window to resize
// each time would thrash it several times a second under a busy agent.
test("a rebuild that does not change height does not resize the window", withPet(async (w, d) => {
  const calls = stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(crowdedView());
  w.togglePanel("status");
  await tick();
  const first = called(calls, "OpenOverlay").length;

  for (let i = 0; i < 5; i++) {
    w.apply(crowdedView());
    await tick();
  }
  eq(called(calls, "OpenOverlay").length, first,
    "the window was resized again for a panel that did not change size");
}, GROWN_H));

/* --- menu ---------------------------------------------------------------- */

// Sleep and Wake are gone. `sleeping` is now only ever something the pet
// arrived at on its own, so nothing offers to put it there by hand.
test("the menu does not offer to sleep or wake the pet", withPet(async (w, d) => {
  w.apply(view({ snapshot: { state: "sleeping", forced: true, sessions: [], stats: {} } }));
  w.openMenu();
  const text = d.getElementById("menu").textContent;
  assert(!text.includes("Sleep"), `Sleep should be gone, menu reads: ${text}`);
  assert(!text.includes("Wake"), `Wake should be gone, menu reads: ${text}`);
}));

test("the menu checks Always on Top to match the window", withPet(async (w, d) => {
  w.apply(view({ on_top: true }));
  w.openMenu();
  const rows = [...d.getElementById("menu").querySelectorAll(".mi")];
  const onTop = rows.find((r) => r.textContent.includes("Always on Top"));
  assert(onTop, "Always on Top missing");
  assert(onTop.querySelector(".check").textContent === "✓", "should be ticked when the window is on top");
}));

test("menu actions reach the backend", withPet(async (w, d) => {
  w.apply(view());
  const calls = stubBackend(w);
  w.openMenu();
  const rows = [...d.getElementById("menu").querySelectorAll(".mi")];
  const click = (label) => rows.find((r) => r.textContent.includes(label)).dispatchEvent(
    new w.MouseEvent("click", { bubbles: true }));

  click("Always on Top");
  await tick();
  eq(called(calls, "SetAlwaysOnTop").length, 1, "Always on Top should call the backend");
  eq(called(calls, "SetAlwaysOnTop")[0].args[0], false, "should toggle away from the current value");
}));

test("Quit asks the backend to quit", withPet(async (w, d) => {
  w.apply(view());
  const calls = stubBackend(w);
  w.openMenu();
  [...d.getElementById("menu").querySelectorAll(".mi")]
    .find((r) => r.textContent.includes("Quit"))
    .dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  await tick();
  eq(called(calls, "Quit").length, 1, "Quit should reach the backend");
}));

/* --- interaction --------------------------------------------------------- */

// Picking the pet up should not have a side effect, so a plain click does
// nothing at all now. The status panel is a menu item.
test("clicking the pet opens nothing", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  const pet = d.getElementById("pet");
  pet.dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true, button: 0, screenX: 500, screenY: 300 }));
  pet.dispatchEvent(new w.MouseEvent("click", { bubbles: true, button: 0, screenX: 500, screenY: 300 }));
  // Longer than the delay the old handler waited before opening the panel.
  await tick(300);
  assert(d.getElementById("panel").classList.contains("hidden"), "a click should open nothing");
  assert(d.getElementById("menu").classList.contains("hidden"), "and certainly not the menu");
}, GROWN_H));

test("a double-click pets the character", withPet(async (w, d) => {
  w.apply(view());
  const calls = stubBackend(w);
  const pet = d.getElementById("pet");
  pet.dispatchEvent(new w.MouseEvent("click", { bubbles: true, button: 0 }));
  pet.dispatchEvent(new w.MouseEvent("dblclick", { bubbles: true }));
  await tick(300);
  eq(called(calls, "Interact").length, 1, "a double-click should pet");
  assert(d.getElementById("panel").classList.contains("hidden"), "and open nothing");
}));

test("right-clicking opens the menu instead of the browser's", withPet(async (w, d) => {
  w.apply(view());
  const e = new w.MouseEvent("contextmenu", { bubbles: true, cancelable: true });
  d.getElementById("pet").dispatchEvent(e);
  assert(e.defaultPrevented, "the native context menu should be suppressed");
  assert(!d.getElementById("menu").classList.contains("hidden"), "the pet menu should open");
}));

test("Escape closes whatever is open", withPet(async (w, d) => {
  w.apply(view());
  w.openMenu();
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert(d.getElementById("menu").classList.contains("hidden"), "Escape should close the menu");

  w.togglePanel("status");
  d.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  assert(d.getElementById("panel").classList.contains("hidden"), "Escape should close the panel");
}));

test("clicking away closes overlays", withPet(async (w, d) => {
  w.apply(view());
  w.togglePanel("status");
  d.getElementById("stage").dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  assert(d.getElementById("panel").classList.contains("hidden"), "clicking outside should dismiss the panel");
}));

test("clicking inside a panel does not dismiss it", withPet(async (w, d) => {
  w.apply(view());
  w.togglePanel("status");
  d.getElementById("panel").dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  assert(!d.getElementById("panel").classList.contains("hidden"), "clicking inside should keep the panel open");
}));

// The window is barely bigger than the character now, so a click "outside" it
// lands on another application and the page never hears about it. Losing focus
// is the same gesture from this side of the glass.
test("clicking away from the window closes the menu", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  w.openMenu();
  await tick();
  assert(!d.getElementById("menu").classList.contains("hidden"), "precondition: menu open");

  w.dispatchEvent(new w.Event("blur"));
  assert(d.getElementById("menu").classList.contains("hidden"),
    "losing focus should close the menu");
}, GROWN_H));

test("losing focus closes a panel too, and returns the room", withPet(async (w, d) => {
  const calls = stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  w.togglePanel("status");
  await tick();
  w.dispatchEvent(new w.Event("blur"));
  assert(d.getElementById("panel").classList.contains("hidden"), "panel should close");
  eq(called(calls, "CloseOverlay").length, 1, "the window should shrink back");
}, GROWN_H));

// In a corner the window is slid back onto the screen but the character is
// not, so an overlay centred on it would hang off the window and be clipped.
test("an overlay stays inside the window when the pet is near its edge", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: 12 }) });
  w.apply(crowdedView());
  w.togglePanel("status");
  await tick();

  const panel = d.getElementById("panel").getBoundingClientRect();
  assert(panel.left >= 0, `panel starts off the window at ${Math.round(panel.left)}`);
  assert(panel.right <= d.documentElement.clientWidth + 1,
    `panel ends at ${Math.round(panel.right)}, past the window's ${d.documentElement.clientWidth}`);
}, GROWN_H));

test("the character sits where the backend places it", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: 40 }) });
  w.apply(view());
  w.togglePanel("status");
  await tick();
  const pet = d.getElementById("pet").getBoundingClientRect();
  const centre = pet.left + pet.width / 2;
  assert(Math.abs(centre - 40) < 2,
    `character centre should be at 40, got ${Math.round(centre)}`);
}, GROWN_H));

/* --- size ---------------------------------------------------------------- */

test("the menu offers three sizes with the current one ticked", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view({ scale: 1 }));
  w.openMenu();
  await tick();

  const rows = [...d.getElementById("menu").querySelectorAll(".mi")];
  for (const name of ["Small", "Medium", "Large"]) {
    assert(rows.some((r) => r.textContent.includes(name)), `${name} missing from the menu`);
  }
  const medium = rows.find((r) => r.textContent.includes("Medium"));
  eq(medium.querySelector(".check").textContent, "✓", "scale 1 should tick Medium");
  const small = rows.find((r) => r.textContent.includes("Small"));
  eq(small.querySelector(".check").textContent, "", "only the current size is ticked");
}, GROWN_H));

test("the ticked size follows the scale in use", withPet(async (w, d) => {
  stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view({ scale: 1.5 }));
  w.openMenu();
  await tick();
  const rows = [...d.getElementById("menu").querySelectorAll(".mi")];
  eq(rows.find((r) => r.textContent.includes("Large")).querySelector(".check").textContent, "✓");
  eq(rows.find((r) => r.textContent.includes("Medium")).querySelector(".check").textContent, "");
}, GROWN_H));

test("picking a size asks the backend for it", withPet(async (w, d) => {
  const calls = stubBackend(w, { OpenOverlay: () => ({ side: "above", pet_x: WINDOW_W / 2 }) });
  w.apply(view());
  w.openMenu();
  await tick();
  [...d.getElementById("menu").querySelectorAll(".mi")]
    .find((r) => r.textContent.includes("Large"))
    .dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  await tick();

  const set = called(calls, "SetScale");
  eq(set.length, 1, "picking a size should call SetScale");
  eq(set[0].args[0], 1.5, "Large should be 1.5");
  assert(d.getElementById("menu").classList.contains("hidden"), "the menu should close after picking");
}, GROWN_H));

/* --- backend absence ----------------------------------------------------- */

test("the UI loads and stays usable with no backend at all", withPet(async (w, d) => {
  // This is the state every one of these tests starts in: window.go is absent
  // because nothing bound it. Nothing may throw, and the window must not be
  // painted opaque — a transparent window is the whole visual design.
  eq(w.backend(), null);
  w.togglePanel("status");
  assert(!d.getElementById("panel").classList.contains("hidden"), "panels should work without a backend");
  const bg = w.getComputedStyle(d.body).backgroundColor;
  assert(bg === "rgba(0, 0, 0, 0)" || bg === "transparent", `the window must stay transparent, got ${bg}`);
}));

test("changing pet through the panel reaches the backend", withPet(async (w, d) => {
  w.apply(view());
  const calls = stubBackend(w, {
    ListPets: () => [
      { id: "sanmao", name: "Sanmao", builtin: true },
      { id: "byte", name: "Byte", builtin: true },
    ],
  });
  w.togglePanel("pets");
  await tick(); // buildPets awaits ListPets
  const rows = [...d.getElementById("panel").querySelectorAll(".mi")];
  eq(rows.length, 2, "both packs should be listed");
  rows.find((r) => r.textContent.includes("Byte")).dispatchEvent(new w.MouseEvent("click", { bubbles: true }));
  await tick();
  eq(called(calls, "SetPet").length, 1, "picking a pet should call SetPet");
  eq(called(calls, "SetPet")[0].args[0], "byte");
}));

/* --- runner -------------------------------------------------------------- */

async function run(report) {
  let pass = 0;
  const failures = [];
  for (const t of tests) {
    try {
      await t.fn();
      pass++;
      report({ name: t.name, ok: true });
    } catch (e) {
      failures.push({ name: t.name, error: String((e && e.message) || e) });
      report({ name: t.name, ok: false, error: String((e && e.message) || e) });
    }
  }
  return { total: tests.length, pass, fail: failures.length, failures };
}
