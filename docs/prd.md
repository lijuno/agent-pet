# Personal Digital Pet for Claude Code and Codex

> **This is the original design document, not a description of the software.**
> It was written before any code and is kept for the reasoning behind the
> decisions, not as a feature list. Much of what follows is unbuilt — the Codex
> adapter, generating a pet from a photo, and the tiredness curve among it — and
> some of what was built ended up different. The README describes what the
> program actually does; [`docs/adr/`](adr/) records where and why this document
> was departed from.

## 1. Overview

Build a lightweight desktop digital pet that acts as a persistent companion while the user works with **Claude Code** and **OpenAI Codex CLI**.

The pet should react to coding-agent activity such as:

- starting and ending sessions
- thinking or working
- executing tools
- waiting for user input
- requesting permission
- completing a task
- encountering an error
- passing or failing tests
- remaining inactive for a period of time

The pet should feel personal rather than being a generic mascot.

A core feature is therefore **customizable appearance**. The user should be able to create a pet based on:

- a photo of a person
- a cartoon character
- an avatar
- a custom illustration
- built-in pet templates

The customized image should be transformed into a collection of expressive pet states such as idle, happy, working, sleeping, confused, celebrating, and requesting attention.

The first version should run locally and avoid unnecessary system permissions.

---

# 2. Product Goals

The product should achieve the following goals.

### Primary goals

1. Provide a fun visual representation of Claude Code and Codex activity.
2. Give useful ambient feedback without requiring the user to watch the terminal.
3. Create an emotional sense of having a coding companion.
4. Allow the pet to be deeply personalized.
5. Support Claude Code and Codex through separate adapters.
6. Keep the pet engine independent from any specific AI coding agent.
7. Run as a lightweight desktop application.
8. Store user data locally by default.

### Secondary goals

The architecture should make it easy to later integrate:

- Git
- test runners
- GitHub
- CI systems
- OpenCode
- Gemini CLI
- custom developer tools
- remote SSH development sessions

---

# 3. Non-Goals for V1

V1 does **not** need to:

- act as an autonomous AI agent
- automatically approve Claude Code or Codex permissions
- read keyboard input globally
- monitor all user activity
- record the user's screen
- access the camera or microphone
- inspect source code unless explicitly provided through an integration
- continuously call an LLM
- provide a full conversational chatbot experience
- synchronize pet state across multiple computers
- provide a cloud account system

The pet should remain primarily an **ambient companion and status indicator**.

---

# 4. High-Level Architecture

Use a modular architecture.

```text
Claude Code Adapter ──────┐
                          │
Codex Adapter ────────────┤
                          │
Git Adapter ──────────────┤
                          ▼
                    Event API
                          │
                          ▼
                       petd
                ┌─────────┴─────────┐
                │                   │
          State Machine         Event Store
                │                   │
                ▼                   ▼
          Pet Renderer           SQLite
                │
                ▼
         Desktop Pet Window
```

The core pet engine must not depend directly on Claude Code or Codex.

Each integration should translate external events into a common internal event schema.

---

# 5. Core Components

The application should contain four primary components.

## 5.1 `petd`

`petd` is the background service responsible for:

- receiving events
- maintaining pet state
- maintaining timers
- storing history
- managing pet configuration
- deciding emotional state
- notifying the UI
- optionally generating dialogue

Prefer a single-process or single-binary architecture where practical.

---

## 5.2 Pet UI

The desktop UI renders the pet.

Requirements:

- small transparent window
- frameless
- draggable
- optional always-on-top mode
- support macOS initially
- architecture should allow Windows and Linux support
- low CPU utilization when idle
- animation should pause or reduce frame rate when appropriate

The user should be able to position the pet anywhere on the desktop.

Example:

```text
                                ┌──────────────┐
                                │ VS Code      │
                                │              │
                                │              │
                                │        🐱    │
                                └──────────────┘
```

The pet may optionally sit near a screen edge or above the dock/taskbar.

---

## 5.3 Agent Adapters

Provide adapters for:

### Claude Code

Translate Claude Code hooks into pet events.

Examples:

```text
SessionStart
PreToolUse
PostToolUse
PermissionRequest
Notification
Stop
SubagentStart
SubagentStop
```

Exact hooks should be mapped based on currently supported Claude Code capabilities.

### Codex CLI

Translate supported Codex notifications/events into the same internal event model.

Codex capabilities may not map one-to-one with Claude Code.

The adapter should gracefully degrade when a particular Codex event is unavailable.

---

## 5.4 Pet Asset Manager

Responsible for:

- built-in pets
- imported characters
- customized faces
- animation assets
- generated expressions
- switching between pets

---

# 6. Common Event API

Use a generic internal event structure.

Example:

```json
{
  "source": "claude",
  "event": "tool_started",
  "timestamp": "2026-08-16T12:00:00+08:00",
  "metadata": {
    "tool": "bash"
  }
}
```

Minimum supported events:

```text
session_started
session_ended

thinking_started
working
idle

tool_started
tool_finished
tool_failed

permission_requested

user_input_requested

task_completed
task_failed

tests_started
tests_passed
tests_failed

git_commit

error

heartbeat
```

The system should tolerate unknown event types.

---

# 7. Pet State Model

The event stream should be mapped into visible pet states.

Minimum states:

| State | Description |
|---|---|
| sleeping | No recent activity |
| idle | Agent is available |
| thinking | Agent is reasoning |
| working | Agent/tool execution |
| attention | User action is required |
| confused | Error or failed operation |
| worried | Repeated failures |
| happy | Successful completion |
| celebrate | Important success |
| tired | Long session |
| heart | User interacts positively with pet |

State transitions should be deterministic.

Example:

```text
session_started
      ↓
    idle
      ↓
thinking_started
      ↓
  thinking
      ↓
tool_started
      ↓
  working
      ↓
task_completed
      ↓
   happy
      ↓
    idle
      ↓
timeout
      ↓
 sleeping
```

---

# 8. Personality Layer

Appearance and personality should be separate.

Example personality configuration:

```json
{
  "name": "Momo",
  "personality": "gentle",
  "energy": 65,
  "curiosity": 80,
  "snark": 10,
  "patience": 90
}
```

Initial personality presets:

- gentle
- cheerful
- calm
- mischievous
- sarcastic
- energetic

Personality influences:

- expressions
- animation frequency
- optional comments
- celebration intensity
- reaction to errors

Personality must not affect permission/security behavior.

---

# 9. Custom Character / Personalized Face

This is a key feature.

Users should be able to create a digital pet from a supplied image.

Supported source types:

- photograph of a person
- selfie
- cartoon character
- avatar
- anime character
- drawing
- existing pet image

Example workflow:

```text
Create New Pet
       │
       ▼
Choose Image
       │
       ▼
Crop / Position Face
       │
       ▼
Select Style
       │
       ├── Keep original
       ├── Cartoon pet
       ├── Pixel art
       ├── Chibi
       └── Sticker
       │
       ▼
Generate Expressions
       │
       ▼
Preview
       │
       ▼
Save Pet
```

---

# 10. Personalized Face Requirements

For a human-photo-based pet, preserve recognizable identity while making the result suitable for a small animated character.

The generated character should have consistent:

- facial structure
- hairstyle
- glasses
- facial hair
- key identifying visual features

Create expressions for at least:

```text
neutral
thinking
working
happy
laughing
sleeping
surprised
worried
confused
celebrating
attention
heart
```

The character's identity should remain visually consistent across states.

---

# 11. Custom Pet Asset Format

Each pet should live in its own directory.

Example:

```text
pets/
└── momo/
    ├── manifest.json
    ├── source.png
    ├── idle.webp
    ├── thinking.webp
    ├── working.webp
    ├── attention.webp
    ├── confused.webp
    ├── happy.webp
    ├── celebrate.webp
    ├── sleeping.webp
    ├── tired.webp
    └── heart.webp
```

Example manifest:

```json
{
  "id": "momo",
  "name": "Momo",
  "version": 1,

  "animations": {
    "idle": "idle.webp",
    "thinking": "thinking.webp",
    "working": "working.webp",
    "attention": "attention.webp",
    "confused": "confused.webp",
    "happy": "happy.webp",
    "celebrate": "celebrate.webp",
    "sleeping": "sleeping.webp",
    "tired": "tired.webp",
    "heart": "heart.webp"
  }
}
```

Animated WebP, GIF, sprite sheets, or another simple animation format may be used.

Prefer formats that provide:

- transparency
- small file size
- simple decoding
- broad platform support

---

# 12. Image Generation Architecture

Do not tightly couple image generation to the runtime.

Treat character generation as a separate optional module.

```text
Source photo
    │
    ▼
Character Generator
    │
    ▼
Pet Asset Pack
    │
    ▼
Local Pet Runtime
```

After creation, the runtime should only need local assets.

It should **not need an AI image API every time the pet animates**.

---

# 13. Privacy Requirements

Personal photos must be treated as private data.

Default behavior:

- source images stored locally
- pet assets stored locally
- no cloud upload unless required and explicitly initiated
- clearly indicate if an external image-generation API is used
- allow deleting both the original image and generated assets

If cloud generation is used, disclose before upload:

```text
This image will be sent to <service>
to generate pet expressions.
```

The user should be able to choose:

```text
Keep original photo locally
Delete original after generation
```

---

# 14. Desktop Interaction

The user should be able to interact directly with the pet.

Minimum interactions:

### Click

Show pet status.

Example:

```text
Momo

Claude is working.

Session: 34 min
Tasks completed: 3
```

### Double-click

Play a positive reaction.

### Drag

Move the pet.

### Right-click

Show menu:

```text
Pet Status
Change Pet
Pet Settings
Always on Top
Mute
Sleep
Statistics
Quit
```

---

# 15. Permission Request Behavior

When Claude Code asks the user for permission, the pet enters:

```text
attention
```

The pet should visually signal the request.

Example:

```text
     !!!
    (•̀ᴗ•́)
   /|    |\
```

Clicking the pet may bring the terminal/application to the foreground.

For V1, the pet **must not automatically approve anything**.

Future versions may expose:

```text
Approve
Reject
```

but only when the user explicitly clicks the corresponding action.

---

# 16. Optional Speech Bubble

The pet may display short messages.

Examples:

```text
Claude needs you.
```

```text
Tests passed! 🎉
```

```text
That one took a while.
```

```text
We've been coding for two hours.
```

Messages should normally be generated from templates.

An LLM may optionally generate occasional dialogue.

The application must remain fully functional without an LLM.

---

# 17. Local Memory and Progression

The pet should have lightweight persistence.

Store:

```text
pet name
pet creation date

total sessions
total coding time
tasks completed
errors encountered
tests passed
tests failed
commits observed

pet XP
pet level
interaction count
```

Use SQLite.

Example:

```text
Pet: Momo
Level: 7

Coding together:
73 hours

Tasks completed:
238

Bugs defeated:
41
```

---

# 18. XP System

Pet progression is primarily cosmetic.

Example:

```text
task completed      +3 XP
tests passed        +5 XP
git commit          +3 XP
long session        +2 XP
user pets character +1 XP
```

Avoid rewarding raw token consumption or excessive coding time.

The system should not incentivize unhealthy work patterns.

---

# 19. Pet Evolution

Optional later feature.

At certain levels, the pet can unlock:

- new idle animations
- accessories
- backgrounds
- expressions
- celebration animations

Example:

```text
Level 1
🙂

Level 5
🙂☕

Level 10
😎💻

Level 20
🧙‍♂️
```

Do not alter the user's selected face without explicit choice.

---

# 20. Activity Timing

Suggested inactivity behavior:

```text
0–30 sec
working

30 sec–5 min
idle

5–30 min
relaxing

>30 min
sleeping
```

These thresholds should be configurable.

---

# 21. Long Session Behavior

After prolonged use, the pet may visibly become tired.

For example:

```text
> 60 min     coffee animation

> 120 min    tired animation

> 180 min    stretching animation
```

These behaviors should remain playful rather than intrusive.

Do not repeatedly nag the user.

---

# 22. Multiple Agent Sessions

The user may run multiple Claude Code or Codex instances.

`petd` should track sessions independently.

Example:

```text
Claude session A → working
Claude session B → permission requested
Codex session C  → idle
```

Overall pet state should use priority:

```text
attention
    >
error
    >
celebrate
    >
working
    >
thinking
    >
idle
    >
sleeping
```

---

# 23. Multiple Agent Personality Option

Later versions may allow different visual behaviors for different agents.

For example:

```text
Claude → purple notebook
Codex  → green terminal
Git    → tiny hammer
Tests  → science goggles
```

But there should still be one coherent pet.

---

# 24. Configuration

Suggested config location:

```text
~/.config/digital-pet/config.yaml
```

Example:

```yaml
pet:
  active: momo
  always_on_top: true
  scale: 1.0
  sound: true

behavior:
  sleeping_after: 30m
  dialogue: true
  xp: true

integrations:
  claude:
    enabled: true

  codex:
    enabled: true

  git:
    enabled: false
```

---

# 25. Local Event API

Provide a simple API so external tools can interact with the pet.

For example:

```text
POST http://127.0.0.1:9876/event
```

Payload:

```json
{
  "source": "claude",
  "event": "working"
}
```

Alternative CLI:

```bash
petctl event claude working
```

Examples:

```bash
petctl event claude permission_requested

petctl event codex task_completed

petctl event tests tests_failed
```

---

# 26. Security Requirements

The event server must listen only on:

```text
127.0.0.1
```

by default.

Do not expose the port to the LAN.

Incoming events must not contain arbitrary executable commands.

Pet actions must not execute arbitrary shell commands received through the API.

Agent metadata should be treated as untrusted input.

Do not render arbitrary HTML from agent events.

---

# 27. OS Permissions

The core application should require:

```text
NO administrator privileges
NO root privileges
NO Accessibility permission
NO global keyboard monitoring
NO screen recording
NO camera
NO microphone
```

Optional desktop notification permission is acceptable.

Avoid requesting unnecessary permissions.

---

# 28. Claude Code Integration

Provide an installer/helper command:

```bash
petctl install claude
```

It should configure the required Claude Code hooks.

The operation should:

1. inspect the existing configuration
2. preserve existing hooks
3. add pet hooks
4. avoid destructive rewriting
5. support uninstall

Provide:

```bash
petctl uninstall claude
```

---

# 29. Codex Integration

Provide:

```bash
petctl install codex
```

Configure all supported Codex notification/event mechanisms.

Because Codex may expose fewer lifecycle events than Claude Code, the adapter should infer state conservatively.

Never fake detailed states that cannot actually be observed.

---

# 30. Integration Diagnostics

Provide:

```bash
petctl doctor
```

Example:

```text
Digital Pet Doctor

✓ petd running
✓ UI connected
✓ SQLite writable

Claude Code
✓ hooks installed
✓ events received

Codex
✓ notification configured
⚠ permission events unavailable

Pet
✓ Momo loaded
✓ 10 animations available
```

---

# 31. Manual Test Commands

Provide developer/debug commands:

```bash
petctl test idle
petctl test thinking
petctl test working
petctl test attention
petctl test confused
petctl test happy
petctl test celebrate
petctl test sleeping
```

This is important for testing animations without running Claude Code.

---

# 32. Logging

Logs should be concise.

Location:

```text
~/.local/share/digital-pet/logs/
```

Avoid storing:

- prompts
- source code
- terminal contents
- command arguments

unless explicitly enabled for debugging.

Default logs should contain only event categories.

Example:

```text
12:41:02 claude session_started
12:41:08 claude tool_started
12:41:17 claude tool_finished
12:41:31 claude permission_requested
```

---

# 33. Technical Preference

Prefer a lightweight implementation.

Suggested backend:

```text
Go
```

Benefits:

- single binary
- easy local HTTP server
- good concurrency
- SQLite support
- straightforward cross-platform distribution

For desktop UI, evaluate:

```text
Wails
Tauri
native platform UI
lightweight transparent webview
```

Avoid Electron unless there is a strong reason.

The target application should consume little memory while idle.

---

# 34. Suggested Repository Structure

```text
digital-pet/
├── cmd/
│   ├── petd/
│   └── petctl/
│
├── internal/
│   ├── events/
│   ├── state/
│   ├── storage/
│   ├── personality/
│   └── pets/
│
├── adapters/
│   ├── claude/
│   ├── codex/
│   └── git/
│
├── ui/
│
├── pets/
│   └── default/
│
├── docs/
│
└── README.md
```

---

# 35. V1 Milestones

## Milestone 1 — Core Pet

Implement:

- `petd`
- local event API
- state machine
- simple pet UI
- built-in test character
- manual event testing

No Claude or Codex integration yet.

---

## Milestone 2 — Claude Code

Implement:

- Claude Code hook adapter
- session lifecycle
- tool activity
- permission requests
- completion events

Demonstrate:

```text
Claude works
      ↓
pet works

Claude requests permission
      ↓
pet asks for attention

Claude finishes
      ↓
pet celebrates
```

---

## Milestone 3 — Codex

Implement:

- Codex adapter
- supported activity events
- completion notifications
- fallback behavior where detailed events are unavailable

---

## Milestone 4 — Persistent Pet

Implement:

- SQLite
- XP
- level
- statistics
- session history
- pet interactions

---

## Milestone 5 — Personalized Pet

Implement:

- import image
- crop image
- create custom pet
- generate/assign expressions
- preview state animations
- save asset pack
- switch between pets

This milestone is critical to the product identity.

---

# 36. V1 Acceptance Criteria

The product is considered usable when all of the following work.

### Desktop pet

- [ ] Pet appears as a transparent desktop character.
- [ ] Pet can be dragged.
- [ ] Pet can remain always on top.
- [ ] Pet consumes minimal resources while idle.

### Claude Code

- [ ] Starting Claude Code wakes the pet.
- [ ] Claude working causes the pet to animate.
- [ ] Permission requests cause the pet to request attention.
- [ ] Completion causes a positive reaction.
- [ ] Ending the session eventually causes the pet to sleep.

### Codex

- [ ] Codex events are received where supported.
- [ ] Task completion is represented.
- [ ] Unsupported events degrade gracefully.

### Customization

- [ ] User can import an image.
- [ ] The pet can visually use the face/character from that image.
- [ ] Multiple emotional states are supported.
- [ ] The same recognizable character remains consistent across states.
- [ ] Custom pets can be switched without restarting the application.

### Privacy

- [ ] No source code is captured by default.
- [ ] No prompts are captured by default.
- [ ] Personal images remain local unless the user explicitly invokes an external generation service.
- [ ] No privileged OS permissions are required.

---

# 37. Product Principle

The pet should feel like:

> **a little companion that happens to understand what your coding agent is doing.**

It should not feel like:

> **another monitoring dashboard with a cartoon skin.**

Animations, expressions, personality, progression, and occasional playful reactions should therefore receive as much design attention as the underlying agent integrations.

The personalized face is especially important. A pet based on a person, family member, favorite character, or custom illustration can make the companion feel meaningfully different from a generic mascot.

At the same time, the application should remain technically simple:

```text
agent events
     ↓
local state machine
     ↓
expressive character
```

That simplicity should be preserved throughout the design.

---

# 38. Initial Implementation Instruction for Claude Code

Start with **Milestone 1 only**.

Do not attempt to implement the entire product at once.

Build:

1. the generic event model
2. local `petd` daemon
3. `petctl`
4. deterministic pet state machine
5. a transparent desktop window
6. one placeholder pet with static or simple animated states
7. commands such as:

```bash
petctl test idle
petctl test working
petctl test attention
petctl test happy
petctl test sleeping
```

Structure the project so Claude Code and Codex adapters can be added afterward without changing the core state machine.

Before implementation, document any major architectural decisions and keep external dependencies minimal.