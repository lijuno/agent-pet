#!/usr/bin/env bash
# Cut a release and prepare the update manifest that offers it.
#
#   make release                  # release channel
#   make release CHANNEL=dev      # prereleases
#
# Builds, signs, notarizes, uploads the asset to a GitHub release, and writes
# updates/<channel>.json — but does not commit it. That last step is the one
# that matters: publishing an asset and offering it to everybody are two
# separate acts here, and the second one is a commit you make by hand.
#
# Until that commit is pushed, nothing any user runs will see the new version.
# You can build a release, live with it for a week, and then decide. See
# docs/adr/0008-over-the-air-updates.md.
#
# Everything happens on this machine, because signing does — see
# docs/signing.md for why there is no release job on a runner.
set -euo pipefail

CHANNEL="${CHANNEL:-release}"
BUILD=1
UPLOAD=1
NOTARIZE=1

while [ $# -gt 0 ]; do
	case $1 in
	--channel) CHANNEL=$2; shift 2 ;;
	--no-build) BUILD=0; shift ;;
	--no-upload) UPLOAD=0; shift ;;
	--skip-notarize) NOTARIZE=0; UPLOAD=0; shift ;;
	-h | --help) sed -n '2,${/^[^#]/q;p;}' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
die() { echo "error: $*" >&2; exit 1; }

case "$CHANNEL" in
release | dev) ;;
*) die "unknown channel '$CHANNEL' (release or dev)" ;;
esac

# ---- what version is this, and does everything agree? ---------------------

# Exactly on a tag, not merely descended from one. `git describe --abbrev=0`
# would happily report v0.1.0 from a commit twenty patches later, and the
# release would claim to be a version that is not what is in the tree.
#
# Picked by channel rather than by `git describe --exact-match`, because both
# apps are released from the same commit and so HEAD carries two tags — v0.1.0
# and v0.1.0-dev.1. `--exact-match` would return one of them arbitrarily, which
# is how the dev app ends up published as the release version.
TAG=""
for candidate in $(git tag --points-at HEAD); do
	v=${candidate#v}
	case "$CHANNEL" in
	release) [ "${v#*-}" = "$v" ] || continue ;; # release takes only plain versions
	dev) [ "${v#*-}" != "$v" ] || continue ;;    # dev takes only prereleases
	esac
	[ -z "$TAG" ] || die "HEAD has more than one $CHANNEL tag: $TAG and $candidate"
	TAG=$candidate
done
[ -n "$TAG" ] || die "HEAD has no tag for the $CHANNEL channel.

  A release is cut from a tag, because the tag is what the version number comes
  from and what the download URL will point at. The release app takes a plain
  version and the dev app takes a prerelease:

    git tag v0.2.0          && make release
    git tag v0.2.0-dev.1    && make release CHANNEL=dev

  HEAD currently has: $(git tag --points-at HEAD | tr '\n' ' ')"
VERSION=${TAG#v}

[ -z "$(git status --porcelain)" ] ||
	die "the working tree has uncommitted changes.

  What gets built here is not what is committed at $TAG, and there would be no
  way to tell afterwards. Commit or stash first."

# wails.json is not checked against the tag any more, and no longer can be: the
# release app and the dev app ship from the same commit at different versions,
# and that file holds one value. The bundle's version comes from the -ldflags
# stamp by way of scripts/brand.sh, which writes it into Info.plist — so it is
# made correct rather than checked, and wails.json is only the fallback for a
# build cut without a tag at all.

# A prerelease on the release channel would push a half-finished build to
# everybody. The reverse is fine: dev followers get stable versions too.
case "$VERSION:$CHANNEL" in
*-*:release) die "$VERSION is a prerelease; publish it to the dev channel (make release CHANNEL=dev)" ;;
esac

echo "Releasing $VERSION on the $CHANNEL channel."

# ---- build ----------------------------------------------------------------

if [ "$BUILD" = 1 ]; then
	step "Building the $CHANNEL app"
	# VERSION passed explicitly: the Makefile would work it out with
	# `git describe`, which cannot tell the two tags on this commit apart.
	make build CHANNEL="$CHANNEL" VERSION="$VERSION"
fi

# Both apps can be sitting in build/bin at once, so pick the one that says it is
# the flavor being published rather than whichever the filesystem lists first.
# Asked of the binary, so the slug is not spelled out a second time here.
APP=""
for candidate in build/bin/*.app; do
	[ -d "$candidate" ] || continue
	id=$("$candidate/Contents/MacOS/petd" --flavor 2>/dev/null |
		python3 -c "import json,sys; print(json.load(sys.stdin)['id'])" 2>/dev/null) || continue
	if [ "$id" = "$CHANNEL" ]; then
		APP=$candidate
		break
	fi
done
[ -n "$APP" ] || die "no $CHANNEL app in build/bin — run: make build CHANNEL=$CHANNEL"

# Three places carry the version and all three have to agree. A stale bundle
# left by a failed build is the reason this is checked rather than assumed:
# `wails build` failing leaves the previous one in place.
step "Checking the built bundle says $VERSION on the $CHANNEL channel"
PLIST=$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP/Contents/Info.plist")
[ "$PLIST" = "$VERSION" ] || die "the bundle says $PLIST, not $VERSION — rebuild"
for bin in petd petctl; do
	got=$("$APP/Contents/MacOS/$bin" --version | awk '{print $2}')
	[ "$got" = "$VERSION" ] || die "$bin reports $got, not $VERSION — rebuild"
	echo "  $bin $got"
done

# The flavor as well as the version. A release-stamped binary in a bundle named
# agent-pet-dev would publish the release build to dev followers, and the two
# are told apart by nothing else at this point.
BUILT=$("$APP/Contents/MacOS/petd" --flavor |
	python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
[ "$BUILT" = "$CHANNEL" ] ||
	die "the bundle is the $BUILT app but you are publishing to $CHANNEL — rebuild with CHANNEL=$CHANNEL"
echo "  $(basename "$APP") is the $BUILT app"

# ---- sign ------------------------------------------------------------------

if [ "$NOTARIZE" = 1 ]; then
	step "Signing and notarizing"
	./scripts/notarize.sh --app "$APP"
else
	step "Signing without notarizing"
	./scripts/notarize.sh --app "$APP" --skip-notarize
fi

NAME=$(basename "$APP" .app)
ZIP="build/bin/$NAME-$VERSION-universal.zip"
[ -f "$ZIP" ] || die "expected $ZIP and it is not there"

# ---- the manifest ----------------------------------------------------------

SHA=$(shasum -a 256 "$ZIP" | cut -d' ' -f1)
SIZE=$(stat -f%z "$ZIP")
URL="https://github.com/lijuno/agent-pet/releases/download/$TAG/$(basename "$ZIP")"
NOTES="https://github.com/lijuno/agent-pet/releases/tag/$TAG"
MIN_MACOS=${MIN_MACOS:-12.0}

if [ "$UPLOAD" = 1 ]; then
	step "Uploading to the GitHub release"
	command -v gh >/dev/null ||
		die "gh is not installed. It is used so no token has to live in this script:
  brew install gh && gh auth login"
	PRE=""
	case "$VERSION" in *-*) PRE="--prerelease" ;; esac
	if gh release view "$TAG" >/dev/null 2>&1; then
		echo "  the release exists; replacing the asset"
		gh release upload "$TAG" "$ZIP" --clobber
	else
		# shellcheck disable=SC2086
		gh release create "$TAG" "$ZIP" $PRE \
			--title "$VERSION" \
			--notes "Signed and notarized. Install with the plugin's install skill, or \`petctl update\`."
	fi
else
	echo
	echo "Not uploading (--no-upload). The manifest below names a URL that will"
	echo "404 until $(basename "$ZIP") is attached to $TAG."
fi

step "Writing updates/$CHANNEL.json"
mkdir -p updates
python3 - "$CHANNEL" "$VERSION" "$URL" "$SHA" "$SIZE" "$MIN_MACOS" "$NOTES" <<'PY'
import json, sys, datetime, subprocess

channel, version, url, sha, size, min_macos, notes = sys.argv[1:8]
# The tag's own date, not today's. Re-running this script must not change what
# it writes, or the diff you are about to review would be noise.
date = subprocess.run(
    ["git", "log", "-1", "--format=%cd", "--date=format:%Y-%m-%d"],
    capture_output=True, text=True, check=True).stdout.strip()

path = f"updates/{channel}.json"
with open(path, "w") as f:
    json.dump({
        "channel": channel,
        "version": version,
        "url": url,
        "sha256": sha,
        "size": int(size),
        "min_macos": min_macos,
        "published": date,
        "notes_url": notes,
    }, f, indent=2)
    f.write("\n")
print(f"  {path}")
PY

cat <<EOF

$(printf '\033[1m==> Built and published. Nobody is offered it yet.\033[0m')

  $ZIP
  sha256 $SHA

The manifest is written but not committed. That commit is the release: until
it is pushed, every installed pet still sees the previous version.

  git diff updates/$CHANNEL.json
  petctl update --check --channel $CHANNEL   # against the file as it will be

  git add updates/$CHANNEL.json
  git commit -m "Offer $VERSION on the $CHANNEL channel"
  git push
EOF
