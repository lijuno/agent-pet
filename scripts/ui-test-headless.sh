#!/bin/bash
# Runs the UI suite without a person watching it.
#
# `make test-ui` opens the page in your browser and you read the result. That
# needs a browser you can see, so it cannot run in CI or in a cloud session —
# and it is the same suite either way, so this drives the same page through
# headless Chromium and reads the summary out of the DOM.
#
# --virtual-time-budget rather than a sleep: it fast-forwards the page's timers
# and only then dumps, so a slow machine cannot make this report a green suite
# that had not finished running. The page raises its own flag — the summary
# starts as "running…" and is only replaced when the last test returns — so a
# dump taken too early fails loudly instead of passing quietly.
set -euo pipefail

cd "$(dirname "$0")/.."

# Playwright's copy first: on a cloud session that is the only one installed,
# and PLAYWRIGHT_BROWSERS_PATH is where it puts it.
chrome="${CHROMIUM:-}"
if [ -z "$chrome" ]; then
	for c in "${PLAYWRIGHT_BROWSERS_PATH:-/opt/pw-browsers}/chromium" \
		chromium chromium-browser google-chrome \
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"; do
		if [ -x "$c" ]; then chrome="$c"; break; fi
		if command -v "$c" >/dev/null 2>&1; then chrome="$c"; break; fi
	done
fi
if [ -z "$chrome" ]; then
	echo "no chromium found. Set CHROMIUM=/path/to/chrome, or run 'make test-ui'" >&2
	exit 1
fi

# A fixed port is a port the last run may still be holding. Ask the kernel.
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')

# file:// will not do: the tests need same-origin access to the frame they load
# the real UI into.
python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
	if curl -sSf -o /dev/null "http://127.0.0.1:$port/ui/test/index.html" 2>/dev/null; then break; fi
	sleep 0.1
done

profile=$(mktemp -d)
trap 'kill $server 2>/dev/null || true; rm -rf "$profile"' EXIT

dom=$("$chrome" \
	--headless \
	--no-sandbox \
	--disable-gpu \
	--user-data-dir="$profile" \
	--virtual-time-budget=60000 \
	--dump-dom "http://127.0.0.1:$port/ui/test/index.html" 2>/dev/null)

summary=$(printf '%s' "$dom" | sed -n 's/.*<div id="summary"[^>]*>\([^<]*\)<.*/\1/p')

case "$summary" in
"all "*" tests passed")
	echo "$summary"
	;;
"")
	echo "the page said nothing — chromium rendered no summary at all" >&2
	exit 1
	;;
*)
	echo "$summary" >&2
	# The failing names and their messages, in the order the page listed them.
	# One tag per line first: the whole dump arrives as a single line, and two
	# substitutions against one line means the second never sees the tag the
	# first replaced — which is how this printed the names without the reasons.
	printf '%s' "$dom" | sed 's/</\n</g' |
		sed -n 's|^<li class="fail">\(.*\)|  ✗ \1|p;s|^<div class="err">\(.*\)|    \1|p' >&2
	exit 1
	;;
esac
