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

# Say which browser, so a failure on somebody else's machine or on a CI runner
# names the thing that behaved differently.
echo "$("$chrome" --version 2>/dev/null || echo "unknown build") at $chrome" >&2

# Into a file rather than $( ). Chrome's zygote and renderer children inherit
# the pipe, and a command substitution waits for every writer to let go of it,
# so one lingering child hangs the script with the browser already gone.
dump="$profile/dom.html"

# --password-store=basic and --use-mock-keychain keep it away from the system
# keyring; the rest keep it from reaching for a network, a profile or an
# extension it does not have. Without them a headless Chrome on a bare CI
# runner blocks on dbus rather than rendering anything — which is not a
# failure, it is a job that runs until the six-hour limit and then is killed.
flags="--headless --no-sandbox --disable-gpu --disable-dev-shm-usage
	--no-first-run --no-default-browser-check --disable-background-networking
	--disable-sync --disable-default-apps --disable-extensions
	--password-store=basic --use-mock-keychain"

# And a bound on the whole thing, so a browser that hangs anyway costs two
# minutes and says so. `timeout` is coreutils: on a Mac it is gtimeout, or it
# is not there at all, and a local run manages without one.
runner=""
for t in timeout gtimeout; do
	if command -v "$t" >/dev/null 2>&1; then
		runner="$t 120"
		break
	fi
done

# shellcheck disable=SC2086 # $flags and $runner are meant to be word-split
if ! $runner "$chrome" $flags \
	--user-data-dir="$profile" \
	--virtual-time-budget=60000 \
	--dump-dom "http://127.0.0.1:$port/ui/test/index.html" >"$dump" 2>/dev/null; then
	echo "chromium did not finish within the time allowed; it wrote $(wc -c <"$dump") bytes" >&2
	exit 1
fi

python3 -c '
import re, sys

dom = sys.stdin.read()
m = re.search(r"<div id=\"summary\"[^>]*>([^<]*)", dom)
summary = m.group(1).strip() if m else ""

if not summary or summary == "running…":
    print("the page never finished: summary is %r" % summary, file=sys.stderr)
    sys.exit(1)

if re.fullmatch(r"all \d+ tests passed", summary):
    print(summary)
    sys.exit(0)

print(summary, file=sys.stderr)
# Names and reasons in the order the page listed them: a .fail li, each
# followed by its .err div when the failure carried a message.
for tag, text in re.findall(r"<li class=\"fail\">([^<]*)|<div class=\"err\">([^<]*)", dom):
    if tag:
        print("  \u2717 " + tag.strip(), file=sys.stderr)
    elif text:
        print("    " + text.strip(), file=sys.stderr)
sys.exit(1)
' <"$dump"
