#!/usr/bin/env python3
"""Derive the dev app's Finder icon from the release one.

    python3 scripts/gen-appicon-dev.py

The two apps sit next to each other in /Applications (ADR 0008), so their icons
have to be told apart at a glance. Unlike the menu-bar icon this one is in full
colour, so the badge can be a coloured corner tag rather than a shape change.

Writes build/appicon-dev.png, which is committed. scripts/brand.sh turns it into
an .icns at build time with sips and iconutil, both of which ship with macOS —
so building the dev app needs no Pillow.
"""
import os
import sys

try:
    from PIL import Image, ImageDraw
except ImportError:
    sys.exit("this script needs Pillow:  pip3 install pillow")

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(HERE, "build", "appicon.png")
DST = os.path.join(HERE, "build", "appicon-dev.png")

# A badge in the bottom-right corner, the same place and the same idea as the
# menu-bar icon's — so the two marks read as the same thing in two sizes. The
# artwork leaves that corner empty, which is why a badge works here and a corner
# ribbon does not: a ribbon has nothing opaque to lie across.
BADGE = (240, 158, 30, 255)
EDGE = (90, 55, 5, 255)


def main():
    im = Image.open(SRC).convert("RGBA")
    w, h = im.size
    cx, cy = int(w * 0.80), int(h * 0.80)
    r = int(w * 0.15)
    gap = max(2, w // 64)

    # Punch a hole first so the badge never merges into the artwork, then draw
    # into it. Same two steps as gen-trayicon-dev.py.
    hole = Image.new("L", (w, h), 0)
    ImageDraw.Draw(hole).ellipse([cx - r - gap, cy - r - gap, cx + r + gap, cy + r + gap], fill=255)
    alpha = im.split()[3]
    alpha.paste(0, (0, 0), hole)
    im.putalpha(alpha)

    badge = Image.new("RGBA", (w, h), (0, 0, 0, 0))
    d = ImageDraw.Draw(badge)
    d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=BADGE,
              outline=EDGE, width=max(2, w // 96))
    im = Image.alpha_composite(im, badge)

    im.save(DST)
    print("wrote", os.path.relpath(DST, HERE))


if __name__ == "__main__":
    main()
