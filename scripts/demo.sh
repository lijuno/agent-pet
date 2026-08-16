#!/usr/bin/env bash
# Walk the pet through a realistic Claude Code session, with pauses so you can
# actually watch it. Requires a running petd.
set -euo pipefail

PETCTL="${PETCTL:-bin/petctl}"
S="demo-$$"

say() { printf '\n\033[1m%s\033[0m\n' "$1"; }
fire() { "$PETCTL" event "$1" "$2" --session "$S" "${@:3}" >/dev/null; }

if ! "$PETCTL" status >/dev/null 2>&1; then
  echo "petd is not running. Start it with 'wails dev' or 'make dev' first." >&2
  exit 1
fi

say "Claude starts up"
fire claude session_started
sleep 2

say "It thinks, then runs a tool"
fire claude thinking_started
sleep 2
fire claude tool_started --meta tool=bash
sleep 3

say "It needs permission — the pet asks for you"
fire claude permission_requested
sleep 4

say "You said yes; back to work"
fire claude tool_started --meta tool=edit
sleep 3
fire claude tool_finished
sleep 2

say "Something breaks"
fire claude tool_failed
sleep 3

say "And breaks again, twice more — now the pet is worried"
fire claude error
sleep 1
fire claude error
sleep 4

say "Fixed. Task complete"
fire claude task_completed
sleep 4

say "Tests pass"
fire claude tests_passed
sleep 5

say "A second agent shows up in parallel"
"$PETCTL" event codex working --session "codex-$$" >/dev/null
sleep 3

say "Everything finishes"
fire claude session_ended
"$PETCTL" event codex session_ended --session "codex-$$" >/dev/null

say "Done. The pet will drift to sleep on its own, or:"
echo "  $PETCTL test sleeping --for 10s"
