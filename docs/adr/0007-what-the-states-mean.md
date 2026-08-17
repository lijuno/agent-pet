# ADR 0007 — What the pet's states mean

Status: accepted (Milestone 2)

## Context

The visible state answers two different questions at once, and conflating them
produced most of the bugs in this area:

1. **Is an agent there at all?**
2. **What is it doing?**

Sessions were the only signal for both. That broke in two directions. A pet
with no sessions looked awake and idle for a full minute after Claude Code
exited, saying "something is here" when nothing was. And `sleeping` meant both
"quiet for a while" and — when it was still a menu command — "the user asked
for quiet", with no way to tell them apart on screen.

## Decision

**Colour answers the first question, the state answers the second.** Grey means
no agent is connected. The character's state means what that agent is doing.
They are orthogonal: grey and `sleeping` is Claude Code gone; colour and
`sleeping` is Claude Code open and quiet.

**Two sleep rules, stated positively, with no exception list.**

```
no sessions                        -> sleeping
best == Idle and quiet for 60s     -> sleeping
```

A pending request is `attention`, not idle, so it is outside the second rule
rather than excused from it. Reactions are outside it too, and would expire
long before sixty seconds in any case. Writing it as `best == Idle` rather than
"anything at or below working" is what removes the need for an exception, and
it is shorter.

**`SessionEnd` is not the same as the agent leaving.** Rewinding, `/clear` and
resuming end a conversation while Claude Code keeps running. The adapter reads
the reason and only ends the session for `prompt_input_exit` and `logout`.
Anything unrecognised counts as still running: a pet left in colour for an
agent that has quit still falls asleep a minute later, whereas a pet greyed out
for one that is running is simply wrong.

**Removed: `tired`, and the Sleep/Wake commands.**

`tired` was a long-session pose. Once `sleeping_after` dropped to a minute its
reachable window shrank to about fifty seconds, so it was a state, a sprite and
four sets of speech lines that almost nobody would ever see.

Sleep and Wake pinned the display to the sleeping animation and released it.
They collided with the automatic `sleeping` state — identical on screen,
entirely different meaning — and sat on a crowded ladder beside Mute, Show Pet
and grey. Removing them also made the status panel honest: it says "forced by
`petctl test`", and now that is the only thing that can force a state.

## Consequences

- Adding a state means asking which of the two questions it answers. If it is
  the first, it probably belongs in the colour treatment, not the state
  machine.
- The sleep threshold is short (60s) because grey carries the "is anyone
  there" signal. Lengthening it without changing that would make the pet stare
  at an empty prompt again.
- `attention` staying awake is a property of the rule, not a guard. Rewriting
  the condition to something that includes `attention` would silently lose it,
  which is why there is a test asserting a request survives five sleep
  intervals.
- Anything that ends a session without the agent leaving must map to `idle`,
  not `session_ended`. A future Codex adapter faces the same distinction.
