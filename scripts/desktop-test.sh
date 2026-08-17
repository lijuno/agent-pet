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

P="${DIGITAL_PET_ADDR:-127.0.0.1:9876}"
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
menu=$(field status_menu)
for item in "Show Pet" "Pet Status" "Statistics" "Change Pet" "Always on Top" "Mute" "Quit"; do
	want "the menu offers $item" "$menu" "$item"
done

echo
echo "Every menu item survives being clicked"
# The suite used to click only Show Pet. Three of the others emitted an event
# from the main thread and killed the process, and nothing here noticed.
for item in show show status stats change ontop ontop mute mute sleep pet:byte pet:momo; do
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
menu=$(field status_menu)
want "it lists the cat"   "$menu" ":>SanMao"
want "it lists the robot" "$menu" ":>Byte"
want "the character in use is ticked" "$menu" ">SanMao (三毛)[on]"

post '{"status_item":"pet:byte"}'
sleep 0.8
want "picking one switches the character" "$(field status_menu)" ">Byte[on]"

# The tick has to follow a change made anywhere, not only from this menu.
curl -sS --max-time 5 -X POST "$BASE/pet" -H 'content-type: application/json' \
	-d '{"id":"momo"}' >/dev/null
sleep 0.8
want "and follows a change made elsewhere" "$(field status_menu)" ">SanMao (三毛)[on]"

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
	if [ "$x" -ge 0 ] && [ "$y" -ge 0 ] && [ "$x" -le "$((uw - 300))" ] && [ "$y" -le "$((uh - 184))" ]; then
		ok "recovered from $name ($win)"
	else
		bad "recovered from $name" "still outside the usable ${uw}x${uh}: $win"
	fi
done

echo
echo "Overlays stay on screen in every corner"
for corner in "0,0 top-left" "$((uw - 300)),0 top-right" \
	"0,$((uh - 184)) bottom-left" "$((uw - 300)),$((uh - 184)) bottom-right" \
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
