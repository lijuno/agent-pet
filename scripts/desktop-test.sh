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
echo "Show Pet finds a character parked off screen"
usable=$(field usable)
uw=$(echo "$usable" | sed 's/x.*//')
uh=$(echo "$usable" | sed 's/^[0-9]*x//; s/ .*//')
for spot in "$((uw - 40)),300 off the right" "-250,300 off the left" \
	"300,$((uh - 40)) off the bottom"; do
	pos=${spot%% *}
	name=${spot#* }
	post "{\"x\":${pos%,*},\"y\":${pos#*,}}"
	sleep 0.4
	post '{"status_item":"show"}'
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
		post "{\"x\":${pos%,*},\"y\":${pos#*,}}"
		sleep 0.4
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

echo
if [ "$fails" -eq 0 ]; then
	printf '\033[32mall desktop checks passed\033[0m\n'
else
	printf '\033[31m%d desktop check(s) failed\033[0m\n' "$fails"
	exit 1
fi
