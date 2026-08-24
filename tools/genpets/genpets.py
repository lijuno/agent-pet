#!/usr/bin/env python3
"""Generate the built-in pixel-art pet packs.

Every built-in pet is drawn from the same parametric creature, so the ten
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
    "heart":     (4, 5),
}


@dataclass
class Palette:
    body: tuple
    body_dark: tuple
    belly: tuple
    line: tuple
    accent: tuple
    # `eye` is the dark of the eye: the pupil, and every lid, lash and brow the
    # expression grammar draws. `iris` is the colour around that pupil, and
    # only an open round eye has one.
    eye: tuple = (40, 32, 42, 255)
    iris: tuple = None
    # The lit foot of a lashed pupil. Separate from `iris` because `iris` also
    # fills the sclera of the `wide` and `confused` eyes, where a warm brown
    # reads as a bruise rather than as an eye.
    eye_light: tuple = None
    blush: tuple = (242, 152, 152, 150)
    # Only a patched coat needs these; the robot and the slime leave them unset.
    patch: tuple = None
    white: tuple = (252, 250, 246, 255)
    nose: tuple = None
    # A character with hair and clothes rather than a coat needs these; the
    # cat and the robot leave them unset.
    hair: tuple = None
    hair_light: tuple = None
    cloth: tuple = None
    lip: tuple = None
    ribbon: tuple = None
    ribbon_dark: tuple = None
    gold: tuple = None
    gold_dark: tuple = None
    # think is the colour of the thinking dots. They used to be drawn in
    # `line`, the character's own outline, which made the one state that says
    # "the agent is reasoning" the hardest of all to notice. Props that carry a
    # state need to contrast with the animal, not match it.
    think: tuple = None


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
    hair: str = "none"      # none | long | side
    lashes: bool = False
    accessory: str = "none"  # none | bow
    extras: dict = field(default_factory=dict)


MOMO = Species(
    # The id stays `momo`: it is what config.yaml files in the wild already
    # name, and a display name is not worth breaking them over.
    pid="momo",
    name="SanMao (三毛)",
    description="A tortoiseshell tabby with a white bib and white paws.",
    palette=Palette(
        # A ginger base with dark tortie patches over it, white underneath.
        body=(224, 158, 99, 255),
        body_dark=(188, 122, 68, 255),
        belly=(252, 250, 246, 255),
        line=(52, 38, 30, 255),
        accent=(232, 158, 156, 255),   # the pink inside an ear
        eye=(48, 38, 32, 255),         # pupil, lids and lashes
        iris=(178, 166, 96, 255),      # hazel: gold in one light, green in another
        patch=(86, 62, 48, 255),
        white=(252, 250, 246, 255),
        nose=(214, 146, 138, 255),
        think=(120, 196, 255, 255),    # cool blue, opposite her ginger
    ),
    ears="cat", face="muzzle", tail=True, body_w=24, body_h=22,
    markings="tortie",
)

PEACH = Species(
    pid="peach",
    name="Peach (桃桃)",
    description="A girl with long dark hair swept over one shoulder, a peach bow and a gold necklace.",
    palette=Palette(
        # Skin, hair and eyes are sampled from the reference photo and then
        # pushed apart: at 40px, tones a camera can tell apart collapse into
        # one flat shape, so the shadow is darker and the hair blacker than
        # they photograph.
        body=(244, 210, 186, 255),
        body_dark=(214, 170, 146, 255),
        belly=(250, 226, 208, 255),
        line=(92, 60, 52, 255),        # warm outline; a black one reads as ink
        accent=(160, 198, 228, 255),   # the pale blue of her top, for sparkles
        eye=(46, 32, 30, 255),
        eye_light=(128, 86, 64, 255),  # a warm rim at the foot of the pupil
        blush=(240, 148, 148, 150),
        hair=(38, 30, 34, 255),
        hair_light=(88, 68, 72, 255),
        cloth=(250, 250, 250, 255),
        lip=(206, 116, 116, 255),
        ribbon=(255, 172, 142, 255),   # peach, for the girl called Peach
        ribbon_dark=(226, 124, 104, 255),
        gold=(244, 202, 108, 255),
        gold_dark=(198, 152, 66, 255),
        nose=None,
        think=(255, 194, 108, 255),    # warm, against all that dark hair
    ),
    ears="none", face="human", tail=False, body_w=24, body_h=22,
    hair="side", lashes=True, accessory="bow",
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
        think=(150, 236, 244, 255),
    ),
    ears="antenna", face="screen", tail=False, body_w=23, body_h=21,
)


# BYTE stays defined but unshipped, as the worked example of a third species —
# antenna instead of ears, a lit screen instead of a face — and the reason the
# drawing code is parametric at all. Adding it to this list is the whole of
# shipping it.
SPECIES = [MOMO, PEACH]


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


def dots(d, x, y, fill, edge, phase):
    """Thinking dots, filling in one at a time then clearing.

    Each dot is a bright square inside a dark one. The fill separates it from
    the character; the edge separates it from whatever wallpaper is behind the
    window, which may be any colour at all and is the reason a single flat
    colour cannot work here.
    """
    shown = phase % 4
    for i in range(3):
        if i < shown:
            x0, y0 = x + i * 4, y - i
            rect(d, x0 - 1, y0 - 1, x0 + 3, y0 + 3, edge)
            rect(d, x0, y0, x0 + 2, y0 + 2, fill)


# --------------------------------------------------------------------------
# expression grammar
#
# Eyes carry the state; the mouth reinforces it. Both are drawn relative to a
# face centre so every species animates identically.
# --------------------------------------------------------------------------

def eyes(d, ex, ey, kind, pal, look=(0, 0), lashes=False):
    # A plain eye is three pixels wide, so the right one starts at +4 to sit the
    # same distance from centre as the left at -6. At +3 both eyes were shifted
    # one pixel left of the face, which is small enough to look like nothing in
    # particular and wrong enough to notice. A lashed eye is four wide and sets
    # its own origin, one pixel further out on each side.
    lx, rx = ex - 6, ex + 4
    ox, oy = look
    c = pal.eye
    white = (255, 255, 255, 255)

    if kind == "dot" and lashes:
        # A tall oval with two catchlights, lined up with the arcs the other
        # kinds draw. Eye size is most of what separates "a girl" from "a cute
        # girl" at 40px — the three-pixel square this replaced read as a face
        # with its eyes screwed shut.
        for sx, x0 in ((-1, ex - 7), (1, ex + 4)):
            d.line([(x0 - 1, ey - 1), (x0 + 4, ey - 1)], fill=c)
            px(d, x0 + (4 if sx > 0 else -1), ey - 2, c)   # outer lash tick
            d.ellipse([x0 + ox, ey + oy, x0 + 3 + ox, ey + 4 + oy], fill=c)
            if pal.eye_light:
                rect(d, x0 + 1 + ox, ey + 3 + oy, x0 + 2 + ox, ey + 3 + oy, pal.eye_light)
            # The catchlight sits centred, not against the outer edge. Pushed
            # out there it reads as both eyes glancing the same way, in every
            # state, which is a squint she never recovers from.
            rect(d, x0 + 1 + ox, ey + 1 + oy, x0 + 2 + ox, ey + 1 + oy, white)
            px(d, x0 + 1 + ox, ey + 2 + oy, (255, 255, 255, 190))
    elif kind == "dot":
        for x in (lx, rx):
            if pal.iris:
                # A cat's eye is an iris with a slit pupil, not a solid disc,
                # and a dark rim above it — the eyeliner every tabby wears.
                #
                # No catchlight: the eye is three pixels wide, so a highlight
                # costs a whole column of iris and the eye reads
                # white-pupil-iris, lopsided. Symmetry matters more than shine
                # at this size.
                d.line([(x - 1, ey - 1), (x + 3, ey - 1)], fill=pal.line)
                rect(d, x + ox, ey + oy, x + 2 + ox, ey + 2 + oy, pal.iris)
                rect(d, x + 1 + ox, ey + oy, x + 1 + ox, ey + 2 + oy, c)
            else:
                rect(d, x + ox, ey + oy, x + 2 + ox, ey + 2 + oy, c)
                px(d, x + ox, ey + oy, (255, 255, 255, 170))
    elif kind == "wide":
        if lashes:
            for x in (lx, rx):
                d.line([(x - 1, ey - 3), (x + 3, ey - 3)], fill=c)
        for x in (lx - 1, rx - 1):
            # Startled: the pupil dilates and swallows the iris.
            d.ellipse([x, ey - 2, x + 4, ey + 3], fill=pal.iris or white, outline=pal.line)
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
    elif kind == "squint" and lashes:
        # A lowered lid with the eye still under it. `working` is the state she
        # is in most of the time, and the plain squint's two flat bars read as
        # a face switched off rather than a face concentrating.
        for sx, x0 in ((-1, ex - 7), (1, ex + 4)):
            d.line([(x0 - 1, ey - 1), (x0 + 4, ey - 1)], fill=c)
            px(d, x0 + (4 if sx > 0 else -1), ey - 2, c)
            # The same eye, narrowed and dropped a row, rather than a lid drawn
            # on top of it: a bar joined to a pupil reads as a heavy brow, and
            # she spends her working hours looking cross.
            d.ellipse([x0, ey + 1, x0 + 3, ey + 3], fill=c)
            px(d, x0 + 1, ey + 1, (255, 255, 255, 210))
    elif kind == "squint":
        # A brow with a pupil under it. A bare bar would be indistinguishable
        # from the closed eyes of `sleeping` at this size.
        for x in (lx - 1, rx - 1):
            d.line([(x, ey - 2), (x + 4, ey - 2)], fill=pal.line)
            rect(d, x + 1, ey, x + 3, ey + 1, c)
    elif kind == "confused":
        d.ellipse([lx - 1, ey - 2, lx + 3, ey + 3], fill=pal.iris or white, outline=pal.line)
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
            if lashes:
                rect(d, x - 1, ey + 1, x + 1, ey + 4, c)
            else:
                rect(d, x, ey + 1, x + 1, ey + 3, c)
    elif kind == "sparkle":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey + 2), (x + 2, ey - 1)], fill=c)
            d.line([(x + 2, ey - 1), (x + 4, ey + 2)], fill=c)
        px(d, lx + 4, ey - 3, white)
        px(d, rx - 2, ey - 3, white)


def mouth(d, mx, my, kind, pal, muzzle_style="cat"):
    # Lips, on a character that has them: a mouth drawn in the outline colour
    # is a scar on skin. The cat and the robot leave `lip` unset.
    c = pal.lip or pal.line
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

def blush(d, s, cx, ey, pal, soft=False):
    """Cheeks. Skipped for screen faces: a robot has no cheeks, and the marks
    would land outside the panel and read as damage.

    A lashed eye is five rows tall against the plain eye's three, so the cheeks
    drop by two — drawn at the old height they sit on the eye itself and read
    as bloodshot rather than as a blush.

    `soft` is the version she wears in every state. A permanent faint blush is
    most of what makes a character read as cute rather than merely drawn; the
    full-strength one still goes on top in `happy` and `heart`.
    """
    if s.face == "screen":
        return
    c = pal.blush
    if soft:
        c = c[:3] + (max(1, c[3] // 3),)
    dy = 5 if s.lashes else 3
    d.ellipse([cx - 10, ey + dy, cx - 7, ey + dy + 2], fill=c)
    d.ellipse([cx + 7, ey + dy, cx + 10, ey + dy + 2], fill=c)


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


def bow(d, x, y, pal):
    """One accessory that is not part of the body. At this size that, and the
    eyes, is what cuteness is made of — there is no room for anything subtler."""
    c, dark = pal.ribbon, pal.ribbon_dark or pal.ribbon
    d.polygon([(x - 4, y - 2), (x - 1, y), (x - 4, y + 2)], fill=c)
    d.polygon([(x + 4, y - 2), (x + 1, y), (x + 4, y + 2)], fill=c)
    px(d, x - 3, y, dark)
    px(d, x + 3, y, dark)
    rect(d, x - 1, y - 1, x + 1, y + 1, dark)


def hair_back(d, s, top, bw, bh, cy, pal):
    """The mass of hair behind the head, and the only part of the character
    allowed outside the body silhouette.

    It is deliberately lopsided: everything falls forward over her right
    shoulder and runs to the waist, while the far side is tucked behind and
    stops at the jaw. A symmetrical curtain of the same hair reads as a helmet,
    which is exactly what the first version of this looked like.

    Drawn before the body ellipse, so the face lands on top of the half of
    these shapes that crosses it.
    """
    if s.hair != "side":
        return
    x0, x1 = CX - bw // 2, CX + bw // 2

    # The crown. It hugs the head on the side the hair is swept *away* from,
    # and stops at the head's own edge on the other: swept hair is tighter over
    # the ear it leaves, not puffier. That also keeps it out of the top-right
    # prop column, which is the difference between a Z that reads and a Z lost
    # in black hair — the props are drawn last but they are drawn dark.
    d.ellipse([x0 - 2, top - 5, x1, top + 11], fill=pal.hair)

    # The long side: out over the shoulder at the jaw, then straight down to a
    # tip below the body. It only swings wide *below* the props, which all sit
    # above y=18.
    d.polygon([(CX + 3, top + 1), (x1 + 2, top + 4), (x1 + 5, top + 11),
               (x1 + 5, top + 20), (CX + 10, top + 25), (CX + 6, top + 19),
               (CX + 4, top + 10)], fill=pal.hair)
    # The short side, tucked behind the shoulder.
    d.polygon([(CX - 3, top + 1), (x0 - 2, top + 4), (x0 - 2, top + 11),
               (CX - 9, top + 17), (CX - 5, top + 10)], fill=pal.hair)

    # Lit strands, following each fall. Straight black hair with no highlight
    # is a flat blob whatever shape it is cut into, and the two strands are
    # also what tells you the two sides are different lengths.
    light = pal.hair_light or pal.hair
    d.line([(x1 + 3, top + 8), (x1 + 3, top + 19)], fill=light)
    d.line([(CX + 8, top + 21), (CX + 9, top + 24)], fill=light)
    d.line([(x0 - 1, top + 6), (x0 - 1, top + 13)], fill=light)


def hairline(img, s, top, bw, bh, cy, pal):
    """Fringe and the locks that frame the face.

    Clipped to the head ellipse for the same reason the tortie markings are:
    the bob squashes and stretches the body every frame, and redoing that
    ellipse arithmetic per feature gets it wrong in one of them.

    The fringe stops four rows above the eyes on purpose. Drawn any lower it
    touches the lash line and the eyes stop reading as eyes — they become the
    bottom edge of the hair. `worried` needs the room too: its brows are drawn
    in the outline colour, which on hair this dark is no colour at all.
    """
    if s.hair != "side":
        return
    overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    o = ImageDraw.Draw(overlay)
    x0, x1 = CX - bw // 2, CX + bw // 2

    rect(o, x0 - 3, top - 4, x1 + 3, top + 3, pal.hair)
    # Swept across to the long side, so the parting sits off centre and agrees
    # with where the weight of the hair is.
    o.polygon([(x0 - 3, top + 3), (CX - 5, top + 3), (x0 - 3, top + 8)], fill=pal.hair)
    o.polygon([(x1 + 3, top + 3), (CX + 2, top + 3), (x1 + 3, top + 11)], fill=pal.hair)
    # A broken highlight across the crown. Two segments with a gap, not one
    # arc: a continuous line reads as a hairband, and the gap is what makes
    # hair this dark look glossy rather than matte.
    light = pal.hair_light or pal.hair
    o.line([(CX - 8, top + 2), (CX - 4, top + 1)], fill=light)
    o.line([(CX - 1, top), (CX + 3, top + 1)], fill=light)

    # Locks down the cheeks: two pixels on the tucked side, four on the side
    # the hair falls, because that is the asymmetry read from the front.
    rect(o, x0, top + 3, x0 + 1, top + bh - 8, pal.hair)
    rect(o, x1 - 3, top + 3, x1, top + bh - 6, pal.hair)

    mask = Image.new("L", (W, H), 0)
    ImageDraw.Draw(mask).ellipse([x0 + 1, top + 1, x1 - 1, top + bh - 1], fill=255)
    overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
    img.alpha_composite(overlay)


def necklace(d, s, top, bw, bh, cy, pal):
    """A fine gold chain in the open neck, with a pendant in the notch.

    Drawn after the top, and shaped to sit *inside* the neck opening rather
    than along its edge: a chain that follows the collar reads as piping on the
    garment instead of as jewellery on her. The two limbs pass a pixel outside
    the mouth at its widest, which is the whole vertical budget there is
    between a chin and a collarbone.
    """
    if not pal.gold:
        return
    hem = top + bh - 4
    g, gd = pal.gold, pal.gold_dark or pal.gold
    for k in range(4):
        px(d, CX - 4 + k, hem - 3 + k, g)
        px(d, CX + 4 - k, hem - 3 + k, g)
    # The pendant hangs in the open neck, just clear of the collar.
    px(d, CX, hem, gd)
    px(d, CX, hem + 1, g)


def outfit(img, s, top, bw, bh, cy, pal):
    """The white top, clipped to the body the same way.

    Without it the skin runs to the bottom of the ellipse and she reads as a
    floating head. But the body is a head and a torso merged into one shape, so
    every row the top takes is a row the face loses: at the cat's bib height it
    was a quarter of her visible pixels and looked like a shirt pulled up over
    her jaw. It sits four rows off the bottom now, not six.

    The shoulders are the high points and the neck the low one, which is what
    makes it a scoop neck. A collar drawn straight across is a stripe.
    """
    if not pal.cloth:
        return
    overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    o = ImageDraw.Draw(overlay)
    x0, x1 = CX - bw // 2, CX + bw // 2
    hem = top + bh - 4

    # Shoulders five rows above the notch. Cut straight across at the notch's
    # height — which is where it sat when the neckline was raised to make room
    # for the chain — the top becomes a band at the bottom of the frame and she
    # reads as bare to the collarbone. The garment needs somewhere to hang from.
    neck = [(x0 - 3, hem - 5), (CX - 7, hem - 3), (CX - 3, hem), (CX, hem + 2),
            (CX + 3, hem), (CX + 7, hem - 3), (x1 + 3, hem - 5),
            (x1 + 3, top + bh + 3), (x0 - 3, top + bh + 3)]
    # No trim. It edged the neckline first, a pixel from the chain, and the two
    # parallel lines read as piping with the gold lost inside it; moved to the
    # shoulders it became two dashes floating clear of a garment that is barely
    # there at that height. One accent at the neck is the whole idea, and the
    # accent is the necklace.
    o.polygon(neck, fill=pal.cloth)

    mask = Image.new("L", (W, H), 0)
    ImageDraw.Draw(mask).ellipse([x0 + 1, top + 1, x1 - 1, top + bh - 1], fill=255)
    overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
    img.alpha_composite(overlay)


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

    # ground shadow — shrinks as the pet rises, which is what sells the hop
    shadow_w = bw - 2 + bob
    d.ellipse([CX - shadow_w // 2, GROUND, CX + shadow_w // 2, GROUND + 3],
              fill=(0, 0, 0, 60))

    top = cy - bh // 2

    # --- hair, behind the body: it is the silhouette, so it goes down first
    hair_back(d, s, top, bw, bh, cy, pal)

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
    outfit(img, s, top, bw, bh, cy, pal)
    hairline(img, s, top, bw, bh, cy, pal)
    necklace(d, s, top, bw, bh, cy, pal)
    if s.accessory == "bow":
        # On the tucked side: the other one is under the fall of the hair, and
        # a bow you cannot see is three pixels of nothing.
        bow(d, CX - bw // 2 + 2, top + 4, pal)

    # White socks, on a cat that has them.
    paw = pal.white if s.markings == "tortie" else pal.body

    # --- limbs that must sit on top of the body
    if state == "celebrate":
        # A head that fills the top of the body has no room above it for an
        # arm: raised to the cat's height the hands sit level with her temples
        # and read as a second pair of ears. So they start at the shoulder.
        shoulder, reach = (cy + 5, cy - 1) if s.face == "human" else (cy + 1, cy - 6)
        for sx in (-1, 1):
            ax = CX + sx * (bw // 2 - 1)
            d.line([(ax, shoulder), (ax + sx * 4, reach)], fill=pal.body_dark, width=3)
            d.ellipse([ax + sx * 4 - 2, reach - 3, ax + sx * 4 + 2, reach + 1],
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

    if s.face == "human":
        # A face, not a muzzle: nothing is drawn between the eyes and the
        # mouth, and the mouth sits higher so it lands on the chin rather than
        # on the collar.
        my = cy + 2
    elif s.face == "screen":
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

    # The faint permanent blush, under whatever the state draws over it.
    if s.lashes and state != "sleeping":
        blush(d, s, CX, ey, pal, soft=True)

    # Whiskers sit under the expression, so a brow or a wide eye still wins.
    if s.markings == "tortie" and state != "sleeping":
        whiskers(d, face_cx, my, pal)

    if state == "idle":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal, lashes=s.lashes)
        mouth(d, face_cx, my, "cat", pal, mstyle)
    elif state == "thinking":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal, look=(-1, -1), lashes=s.lashes)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        dots(d, PROP_X + 1, cy - 10, pal.think or pal.accent, pal.line, i)
    elif state == "working":
        eyes(d, face_cx, ey, "squint", pal, lashes=s.lashes)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        for k in range(2):
            if (i + k) % 2 == 0:
                sparkle(d, PROP_X + 3 + k * 5, cy - 9 + k * 4, pal.accent, 1)
    elif state == "attention":
        eyes(d, face_cx, ey, "wide", pal, lashes=s.lashes)
        mouth(d, face_cx, my, "o", pal, mstyle)
        bang(d, PROP_X + 4, cy - 16 + i % 2, (236, 78, 78, 255))
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
        json.dump(manifest, f, indent=2, ensure_ascii=False)
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
