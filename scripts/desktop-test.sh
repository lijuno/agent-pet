#!/bin/sh
# Tests the parts of the pet that only exist on a desktop: the menu-bar item,
# and where menus land when the character is in a corner.
#
# None of this can be unit-tested and none of it can be seen from a test —
# accessibility access is refused to anything that tries to read a menu bar, so
# the app is asked about itself instead. It drives a running petd through the
# loopback API, doing only what a user can do with a mouse, and checks the
# answers.
#
# Run the app, then:  make test-desktop
set -eu

P="${AGENT_PET_ADDR:-127.0.0.1:9876}"
BASE="http://$P"
fails=0

post() {
	curl -sS --max-time 5 -X POST "$BASE/window" \
		-H 'content-type: application/json' -d "$1" >/dev/null
}
field() {
	curl -sS --max-time 5 "$BASE/diagnostics" |
		python3 -c "import sys,json;print(json.load(sys.stdin)['desktop'].get('$1',''))"
}
# move_to parks the window and waits until it has actually arrived. Wails
# applies a move on the main thread, so posting and sleeping is a race: the
# placement would be computed against the position the window is leaving, and
# the pending move then lands on top of the result. Poll instead of guess.
move_to() {
	post "{\"x\":$1,\"y\":$2}"
	i=0
	while [ "$i" -lt 40 ]; do
		case "$(field window)" in
		*"at $1,$2") return 0 ;;
		esac
		sleep 0.1
		i=$((i + 1))
	done
	return 1
}
ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n     %s\n' "$1" "$2"; fails=$((fails + 1)); }
want() { # want <name> <haystack> <needle>
	case "$2" in
	*"$3"*) ok "$1" ;;
	*) bad "$1" "expected '$3' in: $2" ;;
	esac
}

if ! curl -sS --max-time 3 "$BASE/healthz" >/dev/null 2>&1; then
	echo "petd is not running on $P — start the app first" >&2
	exit 1
fi

echo "Menu bar"
want "the status item is installed" "$(field menu_bar)" "installed"
# LSUIElement alone does not do this: Wails sets the activation policy back to
# Regular at launch, and the app puts it to Accessory afterwards. Nothing about
# that ordering is visible from the Go suite, and getting it wrong just means a
# Dock icon nobody notices in review.
want "the app keeps out of the Dock" "$(field dock)" "menu bar only"
menu=$(field status_menu)
for item in "Show Pet" "Pet Status" "Statistics" "Change Pet" "Always on Top" "Mute" "Quit"; do
	want "the menu offers $item" "$menu" "$item"
done

echo
echo "Every menu item survives being clicked"
# The suite used to click only Show Pet. Three of the others emitted an event
# from the main thread and killed the process, and nothing here noticed.
for item in show show status stats change ontop ontop mute mute pet:momo update; do
	if ! curl -sS --max-time 5 "$BASE/healthz" >/dev/null 2>&1; then
		bad "clicking $item" "petd is gone — an earlier item killed it"
		break
	fi
	post "{\"status_item\":\"$item\"}"
	sleep 0.6
	if curl -sS --max-time 5 "$BASE/healthz" >/dev/null 2>&1; then
		ok "clicking $item leaves the pet running"
	else
		bad "clicking $item" "the process died"
	fi
	post '{"panel":"close"}'
	sleep 0.2
done
# Leave the toggles as they were found.
post '{"shown":true}'
sleep 0.4

echo
echo "Change Pet is a submenu of the menu bar menu"
# Start from a known character. This section used to assert on whichever pet
# the last run — or a stray click — happened to leave active, so it passed in
# sequence and failed on its own.
curl -sS --max-time 5 -X POST "$BASE/pet" -H 'content-type: application/json' \
	-d '{"id":"momo"}' >/dev/null
sleep 0.6
menu=$(field status_menu)
want "it lists the cat" "$menu" ":>SanMao"
want "it lists the girl" "$menu" ">Peach"
want "the character in use is ticked" "$menu" ">SanMao (三毛)[on]"

# Switching needs somewhere to switch to. Two characters ship, so this runs;
# it stays guarded because a build with one pack should skip rather than fail.
others=$(curl -sS --max-time 5 "$BASE/pets" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(' '.join(p['id'] for p in d['pets'] if p['id'] != d['active']))")
if [ -z "$others" ]; then
	printf '  \033[33mskip\033[0m only one character is installed, so switching is untested\n'
else
	other=${others%% *}
	post "{\"status_item\":\"pet:$other\"}"
	sleep 0.8
	case "$(field status_menu)" in
	*"[on]"*) ok "picking one switches the character (to $other)" ;;
	*) bad "picking one switches the character" "nothing ticked after choosing $other" ;;
	esac
	# The tick has to follow a change made anywhere, not only from this menu.
	curl -sS --max-time 5 -X POST "$BASE/pet" -H 'content-type: application/json' \
		-d '{"id":"momo"}' >/dev/null
	sleep 0.8
	want "and follows a change made elsewhere" "$(field status_menu)" ">SanMao (三毛)[on]"
fi

echo
echo "Updates"
# Stateful, like the rest of this file. A previous run leaves an update showing,
# so the "nothing found yet" state has to be restored rather than assumed —
# asserting on what the last run left behind passes in sequence and fails alone.
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"available":false}' >/dev/null
sleep 0.4

# The channel is a build fact, not a setting: the other channel is the other
# application (ADR 0008). So there is no picker, and its absence is worth
# asserting — a stray one would mean the switching code came back.
menu=$(field status_menu)
case "$menu" in
*"Update Channel"*) bad "the menu offers no channel picker" "still there: $menu" ;;
*) ok "the menu offers no channel picker" ;;
esac

# Nothing has been checked, so there is nothing to say and the item is hidden.
# An item permanently announcing "no update available" is furniture.
want "the update item is hidden until there is one" "$menu" "Update Available[hidden]"

# The app cannot find an update by itself — it holds no HTTP client. This is
# petctl's half of the conversation, which is the only way one arrives.
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"latest":"9.9.9","available":true}' >/dev/null
sleep 0.6
menu=$(field status_menu)
want "a reported update names the version" "$menu" "Update to 9.9.9…"
case "$menu" in
*"Update to 9.9.9…[hidden]"*) bad "and the item becomes visible" "still hidden: $menu" ;;
*) ok "and the item becomes visible" ;;
esac

# Clicking it opens the release page. There is no notes URL in what was posted
# above, deliberately: this checks the click path without opening a browser
# window in the middle of a test run.
post '{"status_item":"update"}'
sleep 0.6
if curl -sS --max-time 5 "$BASE/healthz" >/dev/null 2>&1; then
	ok "clicking it leaves the pet running"
else
	bad "clicking it leaves the pet running" "the process died"
fi

# A result for the other channel is refused rather than stored: this build can
# never install it, so there is no moment at which showing it would be true.
mine=$(curl -sS --max-time 5 "$BASE/update" |
	python3 -c "import sys,json;print(json.load(sys.stdin)['channel'])")
other=dev
[ "$mine" = dev ] && other=release
code=$(curl -sS --max-time 5 -o /dev/null -w '%{http_code}' -X POST "$BASE/update" \
	-H 'content-type: application/json' \
	-d "{\"channel\":\"$other\",\"latest\":\"8.8.8\",\"available\":true}")
want "a $other result is refused by the $mine build" "$code" "400"
want "and the menu still shows the real one" "$(field status_menu)" "Update to 9.9.9…"

# Put it back on the way out as well as on the way in. Restoring at the top is
# what lets this section run alone; clearing here is for the human afterwards,
# whose menu bar otherwise offers "Update to 9.9.9…" until they next quit the
# app. A stale test is a nuisance, but a product telling somebody a release
# exists when it does not is a different kind of wrong.
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"available":false}' >/dev/null

echo
echo "Show Pet is a toggle"
# Start from a known state. Asserting on whatever the last run left behind
# makes the first check depend on the previous one, across invocations.
post '{"shown":true}'
sleep 0.5
want "it is ticked while the pet is on screen" "$(field status_menu)" "Show Pet[on]"
post '{"status_item":"show"}'
sleep 0.8
menu=$(field status_menu)
case "$menu" in
*"Show Pet[on]"*) bad "clicking it hides the pet" "still ticked: $menu" ;;
*"Show Pet"*) ok "clicking it hides the pet" ;;
*) bad "clicking it hides the pet" "no Show Pet item: $menu" ;;
esac
want "and the pet reports itself hidden" "$(field visible)" "no"
post '{"status_item":"show"}'
sleep 0.8
want "clicking it again brings the pet back" "$(field status_menu)" "Show Pet[on]"
want "and the pet reports itself visible" "$(field visible)" "yes"
# Hiding by any route must move the tick, not just clicking the item.
post '{"shown":false}'
sleep 0.6
menu=$(field status_menu)
case "$menu" in
*"Show Pet[on]"*) bad "hiding from elsewhere clears the tick" "still ticked: $menu" ;;
*) ok "hiding from elsewhere clears the tick" ;;
esac
post '{"shown":true}'
sleep 0.6
want "showing from elsewhere sets it again" "$(field status_menu)" "Show Pet[on]"

echo
echo "Show Pet finds a character parked off screen"
usable=$(field usable)
uw=$(echo "$usable" | sed 's/x.*//')
uh=$(echo "$usable" | sed 's/^[0-9]*x//; s/ .*//')
# The window is as big as the character, and the character has three sizes.
# These bounds used to assume the medium one, so the whole section failed on a
# pet set to Small — the test being wrong, not the pet.
base=$(field window_base)
ww=$(echo "$base" | sed 's/x.*//')
wh=$(echo "$base" | sed 's/^[0-9]*x//')
for spot in "$((uw - 40)),300 off the right" "-250,300 off the left" \
	"300,$((uh - 40)) off the bottom"; do
	pos=${spot%% *}
	name=${spot#* }
	move_to "${pos%,*}" "${pos#*,}" || bad "$name" "the window never reached ${pos}"
	# Showing, not toggling: the toggle would hide a pet that is already shown,
	# which the section above covers.
	post '{"shown":true}'
	sleep 0.8
	win=$(field window)
	x=$(echo "$win" | sed 's/.* at //; s/,.*//')
	y=$(echo "$win" | sed 's/.*,//')
	if [ "$x" -ge 0 ] && [ "$y" -ge 0 ] && [ "$x" -le "$((uw - ww))" ] && [ "$y" -le "$((uh - wh))" ]; then
		ok "recovered from $name ($win)"
	else
		bad "recovered from $name" "a ${ww}x${wh} window outside the usable ${uw}x${uh}: $win"
	fi
done

echo
echo "Overlays stay on screen in every corner"
for corner in "0,0 top-left" "$((uw - ww)),0 top-right" \
	"0,$((uh - wh)) bottom-left" "$((uw - ww)),$((uh - wh)) bottom-right" \
	"-200,400 hanging off the left" "$((uw - 60)),400 hanging off the right"; do
	pos=${corner%% *}
	name=${corner#* }
	for kind in menu status; do
		move_to "${pos%,*}" "${pos#*,}" || bad "$kind at $name" "the window never reached ${pos}"
		post "{\"panel\":\"$kind\"}"
		sleep 1
		got=$(field overlay)
		case "$got" in
		*"fully on screen"*) ok "$kind at $name" ;;
		*) bad "$kind at $name" "$got" ;;
		esac
		post '{"panel":"close"}'
		sleep 0.3
	done
done

# Leave the pet as it was found: on screen, with nothing open.
post '{"panel":"close"}'
post '{"shown":true}'

echo
if [ "$fails" -eq 0 ]; then
	printf '\033[32mall desktop checks passed\033[0m\n'
else
	printf '\033[31m%d desktop check(s) failed\033[0m\n' "$fails"
	exit 1
fi
