#!/usr/bin/env bash
# Sign, notarize and staple the app bundle, and leave a zip ready to release.
#
#   make build
#   ./scripts/notarize.sh --profile agent-pet
#
# --profile names a keychain profile holding App Store Connect API credentials,
# created once with:
#
#   xcrun notarytool store-credentials agent-pet \
#     --key AuthKey_XXXX.p8 --key-id KEYID --issuer ISSUERID
#
# No credential is ever read by this script, written to the repository, or
# passed on a command line where `ps` would show it. In CI, set
# NOTARY_ISSUER_ID / NOTARY_KEY_ID / NOTARY_KEY_P8 instead of --profile.
set -euo pipefail

APP=""
PROFILE=""
IDENTITY=""
ENTITLEMENTS=""
SKIP_NOTARIZE=0

while [ $# -gt 0 ]; do
	case $1 in
	--app) APP=$2; shift 2 ;;
	--profile) PROFILE=$2; shift 2 ;;
	--identity) IDENTITY=$2; shift 2 ;;
	--entitlements) ENTITLEMENTS=$2; shift 2 ;;
	--skip-notarize) SKIP_NOTARIZE=1; shift ;;
	-h | --help) sed -n '2,${/^[^#]/q;p;}' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown option: $1" >&2; exit 2 ;;
	esac
done

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
die() { echo "error: $*" >&2; exit 1; }

# Discovered rather than hardcoded. The bundle has been renamed once already
# (digital-pet -> agent-pet); a literal name here would have survived that
# rename looking correct and signing nothing.
if [ -z "$APP" ]; then
	count=$(find build/bin -maxdepth 1 -name '*.app' 2>/dev/null | wc -l | tr -d ' ')
	case $count in
	0) die "no .app in build/bin — run make build first" ;;
	1) APP=$(find build/bin -maxdepth 1 -name '*.app') ;;
	*) die "$count bundles in build/bin — name one with --app" ;;
	esac
fi
[ -d "$APP" ] || die "no bundle at $APP — run make build first"

# ---- identity -------------------------------------------------------------

if [ -z "$IDENTITY" ]; then
	# Match the certificate type by name rather than taking the first identity:
	# "Apple Development" and "Mac App Distribution" also sign, and both make
	# notarization fail later with an error that does not mention the cause.
	IDENTITY=$(security find-identity -v -p codesigning |
		grep "Developer ID Application" | head -1 |
		sed 's/.*"\(.*\)".*/\1/') || true
fi
[ -n "$IDENTITY" ] || die "no 'Developer ID Application' certificate in the keychain.
  Create one at https://developer.apple.com/account/resources/certificates/add
  Apple Development and Mac App Distribution certificates cannot notarize."

step "Signing as: $IDENTITY"

ents=()
[ -n "$ENTITLEMENTS" ] && ents=(--entitlements "$ENTITLEMENTS")

# --options runtime is the hardened runtime, which notarization requires.
# --timestamp is what keeps the signature valid after the certificate expires.
sign() {
	codesign --force --sign "$IDENTITY" --options runtime --timestamp \
		${ents[@]+"${ents[@]}"} "$1"
}

# Inside out. Signing the bundle seals whatever is nested inside it, so a
# nested binary signed afterwards invalidates the seal. --deep is Apple's
# deprecated shortcut for this and applies the wrong options to nested code.
step "Signing nested binaries"
for b in "$APP/Contents/MacOS/petctl"; do
	[ -f "$b" ] || die "expected $b — is this bundle from make build?"
	echo "  $(basename "$b")"
	sign "$b"
done

step "Signing the bundle"
sign "$APP"

step "Verifying the signature"
codesign --verify --strict --verbose=2 "$APP"
codesign --display --verbose=2 "$APP" 2>&1 | grep -E "Authority|TeamIdentifier|flags" || true

# ---- package --------------------------------------------------------------

VERSION=$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP/Contents/Info.plist" 2>/dev/null || echo 0.0.0)
NAME=$(basename "$APP" .app)
OUT="build/bin/$NAME-$VERSION-universal.zip"

# ditto, not zip: a bundle carries symlinks and extended attributes that zip
# silently drops, and the result fails to launch in a way that looks like a
# signing problem.
step "Packaging $OUT"
rm -f "$OUT"
ditto -c -k --keepParent "$APP" "$OUT"

if [ "$SKIP_NOTARIZE" = 1 ]; then
	echo
	echo "Signed but not notarized (--skip-notarize)."
	echo "Gatekeeper will still refuse this on another machine."
	exit 0
fi

# ---- notarize -------------------------------------------------------------

creds=()
if [ -n "$PROFILE" ]; then
	creds=(--keychain-profile "$PROFILE")
elif [ -n "${NOTARY_ISSUER_ID:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_KEY_P8:-}" ]; then
	# CI. The key reaches notarytool as a file the runner deletes, never as an
	# argument, so it stays out of the process list and the build log.
	keyfile=$(mktemp -t notarykey)
	printf '%s' "$NOTARY_KEY_P8" | base64 --decode > "$keyfile" 2>/dev/null ||
		printf '%s' "$NOTARY_KEY_P8" > "$keyfile"
	trap 'rm -f "$keyfile"' EXIT
	creds=(--issuer "$NOTARY_ISSUER_ID" --key-id "$NOTARY_KEY_ID" --key "$keyfile")
else
	die "no notarization credentials.
  Locally:  --profile NAME, after xcrun notarytool store-credentials NAME ...
  In CI:    NOTARY_ISSUER_ID, NOTARY_KEY_ID and NOTARY_KEY_P8"
fi

step "Submitting to Apple (this takes a few minutes)"
if ! xcrun notarytool submit "$OUT" ${creds[@]+"${creds[@]}"} --wait --timeout 30m; then
	echo
	echo "Rejected. Ask Apple why — the log names the offending binary:" >&2
	echo "  xcrun notarytool history ${PROFILE:+--keychain-profile $PROFILE}" >&2
	echo "  xcrun notarytool log <submission-id> ${PROFILE:+--keychain-profile $PROFILE}" >&2
	exit 1
fi

# Stapling attaches the ticket to the bundle so it launches on a machine that
# is offline or cannot reach Apple. Without it the app depends on a network
# check at first launch.
step "Stapling the ticket"
xcrun stapler staple "$APP"

# The zip was made before stapling, so it holds an unstapled copy.
step "Repackaging with the ticket"
rm -f "$OUT"
ditto -c -k --keepParent "$APP" "$OUT"

step "What Gatekeeper says"
spctl -a -t exec -vv "$APP"
xcrun stapler validate "$APP"

echo
echo "  $OUT  ($(du -h "$OUT" | cut -f1))"
echo "  Ready to attach to a GitHub release."
