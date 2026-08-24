# ADR 0006 — What Milestone 1 deliberately leaves out

Status: accepted (Milestone 1)

§38 says to build Milestone 1 only. These are the things that were designed for
but not built, and the seam each one will attach to.

| Deferred | Milestone | Seam it attaches to |
|---|---|---|
| SQLite persistence, XP, levels, statistics | 4 | `engine.Engine` already emits every event to a `Sink` interface. The store becomes a second sink. Nothing else changes. |
| Claude Code hook adapter, `petctl install claude` | 2 | Adapters are HTTP clients. `adapters/claude/` gets a hook payload → `events.Event` translator and a settings.json patcher. The engine is untouched. |
| Codex adapter | 3 | Same seam. `source: "codex"`. |
| Custom pet from a photo | 5 | `petassets` already loads packs from `~/.local/share/agent-pet/pets/`. The generator writes a pack there and calls `POST /pet` to switch. |
| Git adapter | secondary | `source: "git"`, event `git_commit`, already in the event vocabulary and the XP table. |
| LLM-generated dialogue | optional | `internal/bubble` is a `Speaker` interface with a template implementation. An LLM speaker would be a second implementation, off by default. |
| Sound | — | Config field `pet.sound` exists and is threaded through; no audio assets in V1. |

**No persistence in Milestone 1.** State lives in memory and is rebuilt from the
event stream. Restarting `petd` resets the pet to `sleeping` and loses session
history. This is intentional — adding SQLite now would mean designing the schema
before the event vocabulary has been exercised against real Claude Code hooks.

**No LLM anywhere.** Speech bubbles come from templates chosen by state,
personality and a seeded RNG. The application has no network client at all
besides its own loopback listener — still true of `petd` after
[ADR 0008](0008-over-the-air-updates.md), which put the updater's network code
in `petctl` for exactly this reason.

**Custom pets are local-only, no AI.** Per the product decision: character packs
are built from local image processing (crop, palette reduction, procedural
expression overlays), never uploaded. No image API key, no cloud disclosure
flow, nothing to opt into. §13's cloud-disclosure requirements are satisfied
vacuously because no external service is ever contacted.
