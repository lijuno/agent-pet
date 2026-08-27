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
# park asks for a position and waits for the window to stop moving, then says
# where it actually ended up.
#
# macOS does not have to honour a position. A window asked to hang off an edge
# may be put back, and whether it is depends on the machine: with a second
# display beside this one, "off the left" is not off anything, it is the other
# screen. move_to insists on the exact coordinate, which is right for parking
# somewhere reachable and wrong for these — every one of them failed on a
# docked laptop, which is a test that only works on the machine it was written
# on. What these checks are about is what the program does with the position it
# ends up at, so that is what they are given.
park() {
	want="$1,$2"
	before=$(field window)
	post "{\"x\":$1,\"y\":$2}"
	last=""
	stable=0
	i=0
	while [ "$i" -lt 40 ]; do
		now=$(field window)
		case "$now" in
		*"at $want")
			echo "$now"
			return
			;;
		esac
		# Settled somewhere else. Two conditions before believing that, both
		# of them paid for by a check that passed from a position nobody asked
		# about: it has to have moved at all — two reads taken before the main
		# thread applies the move are equal to each other — and it has to have
		# held still for half a second. Closing the overlay from the previous
		# spot gives the window's room back, which is itself a move, and a
		# park posted into the middle of it settles wherever that lands.
		if [ "$now" = "$last" ]; then
			stable=$((stable + 1))
		else
			stable=0
		fi
		if [ "$now" != "$before" ] && [ "$stable" -ge 4 ]; then
			echo "$now"
			return
		fi
		last=$now
		sleep 0.12
		i=$((i + 1))
	done
	echo "$last"
}
# settle waits for the window to stop moving on its own.
#
# Closing an overlay gives the window's room back, and that is a move the app
# makes after the request returns. A park posted while it is still in flight is
# applied and then overridden by it, so the next check runs from wherever the
# reflow finished — which is how "menu at top-right" spent a while passing from
# the middle of the screen.
settle() {
	last=""
	stable=0
	i=0
	while [ "$i" -lt 30 ]; do
		now=$(field window)
		if [ "$now" = "$last" ]; then
			stable=$((stable + 1))
		else
			stable=0
		fi
		if [ "$stable" -ge 3 ]; then
			return
		fi
		last=$now
		sleep 0.1
		i=$((i + 1))
	done
}

# xy_of pulls the coordinates out of "300x184 at 40,40".
x_of() { echo "$1" | sed 's/.* at //; s/,.*//'; }
y_of() { echo "$1" | sed 's/.*,//'; }

ok()   { printf '  \033[32mok\033[0m   %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n     %s\n' "$1" "$2"; fails=$((fails + 1)); }
want() { # want <name> <haystack> <needle>
	case "$2" in
	*"$3"*) ok "$1" ;;
	*) bad "$1" "expected '$3' in: $2" ;;
	esac
}
skip() { printf '  \033[33mskip\033[0m %s\n     %s\n' "$1" "$2"; }
gone() { # gone <name> <haystack> <needle>
	case "$2" in
	*"$3"*) bad "$1" "did not expect '$3' in: $2" ;;
	*) ok "$1" ;;
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
for item in "Status" "Change Character" "Always on Top" "Hide" "Reload" "File a Bug" "About" "Quit"; do
	want "the menu offers $item" "$menu" "$item"
done
# Counters nobody asked for. They live on in `petctl status`, where somebody
# curious can go and look, rather than in a menu everybody has to read past.
gone "the menu no longer offers Statistics" "$menu" "Statistics"
# The order, by tag rather than by title: 0 is the state line, then Status,
# Change Character, Always on Top, Hide, Reload, File a Bug, About, the update
# item and Quit. Tags because they do not move when a title is reworded, and
# because the state line's title is a state and the update item's is empty.
# Submenu entries — the characters, which come after their parent — are the
# ':>' lines, and are not part of this.
order=$(echo "$menu" | tr '|' '\n' | grep -v ':>' | grep . | cut -d: -f1 | tr '\n' ' ')
want "the menu is in the order somebody chose" "$order" "0 2 4 5 1 10 11 9 7 8"

echo
echo "Every menu item survives being clicked"
# The suite used to click only the visibility toggle. Three of the others emitted an event
# from the main thread and killed the process, and nothing here noticed.
# bug-open is deliberately absent: it opens the issue tracker in a browser, and
# a test suite that launches a browser window is a test suite nobody runs twice.
for item in hide hide status about change ontop ontop pet:sanmao reload update bug bug-copy; do
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
# About is a real window and the loop above opened it. Nothing else closes it,
# and leaving one on screen after a test run is the same wart as leaving a fake
# update in the menu bar.
post '{"panel":"about-close"}'
post '{"panel":"bug-close"}'

# Leave the toggles as they were found.
post '{"shown":true}'
sleep 0.4

echo
echo "Change Character is a submenu of the menu bar menu"
# Start from a known character. This section used to assert on whichever pet
# the last run — or a stray click — happened to leave active, so it passed in
# sequence and failed on its own.
curl -sS --max-time 5 -X POST "$BASE/pet" -H 'content-type: application/json' \
	-d '{"id":"sanmao"}' >/dev/null
sleep 0.6
menu=$(field status_menu)
want "it lists the cat" "$menu" ":>Sanmao"
want "it lists the girl" "$menu" ">Peach"
want "it lists the man" "$menu" ">Juanmao"
want "it lists the boy" "$menu" ">Maomao"
want "it lists the teenager" "$menu" ">Damao"
want "it lists the woman" "$menu" ">Amiao"
want "the character in use is ticked" "$menu" ">Sanmao (三毛)[on]"

# Switching needs somewhere to switch to. Six characters ship, so this runs;
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
		-d '{"id":"sanmao"}' >/dev/null
	sleep 0.8
	want "and follows a change made elsewhere" "$(field status_menu)" ">Sanmao (三毛)[on]"
fi

echo
echo "Updates"
# Stateful, like the rest of this file. A previous run leaves an update showing,
# so the "nothing found yet" state has to be restored rather than assumed —
# asserting on what the last run left behind passes in sequence and fails alone.
#
# Cleared by reporting the running version, not by posting available:false. A
# result carrying no version is deliberately not remembered, so the bare form
# leaves the last real one — 9.9.9, from the run before — in the file beside the
# config, where the next start reads it back. That is how a fake update outlived
# the quit this used to promise it would not.
clear_update() {
	cur=$(curl -sS --max-time 5 "$BASE/update" |
		python3 -c "import sys,json;print(json.load(sys.stdin)['current'])")
	curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
		-d "{\"latest\":\"$cur\",\"available\":false}" >/dev/null
}
clear_update
sleep 0.4

# The channel is a build fact, not a setting: the other channel is the other
# application (ADR 0008). So there is no picker, and its absence is worth
# asserting — a stray one would mean the switching code came back.
menu=$(field status_menu)
case "$menu" in
*"Update Channel"*) bad "the menu offers no channel picker" "still there: $menu" ;;
*) ok "the menu offers no channel picker" ;;
esac

# Nothing to install, nothing in the menu. The item used to be permanent and
# report the last check — "Up to date", "Nothing published yet" — which is a
# line the menu carried every day to be useful on the rare one. Asserted by tag
# rather than by title because a hidden item has no title worth reading: 7 is
# PET_UPDATE, and [hidden] is the dump's way of saying it is not on screen.
want "no update item when there is nothing to install" "$menu" "7:[hidden][off]"

# The app cannot find an update by itself — it holds no HTTP client. This is
# petctl's half of the conversation, which is the only way one arrives.
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"latest":"9.9.9","available":true}' >/dev/null
sleep 0.6
menu=$(field status_menu)
want "a reported update names the version" "$menu" "Update to 9.9.9…"
# Pressable only when there is a page behind it, which a real result from
# petctl always has — the manifest carries notes_url. Posted separately from
# the click below, which deliberately has none so it opens no browser.
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"latest":"9.9.9","available":true,
	     "notes_url":"https://github.com/lijuno/agent-pet/releases/tag/v9.9.9"}' >/dev/null
sleep 0.5
case "$(field status_menu)" in
*"Update to 9.9.9…[off]"* | *"Update to 9.9.9…[hidden]"*)
	bad "and the item becomes pressable" "not pressable" ;;
*) ok "and the item becomes pressable" ;;
esac

# And a check that finds nothing takes the item away again, rather than leaving
# an offer standing that was true a minute ago. "Up to date" is still an answer
# — it is in the Pet Status panel, with the time of the check beside it.
clear_update
sleep 0.5
menu=$(field status_menu)
want "a check that finds nothing takes it away" "$menu" "7:[hidden]"
gone "and leaves no version behind" "$menu" "Update to"
curl -sS --max-time 5 -X POST "$BASE/update" -H 'content-type: application/json' \
	-d '{"latest":"9.9.9","available":true}' >/dev/null
sleep 0.5

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
# whose menu bar otherwise offers "Update to 9.9.9…" — and now does so across
# restarts, since the result is kept in a file. A stale test is a nuisance, but
# a product telling somebody a release exists when it does not is a different
# kind of wrong.
clear_update

echo
echo "About is a window of its own"
# Parked in a corner first: the whole point of a separate window is that it
# turns up where the eye is, not wherever the pet was last dragged.
move_to 40 40 || bad "parking the pet" "the window never reached 40,40"
post '{"panel":"about"}'
sleep 1
got=$(field about)
case "$got" in
*"0,0 from centre"*) ok "About opens in the middle of the screen" ;;
*) bad "About opens in the middle of the screen" "$got" ;;
esac
want "and leaves the pet where it was" "$(field window)" "at 40,40"
# The rows here are this machine's config and pets paths, and they are as long
# as somebody's home directory makes them. Truncated, the end goes missing —
# and the end is the half that says which of the two apps this is.
want "nothing is cut off" "$got" "text fits"
post '{"panel":"about-close"}'
sleep 0.5
want "and closes again" "$(field about)" "closed"

echo
echo "File a Bug says how to report one"
post '{"panel":"bug"}'
sleep 1
got=$(field bug_report)
case "$got" in
*"0,0 from centre"*) ok "it opens in the middle of the screen" ;;
*) bad "it opens in the middle of the screen" "$got" ;;
esac
want "it offers the details" "$got" "Copy Report"
# The paths in here are as long as this machine's home directory makes them,
# and the log path is the reason somebody opens this window at all.
want "nothing is cut off" "$got" "text fits"
# Neither of these is clicked, here or in the loop above: one opens a browser
# and the other opens the Finder. What they build — the URL and the file — is
# covered by TestPrefilledIssueCarriesTheDetails and TestSavedReportHoldsTheFiles.
want "a file to attach" "$got" "Save Report"
want "and a way to post one" "$got" "Report on GitHub"
# This puts the details on the clipboard, over whatever was there. Nothing else
# in this suite touches anything outside the app, so it is worth saying: the
# button exists to write the clipboard, and a check that would not let it do
# that cannot tell whether it works.
#
# Copying changes nothing anybody can see except the button, which is the whole
# reason the button says so. A press with no visible answer reads as a button
# that does nothing, and there is no badge in a native window to say otherwise.
post '{"status_item":"bug-copy"}'
sleep 0.5
want "copying says so on the button" "$(field bug_report)" "Copied"
# And goes back: "Copied" is the answer to the press that just happened, not a
# permanent label.
sleep 1.6
gone "the button goes back to itself" "$(field bug_report)" "Copied"
post '{"panel":"bug-close"}'
sleep 0.5
want "and closes again" "$(field bug_report)" "closed"

echo
echo "Hide is a toggle, ticked for the state it names"
# Start from a known state. Asserting on whatever the last run left behind
# makes the first check depend on the previous one, across invocations.
post '{"shown":true}'
sleep 0.5
# Empty while the pet is on screen, which is nearly always: a box ticked
# whenever nothing is unusual tells nobody anything.
menu=$(field status_menu)
case "$menu" in
*"Hide[on]"*) bad "it is empty while the pet is on screen" "ticked: $menu" ;;
*"Hide"*) ok "it is empty while the pet is on screen" ;;
*) bad "it is empty while the pet is on screen" "no Hide item: $menu" ;;
esac
post '{"status_item":"hide"}'
sleep 0.8
want "clicking it hides the pet" "$(field status_menu)" "Hide[on]"
want "and the pet reports itself hidden" "$(field visible)" "no"
post '{"status_item":"hide"}'
sleep 0.8
menu=$(field status_menu)
case "$menu" in
*"Hide[on]"*) bad "clicking it again brings the pet back" "still ticked: $menu" ;;
*) ok "clicking it again brings the pet back" ;;
esac
want "and the pet reports itself visible" "$(field visible)" "yes"
# Hiding by any route must move the tick, not just clicking the item.
post '{"shown":false}'
sleep 0.6
want "hiding from elsewhere sets the tick" "$(field status_menu)" "Hide[on]"
post '{"shown":true}'
sleep 0.6
menu=$(field status_menu)
case "$menu" in
*"Hide[on]"*) bad "showing from elsewhere clears it again" "still ticked: $menu" ;;
*) ok "showing from elsewhere clears it again" ;;
esac

echo
echo "Hide finds a character parked off screen"
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
	parked=$(park "${pos%,*}" "${pos#*,}")
	px=$(x_of "$parked")
	py=$(y_of "$parked")
	# If the window is still inside the usable area, the system declined to put
	# it out of reach and there is nothing for Hide to rescue. That is a
	# fact about this desktop, not a fault in the pet.
	if [ "$px" -ge 0 ] && [ "$py" -ge 0 ] &&
		[ "$px" -le "$((uw - ww))" ] && [ "$py" -le "$((uh - wh))" ]; then
		skip "recovered from $name" "the system kept the window on screen ($parked); nothing was out of reach"
		continue
	fi
	# Showing, not toggling: the toggle would hide a pet that is already shown,
	# which the section above covers.
	post '{"shown":true}'
	sleep 0.8
	win=$(field window)
	x=$(x_of "$win")
	y=$(y_of "$win")
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
		# Wherever it settles. An overlay has to stay on screen from the
		# position the window is actually in, and asking for one the system
		# will not give is not a way to find that out.
		parked=$(park "${pos%,*}" "${pos#*,}")
		# Say so when the window is not where it was asked to be. The check is
		# still worth making from wherever it ended up — an overlay has to stay
		# on screen from any position — but a line reading "top-right" while
		# the window sat in the middle of the screen is a check nobody can
		# trust, and this happens often enough to name.
		where=""
		case "$parked" in
		*"at ${pos%,*},${pos#*,}") ;;
		*) where=" — the system put it at ${parked#* at }, not ${pos}" ;;
		esac
		post "{\"panel\":\"$kind\"}"
		sleep 1
		got=$(field overlay)
		case "$got" in
		*"fully on screen"*) ok "$kind at $name$where" ;;
		*) bad "$kind at $name" "at $parked: $got" ;;
		esac
		post '{"panel":"close"}'
		settle
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
