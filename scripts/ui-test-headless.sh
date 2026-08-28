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

# --password-store=basic and --use-mock-keychain keep the browser away from the
# system keyring; the rest keep it from reaching for a network, a profile or an
# extension it does not have. Without them a headless Chrome on a bare CI runner
# blocks on dbus rather than rendering anything — which is not a failure, it is
# a job that runs until the six-hour limit and is then killed.
flags="--headless --no-sandbox --disable-gpu --disable-dev-shm-usage
	--no-first-run --no-default-browser-check --disable-background-networking
	--disable-sync --disable-default-apps --disable-extensions
	--password-store=basic --use-mock-keychain"

# Every browser call is bounded, so one that hangs costs seconds and says so
# rather than sitting there. `timeout` is coreutils: on a Mac it is gtimeout, or
# it is missing altogether, and a local run manages without one.
tmo=""
for t in timeout gtimeout; do
	if command -v "$t" >/dev/null 2>&1; then
		tmo=$t
		break
	fi
done
bound() { # bound SECONDS command...
	local secs=$1
	shift
	if [ -n "$tmo" ]; then "$tmo" "$secs" "$@"; else "$@"; fi
}

# Under $HOME, not /tmp. Ubuntu ships chromium as a snap, and snap confinement
# cannot see /tmp — so the browser cannot write its profile, and it does not say
# so, it hangs.
profile=$(mktemp -d "${HOME:-/tmp}/.agent-pet-uitest-XXXXXX")
trap 'rm -rf "$profile"' EXIT

# renders reports whether a browser can actually produce a DOM.
#
# Trying rather than trusting the first name on PATH, because they are not
# interchangeable. A GitHub runner carries both Google Chrome and a chromium,
# and the chromium returned nothing at all until the job hit its limit. Two
# seconds spent here is the difference between a suite that runs and a job
# nobody can explain.
marker="agent-pet-smoke-ok"
renders() {
	local out
	# shellcheck disable=SC2086 # $flags is meant to be word-split
	out=$(bound 30 "$1" $flags --user-data-dir="$profile/smoke" \
		--dump-dom "data:text/html,<b>$marker</b>" 2>/dev/null) || return 1
	case "$out" in
	*"$marker"*) return 0 ;;
	esac
	return 1
}

# Google Chrome before chromium: on a runner the first is a real installation
# and the second may be a snap. Playwright's copy first of all, because in a
# cloud session it is the only one there.
chrome=""
for c in "${CHROMIUM:-}" \
	"${PLAYWRIGHT_BROWSERS_PATH:-/opt/pw-browsers}/chromium" \
	google-chrome google-chrome-stable chromium chromium-browser \
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"; do
	[ -n "$c" ] || continue
	[ -x "$c" ] || command -v "$c" >/dev/null 2>&1 || continue
	if renders "$c"; then
		chrome="$c"
		break
	fi
	echo "skipping $c: it did not render a test page" >&2
done
if [ -z "$chrome" ]; then
	echo "no browser here could render a page. Set CHROMIUM=/path/to/chrome, or run 'make test-ui'" >&2
	exit 1
fi
# Which browser and which build, because when this breaks it is usually one of
# them behaving differently from another.
echo "$("$chrome" --version 2>/dev/null || echo "unknown build") at $chrome" >&2

# A fixed port is a port the last run may still be holding. Ask the kernel.
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')

# file:// will not do: the tests need same-origin access to the frame they load
# the real UI into.
python3 -m http.server "$port" --bind 127.0.0.1 >/dev/null 2>&1 &
server=$!
trap 'kill $server 2>/dev/null || true; rm -rf "$profile"' EXIT

for _ in $(seq 1 50); do
	if curl -sSf -o /dev/null "http://127.0.0.1:$port/ui/test/index.html" 2>/dev/null; then break; fi
	sleep 0.1
done

# Into a file rather than $( ). Chrome's zygote and renderer children inherit
# the pipe, and a command substitution waits for every writer to let go of it,
# so one lingering child hangs the script with the browser itself already gone.
dump="$profile/dom.html"
# shellcheck disable=SC2086 # $flags is meant to be word-split
if ! bound 120 "$chrome" $flags \
	--user-data-dir="$profile/run" \
	--virtual-time-budget=60000 \
	--dump-dom "http://127.0.0.1:$port/ui/test/index.html" >"$dump" 2>/dev/null; then
	echo "the browser did not finish within the time allowed; it wrote $(wc -c <"$dump") bytes" >&2
	exit 1
fi

# Parsed in python3, not sed or awk. This script already needs python3 for the
# server, the dump arrives as one 50KB line with no trailing newline, and the
# line-length limits and backslash dialects of the BSD tools differ from the GNU
# ones — a difference that would surface only on the Mac, only on a red suite,
# which is the worst possible time to find out. Exit 0 green, 1 otherwise.
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
        print("  ✗ " + tag.strip(), file=sys.stderr)
    elif text:
        print("    " + text.strip(), file=sys.stderr)
sys.exit(1)
' <"$dump"
