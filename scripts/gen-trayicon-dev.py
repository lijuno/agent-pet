#!/usr/bin/env python3
"""Derive the dev app's menu-bar icon from the release one.

    python3 scripts/gen-trayicon-dev.py

Both apps can be installed and running at once (ADR 0008), which means two
icons in the menu bar. Identical ones would be worse than useless: they are the
only way to tell which pet you are about to click, and the app has no Dock icon
or app-switcher entry to fall back on.

The icon is a macOS *template* image — the system throws the colour away and
recolours it for light and dark menu bars — so the two must differ in **shape**,
not in tint. This adds a filled dot in the bottom-right corner, with a
transparent gap around it so it reads as a badge rather than as part of the cat.

Committed, not generated at build time, so building needs no Pillow.
"""
import os
import sys

try:
    from PIL import Image, ImageDraw
except ImportError:
    sys.exit("this script needs Pillow:  pip3 install pillow")

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(HERE, "internal", "desktop", "trayicon.png")
DST = os.path.join(HERE, "internal", "desktop", "trayicon-dev.png")

# Bottom-right, where the silhouette has nothing. The gap is what makes it a
# badge: without it the dot merges into the cat's cheek at 18 points.
CENTER = (37, 37)
RADIUS = 5
GAP = 3


def main():
    im = Image.open(SRC).convert("RGBA")
    w, h = im.size

    # Punch the gap first, then draw the dot inside it.
    hole = Image.new("L", (w, h), 0)
    ImageDraw.Draw(hole).ellipse(
        [CENTER[0] - RADIUS - GAP, CENTER[1] - RADIUS - GAP,
         CENTER[0] + RADIUS + GAP, CENTER[1] + RADIUS + GAP], fill=255)
    alpha = im.split()[3]
    alpha.paste(0, (0, 0), hole)
    im.putalpha(alpha)

    dot = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    ImageDraw.Draw(dot).ellipse(
        [CENTER[0] - RADIUS, CENTER[1] - RADIUS,
         CENTER[0] + RADIUS, CENTER[1] + RADIUS], fill=(0, 0, 0, 255))
    im = Image.alpha_composite(im, dot)

    im.save(DST)
    print("wrote", os.path.relpath(DST, HERE))


if __name__ == "__main__":
    main()
