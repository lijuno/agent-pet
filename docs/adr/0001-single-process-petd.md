# ADR 0001 — `petd` is a single process that also owns the window

Status: accepted (Milestone 1)

## Context

The requirements describe `petd` as a background service and the Pet UI as a
separate component. A literal reading suggests two processes talking over IPC.
§5.1 also says "prefer a single-process or single-binary architecture where
practical".

## Decision

Ship **one binary, `petd`**, built with Wails. It contains:

- the engine (event bus, state machine, session tracker, timers)
- the local HTTP event API on `127.0.0.1:9876`
- the webview window that renders the pet

`petctl` is a second, tiny binary with no dependency on the engine. It only
speaks HTTP to `petd`.

The engine never imports Wails. The Wails layer (`main.go`, `app.go`) is a thin
adapter that subscribes to engine state changes and forwards them to the
frontend. This keeps the boundary the spec cares about — the engine is
replaceable and testable without a GUI.

## Consequences

- No IPC layer to build, debug, or secure between daemon and UI.
- One process to launch, one to quit, one to crash.
- If the window is closed the engine stops too. Acceptable for V1; a headless
  mode would be a `-headless` flag that skips `wails.Run`.
- A future second UI (menu bar app, web dashboard) can attach over the existing
  HTTP API and `/stream` SSE endpoint without any refactor.
