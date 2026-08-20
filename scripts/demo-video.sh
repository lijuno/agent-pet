#!/usr/bin/env bash
# Drive the pet through a recordable demo: one story, tight beats, captions
# large enough to survive being scaled down for a README.
#
# This is not scripts/demo.sh. That one tours every state as a manual check and
# takes about forty-five seconds, which is a fine test and a boring video. This
# tells the only story worth telling — you can look away, and the pet calls you
# back — in under twenty.
#
# Never record a real session. A real one leaks paths, prompts and code into
# something you are about to publish, it cannot be re-shot the same way twice,
# and the pauses are dead air.
#
#   ./scripts/demo-video.sh --countdown 5
#
set -euo pipefail

PETCTL="${PETCTL:-}"
COUNTDOWN=5
LOOP=0
FORCE=0

while [ $# -gt 0 ]; do
	case $1 in
	--countdown) COUNTDOWN=$2; shift 2 ;;
	--loop) LOOP=1; shift ;;
	--force) FORCE=1; shift ;;
	# Print the header comment, however long it grows: stop at the first line
	# that is not one. A hardcoded line range goes stale the moment anyone
	# edits the comment, and prints shell at the reader.
	-h | --help) sed -n '2,${/^[^#]/q;p;}' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

# Prefer the installed bundle so the demo runs the build a viewer would get.
if [ -z "$PETCTL" ]; then
	for c in \
		"/Applications/digital-pet.app/Contents/MacOS/petctl" \
		"build/bin/digital-pet.app/Contents/MacOS/petctl" \
		"bin/petctl"; do
		[ -x "$c" ] && PETCTL=$c && break
	done
fi
[ -n "$PETCTL" ] || { echo "no petctl found — run make build" >&2; exit 1; }

if ! "$PETCTL" status >/dev/null 2>&1; then
	echo "petd is not running. Start the app first." >&2
	exit 1
fi

# A live Claude Code session with the hooks installed drives the same pet, and
# whichever state ranks highest wins. Your own session going to "working"
# halfway through the take is invisible until you watch the recording back, so
# check before spending a take rather than after.
others=$("$PETCTL" status --json 2>/dev/null |
	python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)['snapshot']
except Exception:
    sys.exit(0)
print(sum(1 for s in d.get('sessions', []) if not s['key']['id'].startswith('demo-')))
" 2>/dev/null || echo 0)

if [ "${others:-0}" -gt 0 ] && [ "$FORCE" = 0 ]; then
	cat >&2 <<-MSG

	  $others other agent session(s) are driving this pet right now.

	  They will fight the script for the pet's state and you will not see it
	  until you play the recording back. Quit any running Claude Code session
	  in a project with the hooks installed, or pass --force.

	MSG
	exit 1
fi

S="demo-$$"
fire() { "$PETCTL" event "$1" "$2" --session "$S" "${@:3}" >/dev/null; }

# Captions go through the terminal being recorded, so they are part of the
# frame rather than something added in post. Bold and spaced out: a 14pt
# terminal line is unreadable once the video is 600px wide.
beat() {
	printf '\n\033[1;97m  %s\033[0m\n' "$1"
	[ -n "${2:-}" ] && printf '\033[2;37m  %s\033[0m\n' "$2"
	return 0
}

run_once() {
	beat "Claude Code starts working" "the pet gets to work too"
	fire claude session_started
	sleep 1
	fire claude thinking_started
	sleep 1.5
	fire claude tool_started --meta tool=bash
	sleep 2.5

	beat "You look away" "switch to a browser — the pet stays on top"
	sleep 3

	beat "It needs permission" "the pet turns and asks for you"
	fire claude permission_requested
	sleep 4

	beat "You come back and approve" ""
	fire claude tool_started --meta tool=edit
	sleep 2

	beat "Something breaks" ""
	fire claude tool_failed
	sleep 3

	beat "Fixed — tests pass" ""
	fire claude tests_passed
	sleep 3.5

	beat "Quiet again" "it drifts off on its own"
	fire claude session_ended
	sleep 3
}

if [ "$COUNTDOWN" -gt 0 ]; then
	echo
	echo "  Start recording now. Beginning in:"
	for i in $(seq "$COUNTDOWN" -1 1); do
		printf "\r  %d " "$i"
		sleep 1
	done
	printf "\r    \r"
fi

run_once
while [ "$LOOP" = 1 ]; do
	sleep 2
	"$PETCTL" test clear >/dev/null 2>&1 || true
	run_once
done

echo
echo "  Done — stop the recording."
echo "  Encode it with: ./scripts/encode-demo.sh <recording.mov>"
