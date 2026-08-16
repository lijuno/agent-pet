#!/usr/bin/env python3
"""Generate the built-in pixel-art pet packs.

Every built-in pet is drawn from the same parametric creature, so the eleven
states read consistently across characters: same eye grammar, same prop
positions, same bob rhythm. Only the silhouette and palette change.

Output: ui/dist/pets/<id>/{manifest.json, <state>.png}
Each PNG is a horizontal strip of N frames, 40x40 each, with alpha.

Run:  python3 tools/genpets/genpets.py
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field

from PIL import Image, ImageChops, ImageDraw

W = H = 40
OUT_ROOT = os.path.join("ui", "dist", "pets")

# Every state a pack provides, with its frame count and frame rate.
# The slow rates matter: an idle pet redrawing three times a second is
# invisible on a CPU graph, which is the point (§5.2, low CPU when idle).
STATES = {
    "idle":      (4, 3),
    "thinking":  (4, 4),
    "working":   (4, 8),
    "attention": (4, 6),
    "confused":  (4, 5),
    "worried":   (4, 4),
    "happy":     (4, 6),
    "celebrate": (6, 10),
    "sleeping":  (4, 2),
    "tired":     (4, 2),
    "heart":     (4, 5),
}


@dataclass
class Palette:
    body: tuple
    body_dark: tuple
    belly: tuple
    line: tuple
    accent: tuple
    eye: tuple = (40, 32, 42, 255)
    blush: tuple = (242, 152, 152, 150)
    # Only a patched coat needs these; the robot and the slime leave them unset.
    patch: tuple = None
    white: tuple = (252, 250, 246, 255)
    nose: tuple = None


@dataclass
class Species:
    pid: str
    name: str
    description: str
    palette: Palette
    ears: str = "cat"       # cat | antenna | none
    face: str = "muzzle"    # muzzle | screen | soft
    tail: bool = True
    body_w: int = 24
    body_h: int = 22
    markings: str = "none"  # none | tortie
    extras: dict = field(default_factory=dict)


MOMO = Species(
    pid="momo",
    name="Momo",
    description="A tortoiseshell tabby with a white bib and white paws.",
    palette=Palette(
        # A ginger base with dark tortie patches over it, white underneath.
        body=(224, 158, 99, 255),
        body_dark=(188, 122, 68, 255),
        belly=(252, 250, 246, 255),
        line=(52, 38, 30, 255),
        accent=(232, 158, 156, 255),   # the pink inside an ear
        eye=(126, 156, 84, 255),       # green, going gold at the centre
        patch=(86, 62, 48, 255),
        white=(252, 250, 246, 255),
        nose=(214, 146, 138, 255),
    ),
    ears="cat", face="muzzle", tail=True, body_w=24, body_h=22,
    markings="tortie",
)

BYTE = Species(
    pid="byte",
    name="Byte",
    description="A little terminal robot with a blinking antenna.",
    palette=Palette(
        body=(158, 171, 188, 255),
        body_dark=(116, 131, 150, 255),
        belly=(214, 224, 236, 255),
        line=(38, 44, 54, 255),
        accent=(96, 218, 228, 255),
        eye=(96, 218, 228, 255),
        blush=(96, 218, 228, 90),
    ),
    ears="antenna", face="screen", tail=False, body_w=23, body_h=21,
)

PIP = Species(
    pid="pip",
    name="Pip",
    description="A cheerful slime. Very squishy.",
    palette=Palette(
        body=(140, 216, 166, 255),
        body_dark=(104, 184, 131, 255),
        belly=(216, 246, 226, 255),
        line=(34, 62, 45, 255),
        accent=(255, 212, 100, 255),
    ),
    ears="none", face="soft", tail=False, body_w=26, body_h=20,
)

SPECIES = [MOMO, BYTE, PIP]


# --------------------------------------------------------------------------
# drawing helpers
#
# Everything is drawn on a 40x40 grid. The character occupies a 24x24 box in
# the middle-bottom; the top-right corner is reserved for props (Z's, "!", "?",
# hearts) so a prop never collides with an ear or a tail.
# --------------------------------------------------------------------------

def px(d, x, y, c):
    if 0 <= x < W and 0 <= y < H:
        d.point((x, y), fill=c)


def rect(d, x0, y0, x1, y1, c):
    if x1 < x0:
        x0, x1 = x1, x0
    if y1 < y0:
        y0, y1 = y1, y0
    d.rectangle([x0, y0, x1, y1], fill=c)


def zzz(d, x, y, c, phase):
    """Three Z's rising and fading. The top one blinks out on some frames."""
    for i, (size, dy) in enumerate([(4, 0), (3, -7), (2, -13)]):
        if i == 2 and phase % 4 in (2, 3):
            continue
        if i == 1 and phase % 4 == 3:
            continue
        yy = y + dy - (phase % 2)
        xx = x + i * 3
        d.line([(xx, yy), (xx + size, yy)], fill=c)
        d.line([(xx + size, yy), (xx, yy + size)], fill=c)
        d.line([(xx, yy + size), (xx + size, yy + size)], fill=c)


def sparkle(d, x, y, c, size=2):
    d.line([(x - size, y), (x + size, y)], fill=c)
    d.line([(x, y - size), (x, y + size)], fill=c)
    if size > 1:
        px(d, x - 1, y - 1, c)
        px(d, x + 1, y + 1, c)


def heart(d, x, y, c, big=True):
    if big:
        d.line([(x, y), (x + 1, y)], fill=c)
        d.line([(x + 3, y), (x + 4, y)], fill=c)
        d.line([(x - 1, y + 1), (x + 5, y + 1)], fill=c)
        d.line([(x - 1, y + 2), (x + 5, y + 2)], fill=c)
        d.line([(x, y + 3), (x + 4, y + 3)], fill=c)
        d.line([(x + 1, y + 4), (x + 3, y + 4)], fill=c)
        px(d, x + 2, y + 5, c)
    else:
        px(d, x, y, c); px(d, x + 2, y, c)
        d.line([(x - 1, y + 1), (x + 3, y + 1)], fill=c)
        d.line([(x, y + 2), (x + 2, y + 2)], fill=c)
        px(d, x + 1, y + 3, c)


def bang(d, x, y, c):
    """Exclamation mark — the attention signal, with a soft outline so it
    stays legible against a light desktop wallpaper."""
    rect(d, x, y, x + 2, y + 6, c)
    rect(d, x, y + 8, x + 2, y + 10, c)


def question(d, x, y, c):
    d.line([(x + 1, y), (x + 3, y)], fill=c)
    px(d, x, y + 1, c); px(d, x + 4, y + 1, c)
    px(d, x + 4, y + 2, c)
    d.line([(x + 2, y + 3), (x + 3, y + 3)], fill=c)
    px(d, x + 2, y + 4, c)
    px(d, x + 2, y + 6, c)


def sweat(d, x, y, c):
    px(d, x + 1, y, c)
    d.line([(x, y + 1), (x + 2, y + 1)], fill=c)
    d.line([(x, y + 2), (x + 2, y + 2)], fill=c)
    px(d, x + 1, y + 3, c)


def dots(d, x, y, c, phase):
    """Thinking dots, filling in one at a time then clearing."""
    shown = phase % 4
    for i in range(3):
        if i < shown:
            rect(d, x + i * 4, y - i, x + i * 4 + 2, y + 2 - i, c)


def coffee(d, x, y, line):
    """A mug, for the tired state."""
    rect(d, x, y, x + 6, y + 6, (250, 250, 250, 255))
    d.rectangle([x, y, x + 6, y + 6], outline=line)
    rect(d, x + 1, y + 1, x + 5, y + 2, (120, 76, 48, 255))
    d.line([(x + 7, y + 2), (x + 8, y + 3)], fill=line)
    px(d, x + 7, y + 4, line)


def steam(d, x, y, c, phase):
    for i in range(2):
        yy = y - (phase % 3) - i * 2
        px(d, x + i * 2, yy, c)


# --------------------------------------------------------------------------
# expression grammar
#
# Eyes carry the state; the mouth reinforces it. Both are drawn relative to a
# face centre so every species animates identically.
# --------------------------------------------------------------------------

def eyes(d, ex, ey, kind, pal, look=(0, 0)):
    lx, rx = ex - 6, ex + 3
    ox, oy = look
    c = pal.eye
    white = (255, 255, 255, 255)

    if kind == "dot":
        for x in (lx, rx):
            rect(d, x + ox, ey + oy, x + 2 + ox, ey + 2 + oy, c)
            px(d, x + ox, ey + oy, (255, 255, 255, 170))
    elif kind == "wide":
        for x in (lx - 1, rx - 1):
            d.ellipse([x, ey - 2, x + 4, ey + 3], fill=white, outline=pal.line)
            rect(d, x + 1 + ox, ey + oy, x + 2 + ox, ey + 1 + oy, c)
    elif kind == "happy":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey + 2), (x + 2, ey - 1)], fill=c)
            d.line([(x + 2, ey - 1), (x + 4, ey + 2)], fill=c)
    elif kind == "closed":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey + 1), (x + 4, ey + 1)], fill=c)
            px(d, x, ey, c)
            px(d, x + 4, ey, c)
    elif kind == "half":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey), (x + 4, ey)], fill=c)
            rect(d, x + 1, ey + 1, x + 2, ey + 2, c)
    elif kind == "squint":
        # A brow with a pupil under it. A bare bar would be indistinguishable
        # from the closed eyes of `sleeping` at this size.
        for x in (lx - 1, rx - 1):
            d.line([(x, ey - 2), (x + 4, ey - 2)], fill=pal.line)
            rect(d, x + 1, ey, x + 3, ey + 1, c)
    elif kind == "confused":
        d.ellipse([lx - 1, ey - 2, lx + 3, ey + 3], fill=white, outline=pal.line)
        rect(d, lx, ey, lx + 1, ey + 1, c)
        d.line([(rx - 1, ey + 1), (rx + 3, ey + 1)], fill=c)
    elif kind == "worried":
        # Brows angled sharply inward with a gap between them, over anxious
        # pupils. The gap is what makes it read as worry rather than a scowl.
        d.line([(lx - 1, ey - 4), (lx + 2, ey - 1)], fill=pal.line)
        d.line([(lx - 1, ey - 3), (lx + 2, ey)], fill=pal.line)
        d.line([(rx + 4, ey - 4), (rx + 1, ey - 1)], fill=pal.line)
        d.line([(rx + 4, ey - 3), (rx + 1, ey)], fill=pal.line)
        for x in (lx, rx + 1):
            rect(d, x, ey + 1, x + 1, ey + 3, c)
    elif kind == "sparkle":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey + 2), (x + 2, ey - 1)], fill=c)
            d.line([(x + 2, ey - 1), (x + 4, ey + 2)], fill=c)
        px(d, lx + 4, ey - 3, white)
        px(d, rx - 2, ey - 3, white)


def mouth(d, mx, my, kind, pal, muzzle_style="cat"):
    c = pal.line
    # A pink nose, on a cat that has one. Drawn under the mouth so the mouth
    # line still reads as the boundary of the muzzle.
    if pal.nose and muzzle_style == "cat" and kind in ("cat", "flat", "smile", "wobble"):
        rect(d, mx - 1, my - 1, mx + 1, my, pal.nose)
    if kind == "smile":
        d.line([(mx - 2, my), (mx, my + 2)], fill=c)
        d.line([(mx, my + 2), (mx + 2, my)], fill=c)
    elif kind == "wide_smile":
        d.line([(mx - 3, my), (mx - 2, my + 2)], fill=c)
        d.line([(mx - 2, my + 2), (mx + 2, my + 2)], fill=c)
        d.line([(mx + 2, my + 2), (mx + 3, my)], fill=c)
        rect(d, mx - 1, my + 3, mx + 1, my + 3, (196, 96, 106, 255))
    elif kind == "o":
        d.ellipse([mx - 2, my, mx + 2, my + 3], fill=(120, 60, 62, 255), outline=c)
    elif kind == "flat":
        d.line([(mx - 2, my + 1), (mx + 2, my + 1)], fill=c)
    elif kind == "wobble":
        px(d, mx - 3, my + 2, c); px(d, mx - 2, my + 1, c)
        px(d, mx - 1, my + 2, c); px(d, mx, my + 1, c)
        px(d, mx + 1, my + 2, c); px(d, mx + 2, my + 1, c)
    elif kind == "cat":
        if muzzle_style == "cat":
            px(d, mx, my - 1, c)
            d.line([(mx - 1, my), (mx + 1, my)], fill=c)
            d.line([(mx - 3, my + 2), (mx - 1, my + 1)], fill=c)
            d.line([(mx + 1, my + 1), (mx + 3, my + 2)], fill=c)
        else:
            d.line([(mx - 2, my), (mx, my + 2)], fill=c)
            d.line([(mx, my + 2), (mx + 2, my)], fill=c)


# --------------------------------------------------------------------------
# the character
# --------------------------------------------------------------------------

def blush(d, s, cx, ey, pal):
    """Cheeks. Skipped for screen faces: a robot has no cheeks, and the marks
    would land outside the panel and read as damage."""
    if s.face == "screen":
        return
    d.ellipse([cx - 10, ey + 3, cx - 7, ey + 5], fill=pal.blush)
    d.ellipse([cx + 7, ey + 3, cx + 10, ey + 5], fill=pal.blush)


def markings(img, s, top, bw, bh, cy, pal):
    """A tortoiseshell tabby's coat: a dark cap with brow stripes, tabby bars
    down one flank, and a white bib.

    Every mark is drawn freely and then clipped to the body ellipse, so nothing
    can spill past the silhouette however the squash-and-stretch deforms it.
    Drawing them inside the ellipse arithmetic instead would mean redoing that
    arithmetic in four places and getting it wrong in one.

    The asymmetry is deliberate. A tortie's patches are never mirrored, and a
    symmetrical coat reads as a pattern rather than as an animal.
    """
    if s.markings != "tortie":
        return

    overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    o = ImageDraw.Draw(overlay)
    x0, x1 = CX - bw // 2, CX + bw // 2
    eye_y = cy - 4

    # The dark cap over the top of the head.
    o.ellipse([x0 - 3, top - 5, x1 + 3, top + 6], fill=pal.patch)

    # Tabby brow stripes running down out of the cap. They stop short of the
    # eyes: a stripe crossing an eye costs the expression, and the expression
    # is the whole point of the character.
    for dx, extra in ((-6, 0), (-2, 2), (2, 2), (6, 0)):
        o.line([(CX + dx, top + 3), (CX + dx, top + 6 + extra)], fill=pal.patch)

    # One patch down the left flank, below eye level, with bars over it.
    o.ellipse([x0 - 5, eye_y + 2, x0 + 7, top + bh + 2], fill=pal.patch)
    for k in range(3):
        y = eye_y + 4 + k * 3
        o.line([(x1 - 6, y), (x1 + 2, y)], fill=pal.patch)

    # The white bib. Narrower than the muzzle above it, so the two read as
    # chin-then-chest rather than merging into one white disc — which is what
    # a bib as wide as the body does, and it swallows the ginger cheeks.
    o.polygon([(CX - 4, top + bh - 4), (CX + 4, top + bh - 4),
               (CX + 6, top + bh + 4), (CX - 6, top + bh + 4)], fill=pal.white)

    # Clip to the body, inset by one pixel so the outline stays unbroken.
    mask = Image.new("L", (W, H), 0)
    ImageDraw.Draw(mask).ellipse([x0 + 1, top + 1, x1 - 1, top + bh - 1], fill=255)
    overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
    img.alpha_composite(overlay)


def whiskers(d, cx, my, pal):
    """Two short strokes a side, starting at the edge of the muzzle.

    They have to stay inside the silhouette and stay short. Full-length
    whiskers at 40px are wider than the cat, and they read as bars laid across
    the face rather than as whiskers — they also wipe out the tabby bars and
    the ginger cheeks they cross.
    """
    for sx in (-1, 1):
        for k, dy in enumerate((0, 2)):
            x = cx + sx * 7
            d.line([(x, my + dy), (x + sx * 4, my + dy - 1 + k)], fill=pal.white)


BOB = {
    "idle":      [0, 0, 1, 0],
    "thinking":  [0, 0, 1, 1],
    "working":   [0, 1, 0, -1],
    "attention": [-2, 0, -3, 0],
    "confused":  [0, 1, 0, 1],
    "worried":   [0, 0, 1, 1],
    "happy":     [-1, 0, -2, 0],
    "celebrate": [-2, -4, -1, 0, -2, -4],
    "sleeping":  [0, 0, 1, 1],
    "tired":     [1, 1, 2, 2],
    "heart":     [-1, 0, -1, 0],
}

CX = 19          # character centre x
GROUND = 35      # where the shadow sits
PROP_X = 27      # left edge of the prop column, top-right


def draw_pet(s, state, i, n):
    img = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    pal = s.palette

    bob = BOB.get(state, [0])[i % len(BOB.get(state, [0]))]
    cy = 23 + bob
    bw, bh = s.body_w, s.body_h

    # Squash and stretch. A body that only translates looks like a sticker;
    # one that deforms slightly on the beat looks alive.
    if bob < 0:
        bw, bh = bw - 1, bh + 1
    elif bob > 0:
        bw, bh = bw + 1, bh - 1
    if state == "tired":
        bw, bh = bw + 2, bh - 2

    # ground shadow — shrinks as the pet rises, which is what sells the hop
    shadow_w = bw - 2 + bob
    d.ellipse([CX - shadow_w // 2, GROUND, CX + shadow_w // 2, GROUND + 3],
              fill=(0, 0, 0, 60))

    top = cy - bh // 2

    # --- tail, behind the body, bottom-left so it never hits the prop column
    if s.tail:
        wag = [0, 1, 2, 1][i % 4] if state in ("happy", "celebrate", "heart", "working") else [0, 0, 1, 0][i % 4]
        tx, ty = CX - bw // 2 + 3, cy + bh // 2 - 2
        d.line([(tx, ty), (tx - 6, ty + 1)], fill=pal.body_dark, width=3)
        d.line([(tx - 6, ty + 1), (tx - 9, ty - 3 - wag)], fill=pal.body_dark, width=2)
        d.line([(tx - 9, ty - 3 - wag), (tx - 9, ty - 6 - wag * 2)], fill=pal.body_dark, width=2)
        px(d, tx - 8, ty - 7 - wag * 2, pal.body)

    # --- ears / antenna, behind the head outline
    if s.ears == "cat":
        for sx in (-1, 1):
            ex = CX + sx * (bw // 2 - 4)
            d.polygon([(ex - 4 * sx, top + 4), (ex - sx, top - 5), (ex + 4 * sx, top + 3)],
                      fill=pal.body, outline=pal.line)
            d.polygon([(ex - 2 * sx, top + 2), (ex - sx, top - 2), (ex + 2 * sx, top + 2)],
                      fill=pal.accent)
    elif s.ears == "antenna":
        d.line([(CX, top - 6), (CX, top + 2)], fill=pal.line, width=2)
        lit = state in ("working", "attention", "celebrate") or i % 2 == 0
        if state == "sleeping":
            lit = False
        bulb = pal.accent if lit else pal.body_dark
        d.ellipse([CX - 3, top - 10, CX + 2, top - 5], fill=bulb, outline=pal.line)
        # side plates
        for sx in (-1, 1):
            x = CX + sx * (bw // 2)
            rect(d, x - 1, cy - 3, x + 1, cy + 3, pal.body_dark)
            d.ellipse([x - 1, cy - 3, x + 1, cy + 3], outline=pal.line)

    # --- body
    d.ellipse([CX - bw // 2, top, CX + bw // 2, top + bh], fill=pal.body, outline=pal.line)
    markings(img, s, top, bw, bh, cy, pal)

    # White socks, on a cat that has them.
    paw = pal.white if s.markings == "tortie" else pal.body

    # --- limbs that must sit on top of the body
    if state == "celebrate":
        for sx in (-1, 1):
            ax = CX + sx * (bw // 2 - 1)
            d.line([(ax, cy + 1), (ax + sx * 4, cy - 6)], fill=pal.body_dark, width=3)
            d.ellipse([ax + sx * 4 - 2, cy - 9, ax + sx * 4 + 2, cy - 5],
                      fill=paw, outline=pal.line)
    elif state == "working":
        # A tiny keyboard with two paws tapping out of phase.
        ky = cy + bh // 2 - 1
        rect(d, CX - 9, ky + 2, CX + 9, ky + 4, pal.body_dark)
        d.rectangle([CX - 9, ky + 2, CX + 9, ky + 4], outline=pal.line)
        for k, sx in enumerate((-1, 1)):
            ax = CX + sx * 6
            oy = 0 if (i + k) % 2 else 2
            d.ellipse([ax - 3, ky - 3 + oy, ax + 2, ky + 2 + oy],
                      fill=paw, outline=pal.line)

    face_cx = CX
    ey = cy - 4
    my = cy + 4

    if s.face == "screen":
        # A robot reads better with a lit panel than with fur-and-muzzle.
        d.rounded_rectangle([CX - 8, cy - 8, CX + 8, cy + 4], radius=2,
                            fill=(38, 46, 58, 255), outline=pal.line)
        ey = cy - 4
        my = cy + 1
    elif s.face == "muzzle" and state not in ("sleeping",):
        d.ellipse([CX - 6, cy + 1, CX + 6, cy + 8], fill=pal.belly)
        my = cy + 4
    elif s.face == "muzzle":
        d.ellipse([CX - 6, cy + 1, CX + 6, cy + 8], fill=pal.belly)
        my = cy + 4
    else:
        # slime: a soft highlight instead of a muzzle
        d.ellipse([CX - 7, cy + 2, CX + 7, cy + 7], fill=pal.belly)
        my = cy + 4

    blink = i == 2 and state in ("idle", "thinking")
    mstyle = "cat" if s.face == "muzzle" else "round"

    # Whiskers sit under the expression, so a brow or a wide eye still wins.
    if s.markings == "tortie" and state != "sleeping":
        whiskers(d, face_cx, my, pal)

    if state == "idle":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal)
        mouth(d, face_cx, my, "cat", pal, mstyle)
    elif state == "thinking":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal, look=(-1, -1))
        mouth(d, face_cx, my, "flat", pal, mstyle)
        dots(d, PROP_X + 1, cy - 10, pal.line, i)
    elif state == "working":
        eyes(d, face_cx, ey, "squint", pal)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        for k in range(2):
            if (i + k) % 2 == 0:
                sparkle(d, PROP_X + 3 + k * 5, cy - 9 + k * 4, pal.accent, 1)
    elif state == "attention":
        eyes(d, face_cx, ey, "wide", pal)
        mouth(d, face_cx, my, "o", pal, mstyle)
        if i % 2 == 0:
            bang(d, PROP_X + 4, cy - 16, (236, 78, 78, 255))
        else:
            bang(d, PROP_X + 4, cy - 15, (236, 78, 78, 255))
    elif state == "confused":
        eyes(d, face_cx, ey, "confused", pal)
        mouth(d, face_cx, my, "wobble", pal, mstyle)
        question(d, PROP_X + 4, cy - 14 - (i % 2), pal.line)
    elif state == "worried":
        eyes(d, face_cx, ey, "worried", pal)
        mouth(d, face_cx, my, "wobble", pal, mstyle)
        sweat(d, PROP_X + 2, cy - 9 + (i % 2) * 2, (118, 190, 236, 255))
    elif state == "happy":
        eyes(d, face_cx, ey, "happy", pal)
        mouth(d, face_cx, my, "wide_smile", pal, mstyle)
        blush(d, s, CX, ey, pal)
    elif state == "celebrate":
        eyes(d, face_cx, ey, "sparkle", pal)
        mouth(d, face_cx, my, "wide_smile", pal, mstyle)
        for k, (sx, sy) in enumerate([(13, -14), (-13, -12), (16, -6), (-15, -3)]):
            if (i + k) % 3 != 2:
                sparkle(d, CX + sx, cy + sy, pal.accent, 1 + (i + k) % 2)
    elif state == "sleeping":
        eyes(d, face_cx, ey + 1, "closed", pal)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        zzz(d, PROP_X + 2, cy - 12, pal.line, i)
    elif state == "tired":
        eyes(d, face_cx, ey, "half", pal)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        coffee(d, PROP_X + 4, cy + 2, pal.line)
        steam(d, PROP_X + 6, cy, (210, 210, 210, 190), i)
    elif state == "heart":
        eyes(d, face_cx, ey, "happy", pal)
        mouth(d, face_cx, my, "smile", pal, mstyle)
        blush(d, s, CX, ey, pal)
        heart(d, PROP_X + 4, cy - 13 - (i % 2), (236, 84, 116, 255))
        if i % 2 == 0:
            heart(d, PROP_X - 1, cy - 7, (236, 132, 152, 210), big=False)

    return img
def build(s: Species):
    out_dir = os.path.join(OUT_ROOT, s.pid)
    os.makedirs(out_dir, exist_ok=True)

    animations = {}
    for state, (frames, fps) in STATES.items():
        strip = Image.new("RGBA", (W * frames, H), (0, 0, 0, 0))
        for i in range(frames):
            strip.paste(draw_pet(s, state, i, frames), (i * W, 0))
        path = os.path.join(out_dir, f"{state}.png")
        strip.save(path, optimize=True)
        animations[state] = {"file": f"{state}.png", "frames": frames, "fps": fps, "loop": True}

    manifest = {
        "id": s.pid,
        "name": s.name,
        "version": 1,
        "author": "built-in",
        "description": s.description,
        "frame_width": W,
        "frame_height": H,
        "scale": 3,
        "pixelated": True,
        "animations": animations,
    }
    with open(os.path.join(out_dir, "manifest.json"), "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")
    return out_dir


def contact_sheet(path="/tmp/contact_sheet.png", scale=5):
    """A review grid: one row per pet-state, one column per frame."""
    rows = [(s, st) for s in SPECIES for st in STATES]
    max_frames = max(f for f, _ in STATES.values())
    sheet = Image.new("RGBA", (W * max_frames * scale, H * len(rows) * scale), (24, 26, 32, 255))
    d = ImageDraw.Draw(sheet)
    for r, (s, st) in enumerate(rows):
        frames, _ = STATES[st]
        for i in range(frames):
            f = draw_pet(s, st, i, frames).resize((W * scale, H * scale), Image.NEAREST)
            sheet.alpha_composite(f, (i * W * scale, r * H * scale))
        d.text((4, r * H * scale + 4), f"{s.pid} {st}", fill=(255, 255, 255, 200))
    sheet.save(path)
    return path


if __name__ == "__main__":
    for sp in SPECIES:
        print("wrote", build(sp))
    print("contact sheet:", contact_sheet())
