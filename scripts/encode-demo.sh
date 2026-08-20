#!/usr/bin/env bash
# Turn a screen recording into the two files a README needs: an MP4 that
# GitHub plays inline, and a GIF for everywhere that strips video — the plugin
# marketplace listing, and any mirror of the README that renders images only.
#
#   ./scripts/encode-demo.sh recording.mov
#   ./scripts/encode-demo.sh recording.mov --start 2.5 --duration 18 --width 720
#
# Both are silent. They autoplay muted everywhere they will be seen, so
# anything said out loud is lost — put it in the frame instead.
set -euo pipefail

command -v ffmpeg >/dev/null || { echo "needs ffmpeg: brew install ffmpeg" >&2; exit 1; }

SRC=""; START=""; DUR=""; WIDTH=""; OUTDIR="docs/media"
while [ $# -gt 0 ]; do
	case $1 in
	--start) START=$2; shift 2 ;;
	--duration) DUR=$2; shift 2 ;;
	--width) WIDTH=$2; shift 2 ;;
	--outdir) OUTDIR=$2; shift 2 ;;
	-h | --help) sed -n '2,${/^[^#]/q;p;}' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) SRC=$1; shift ;;
	esac
done
[ -n "$SRC" ] && [ -f "$SRC" ] || { echo "usage: $0 <recording.mov> [options]" >&2; exit 2; }
mkdir -p "$OUTDIR"

# macOS ships bash 3.2, where ${trim[@]+"${trim[@]}"} on an *empty* array is an unbound
# variable under `set -u`. The ${x+...} guard expands to nothing at all when
# the array is empty, which is the only form that works on both 3.2 and 5.
trim=()
[ -n "$START" ] && trim+=(-ss "$START")
[ -n "$DUR" ] && trim+=(-t "$DUR")

# Prefer recording at final size over rescaling here. The frame mixes terminal
# text with pixel art and no single filter flatters both: lanczos keeps the
# text legible and softens the sprite, neighbour does the reverse. Rescaling is
# offered because a Retina capture is 2x, and exactly halving is the one resize
# that costs nothing.
vf="scale=${WIDTH}:-2:flags=lanczos"
[ -z "$WIDTH" ] && vf="null"

base=$(basename "${SRC%.*}")
mp4="$OUTDIR/$base.mp4"
gif="$OUTDIR/$base.gif"

# yuv420p and +faststart are what make it play in a browser rather than
# download. -an because there is no audio worth carrying.
ffmpeg -hide_banner -loglevel error -y ${trim[@]+"${trim[@]}"} -i "$SRC" \
	-vf "$vf" -an -c:v libx264 -preset slow -crf 23 \
	-pix_fmt yuv420p -movflags +faststart "$mp4"

# Two passes: one palette for the whole clip, then apply it. A per-frame
# palette is what turns a GIF into a shimmering mess.
pal=$(mktemp -t pal).png
ffmpeg -hide_banner -loglevel error -y ${trim[@]+"${trim[@]}"} -i "$SRC" \
	-vf "$vf,fps=15,palettegen=max_colors=192:stats_mode=diff" "$pal"
ffmpeg -hide_banner -loglevel error -y ${trim[@]+"${trim[@]}"} -i "$SRC" -i "$pal" \
	-lavfi "$vf,fps=15[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=3" "$gif"
rm -f "$pal"

size() { du -h "$1" | cut -f1 | tr -d ' '; }
echo "  $mp4  $(size "$mp4")"
echo "  $gif  $(size "$gif")"
gk=$(du -k "$gif" | cut -f1)
if [ "$gk" -gt 5120 ]; then
	echo
	echo "  The GIF is over 5MB, which is slow in a README. Shorten it with"
	echo "  --duration, or narrow it with --width." >&2
fi
