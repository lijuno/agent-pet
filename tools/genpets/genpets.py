#!/usr/bin/env python3
"""Generate the built-in pixel-art pet packs.

Every built-in pet is drawn from the same parametric creature, so the ten
states read consistently across characters: the same eye grammar, and the same
props in the same corner of the frame.

What a character may change has grown past the silhouette and the palette it
started at — the head is an ellipse or a rounded rectangle, the face may wear
brows and glasses, and `working` is a desk for one of them and a bicycle for
another, which also suspends the bob. Anything a species does not set falls
back to what the cat does.

Output: ui/dist/pets/<id>/{manifest.json, <state>.png}
Each PNG is a horizontal strip of N frames, 40x40 each, with alpha.

Run:  python3 tools/genpets/genpets.py
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass

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
    # The edge of the garment. `line` is the character's own outline and is
    # warm and dark; drawn round a white dress it reads as piping in leather.
    cloth_line: tuple = None
    # The underside of the jaw where it meets the body. A shadow, not an edge.
    jaw: tuple = None
    lip: tuple = None
    ribbon: tuple = None
    ribbon_dark: tuple = None
    desk: tuple = None
    desk_edge: tuple = None
    gold: tuple = None
    gold_light: tuple = None
    gold_dark: tuple = None
    # The laptop's shell. It used to borrow `cloth_line`, which is the edge of
    # the garment, and that held only while the one character with a laptop
    # wore something cool and grey. A charcoal shirt takes a charcoal edge, and
    # a charcoal laptop against a charcoal chest is one shape, not two.
    shell: tuple = None
    # Trousers, and the shoes under them. A character with bare legs leaves
    # both unset and the legs come out skin-coloured, which is what Peach wants.
    pants: tuple = None
    shoe: tuple = None
    # The metal of a pair of glasses. A cool grey, as dark as the outline of
    # the face it crosses and cooler than it: a pale frame at this size is not
    # wire but the rim of a pair of goggles, which is what he wore first.
    frame: tuple = None
    # The paint on a bicycle frame. The tyres are not here: rubber is black on
    # every bicycle ever made, so they take a literal, the way the lit panel of
    # a screen does.
    bike: tuple = None
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
    # The outline of the head itself. Every character had one ellipse for it
    # until a square-jawed one turned up, and a jaw is most of what a face is
    # recognised by before any of the features are legible.
    head: str = "round"     # round | square
    tail: bool = True
    body_w: int = 24
    body_h: int = 22
    markings: str = "none"  # none | tortie
    hair: str = "none"      # none | side | curly | crop
    lashes: bool = False
    accessory: str = "none"  # none | bow
    # Eyebrows the character wears in every state, drawn above whatever else
    # the face is doing. Peach has none: her expression is carried by eyes
    # three rows tall, and brows over them leave no forehead. A face with small
    # eyes behind glasses has the opposite problem and needs them.
    brows: bool = False
    glasses: str = "none"    # none | rect | round
    # The eye a child has and an adult does not: the same oval the lashed eye
    # is built on, without the lash line over it. Two switches rather than one,
    # because a boy of seven has the size and not the lashes.
    big_eyes: bool = False
    # The faint blush worn in every state. Separate from `lashes`, which used
    # to stand in for it: they travel together on one character and that is not
    # a rule.
    soft_blush: bool = False
    # A separate body under the head, rather than one ellipse that is both.
    torso: str = "none"      # none | chibi
    garment: str = "dress"   # dress | tee
    # What the character is doing in `working`. The state means the agent is
    # busy; what busy looks like is the character's own.
    work: str = "desk"       # desk | bike | write
    # Where the head sits, and where the face sits inside it. A merged
    # head-and-body puts the eyes near the middle of the one ellipse and lets
    # the chin run into the chest; once the head is only a head, the face has
    # to move down inside it or she is all forehead and jaw.
    head_dy: int = 0
    eye_dy: int = 0
    mouth_dy: int = 0


SANMAO = Species(
    # The id was `momo` until long after she shipped, on the grounds that
    # config.yaml files in the wild already named it and a display name was not
    # worth breaking them over. Two more characters later, an id nobody can
    # match to a name is worth more than that: it sorted the Change Pet menu
    # into an order the reader could not verify, and `petctl pet momo` is a
    # thing you have to be told. Config.Sanitised carries the old value across.
    pid="sanmao",
    name="Sanmao (三毛)",
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
    description="A girl in a white dress, with long dark hair, a peach bow and a gold necklace.",
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
        cloth_line=(196, 200, 212, 255),   # a cool grey; her outline is warm
        jaw=(228, 186, 164, 255),          # one step under the skin, no more
        lip=(206, 116, 116, 255),
        ribbon=(255, 172, 142, 255),   # peach, for the girl called Peach
        ribbon_dark=(226, 124, 104, 255),
        desk=(198, 166, 132, 255),        # warm wood, against the cool laptop
        desk_edge=(232, 204, 172, 255),
        gold=(222, 168, 58, 255),
        gold_light=(255, 228, 140, 255),
        gold_dark=(160, 114, 36, 255),
        nose=None,
        think=(255, 194, 108, 255),    # warm, against all that dark hair
    ),
    ears="none", face="human", tail=False, body_w=21, body_h=19,
    hair="side", lashes=True, accessory="bow", soft_blush=True,
    torso="chibi", garment="dress", head_dy=-6, eye_dy=3, mouth_dy=3,
)


JUANMAO = Species(
    pid="juanmao",
    name="Juanmao (卷毛)",
    description="A man with curly hair and rectangular glasses, who works from a bicycle.",
    palette=Palette(
        # Warmer and a shade deeper than Peach's skin, and pushed apart from
        # her the same way she was pushed apart from her photograph: two
        # characters at the same desk have to be told apart at 40px, and at
        # 40px only value separates them.
        body=(236, 196, 166, 255),
        body_dark=(202, 158, 130, 255),
        belly=(244, 214, 190, 255),
        line=(84, 56, 46, 255),
        accent=(255, 178, 96, 255),    # amber, for the sparkles of `celebrate`
        eye=(46, 34, 30, 255),
        blush=(236, 150, 130, 120),
        hair=(40, 32, 28, 255),
        hair_light=(112, 88, 72, 255),  # the lit edge of a curl
        cloth=(56, 60, 70, 255),       # a charcoal t-shirt
        cloth_line=(38, 42, 50, 255),
        pants=(112, 128, 156, 255),
        shoe=(42, 46, 56, 255),
        frame=(74, 68, 74, 255),
        jaw=(216, 176, 148, 255),
        lip=(152, 98, 86, 255),
        # The same wood Peach sits at. They are two people in one room, not two
        # drawings that happen to ship together.
        desk=(198, 166, 132, 255),
        desk_edge=(232, 204, 172, 255),
        shell=(186, 192, 204, 255),   # handlebars, and the hub of a wheel
        bike=(228, 108, 76, 255),     # the frame, in a red he would pick
        nose=None,
        think=(140, 202, 255, 255),    # cool, against an amber accent
    ),
    ears="none", face="human", head="square", tail=False, body_w=21, body_h=19,
    hair="curly", lashes=False, brows=True, glasses="rect",
    torso="chibi", garment="tee", work="bike",
    head_dy=-6, eye_dy=4, mouth_dy=4,
)


MAOMAO = Species(
    pid="maomao",
    name="Maomao (毛毛)",
    description="A seven-year-old boy in clear round glasses, writing at his desk.",
    palette=Palette(
        # Lighter and pinker than either adult. Children are drawn lighter than
        # they photograph for the same reason everything else here is pushed
        # apart: three human characters at 40px are told apart by value first
        # and by everything else second.
        body=(248, 216, 190, 255),
        body_dark=(214, 176, 150, 255),
        belly=(252, 230, 210, 255),
        line=(88, 60, 50, 255),
        accent=(122, 196, 255, 255),   # sky blue, for the sparkles of `working`
        eye=(44, 34, 30, 255),
        eye_light=(120, 84, 66, 255),  # a warm rim at the foot of the pupil
        blush=(244, 158, 150, 160),
        hair=(46, 36, 30, 255),
        hair_light=(104, 82, 66, 255),
        cloth=(76, 122, 186, 255),     # a blue t-shirt
        cloth_line=(54, 92, 148, 255),
        shoe=(72, 96, 152, 255),       # blue trainers, over bare legs
        # Clear plastic. Pale on purpose, which every other frame in this file
        # is not: on Juanmao a pale rim read as goggles, and the difference is
        # that these are round and enormous, which nothing but a pair of
        # children's glasses ever is.
        frame=(206, 218, 234, 255),
        jaw=(228, 194, 170, 255),
        lip=(196, 118, 108, 255),
        # The same wood Peach and Juanmao's world is furnished with.
        desk=(198, 166, 132, 255),
        desk_edge=(232, 204, 172, 255),
        shell=(206, 212, 222, 255),    # the edge of a sheet of paper
        nose=None,
        think=(255, 190, 110, 255),    # warm, against all that blue
    ),
    ears="none", face="human", head="round", tail=False,
    body_w=21, body_h=20,
    hair="crop", lashes=False, big_eyes=True, glasses="round",
    soft_blush=True,
    torso="chibi", garment="tee", work="write",
    # Sat five rows lower than the adults and given a rounder head: a short
    # body under a big round head is the whole of what says child here, and it
    # says it before any feature on the face is legible.
    head_dy=-5, eye_dy=2, mouth_dy=4,
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
SPECIES = [SANMAO, PEACH, JUANMAO, MAOMAO]


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

def eyes(d, ex, ey, kind, pal, look=(0, 0), lashes=False, own_brows=False, big=False):
    # A plain eye is three pixels wide, so the right one starts at +4 to sit the
    # same distance from centre as the left at -6. At +3 both eyes were shifted
    # one pixel left of the face, which is small enough to look like nothing in
    # particular and wrong enough to notice. A lashed eye is four wide and sets
    # its own origin, one pixel further out on each side.
    lx, rx = ex - 6, ex + 4
    ox, oy = look
    c = pal.eye
    white = (255, 255, 255, 255)

    if kind == "dot" and (lashes or big):
        # A tall oval with two catchlights, lined up with the arcs the other
        # kinds draw. Eye size is most of what separates "a girl" from "a cute
        # girl" at 40px — the three-pixel square this replaced read as a face
        # with its eyes screwed shut. It is also most of what separates a child
        # from an adult, which is why the oval and the lash line above it are
        # two switches rather than one: a boy of seven has the eyes and not
        # the lashes.
        for sx, x0 in ((-1, ex - 7), (1, ex + 4)):
            if lashes:
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
    elif kind == "squint" and (lashes or big):
        # A lowered lid with the eye still under it. `working` is the state she
        # is in most of the time, and the plain squint's two flat bars read as
        # a face switched off rather than a face concentrating.
        for sx, x0 in ((-1, ex - 7), (1, ex + 4)):
            if lashes:
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
            if not own_brows:
                d.line([(x, ey - 2), (x + 4, ey - 2)], fill=pal.line)
            rect(d, x + 1, ey, x + 3, ey + 1, c)
    elif kind == "confused":
        d.ellipse([lx - 1, ey - 2, lx + 3, ey + 3], fill=pal.iris or white, outline=pal.line)
        rect(d, lx, ey, lx + 1, ey + 1, c)
        d.line([(rx - 1, ey + 1), (rx + 3, ey + 1)], fill=c)
    elif kind == "worried":
        # Brows angled sharply inward with a gap between them, over anxious
        # pupils. The gap is what makes it read as worry rather than a scowl.
        #
        # Not on a face wearing glasses: these sit two rows above the pupil,
        # which is inside the lens, and a brow drawn behind glass is a smear on
        # the glass. That face angles its own brows, above the frame.
        if not own_brows:
            d.line([(lx - 1, ey - 4), (lx + 2, ey - 1)], fill=pal.line)
            d.line([(lx - 1, ey - 3), (lx + 2, ey)], fill=pal.line)
            d.line([(rx + 4, ey - 4), (rx + 1, ey - 1)], fill=pal.line)
            d.line([(rx + 4, ey - 3), (rx + 1, ey)], fill=pal.line)
        for x in (lx, rx + 1):
            if lashes or big:
                rect(d, x - 1, ey + 1, x + 1, ey + 4, c)
            else:
                rect(d, x, ey + 1, x + 1, ey + 3, c)
    elif kind == "sparkle":
        for x in (lx - 1, rx - 1):
            d.line([(x, ey + 2), (x + 2, ey - 1)], fill=c)
            d.line([(x + 2, ey - 1), (x + 4, ey + 2)], fill=c)
        px(d, lx + 4, ey - 3, white)
        px(d, rx - 2, ey - 3, white)


# How the brows sit in each state, for a character that wears them. This is
# the second channel the expression has, and on a face whose eyes are small and
# behind glass it carries more than the eyes do.
BROWS = {
    "idle":      "flat",
    "thinking":  "arch",      # one thought, lifted
    "working":   "low",       # both ends dropped: concentration
    "attention": "raised",
    "confused":  "arch",
    "worried":   "worried",   # inner ends up — the only shape that reads as worry
    "happy":     "raised",
    "celebrate": "raised",
    "sleeping":  "flat",
    "heart":     "raised",
}


def eyebrows(d, ex, by, kind, pal):
    """Two thick brows above the frame.

    Two rows, not one: a single row of hair over a lens reads as a second rim,
    and the pair then reads as bifocals. They are drawn in the hair colour
    rather than in `line`, which is the colour of his own outline and would put
    the brows in the same ink as the edge of his face.

    Each brow runs from an inner end to an outer one and the two ends move
    independently — that difference is the expression. `worried` lifts the
    inner ends and drops the outer, which is the shape a face makes when
    something has gone wrong and no other arrangement of two bars is.
    """
    c = pal.hair or pal.line
    inner_dy, outer_dy = {
        "flat":    (0, 0),
        "arch":    (-1, 0),
        "raised":  (-1, -1),
        # Lowered flat, not angled down toward the nose. Angled, five pixels
        # of brow at this size is not a man concentrating but a man furious,
        # and `working` is the state he is in most of the day.
        "low":     (1, 1),
        "worried": (-1, 1),   # inner up, outer down
    }[kind]
    for sx in (-1, 1):
        ix, ox_ = ex + sx * 4, ex + sx * 8
        for row in (0, 1):
            d.line([(ix, by + inner_dy + row), (ox_, by + outer_dy + row)], fill=c)


def spectacles(d, ex, ey, pal, kind="rect"):
    """Rectangular metal frames, over the eyes.

    Drawn after the expression, so the rim passes in front of a startled pupil
    instead of being punched through by it — which is what glass does, and the
    alternative is an eye that grows through its own lens in `attention`.

    The four corner pixels are left out. A closed rectangle at seven pixels
    across is a pair of goggles, and dropping the corners is most of the
    difference between the two. The far top corner is also the one pixel of him
    the prop column reaches, so the dot of `thinking` now lands beside the lens
    rather than on the corner of it.
    """
    c = pal.frame or pal.line
    if kind == "round":
        # Two circles nearly meeting at the bridge, each centred on the eye
        # inside it. Round and far too big for the face is what a pair of
        # children's glasses is; drawn to the same seven-pixel box the adult's
        # frames use, he would just be a small man.
        for sx in (-1, 1):
            cx_ = ex + sx * 5
            d.ellipse([cx_ - 4, ey - 2, cx_ + 4, ey + 6], outline=c)
        d.line([(ex - 1, ey + 2), (ex + 1, ey + 2)], fill=c)
        for sx in (-1, 1):
            d.line([(ex + sx * 9, ey + 2), (ex + sx * 10, ey + 2)], fill=c)
        return
    for x0 in (ex - 8, ex + 2):
        x1, y0, y1 = x0 + 6, ey - 2, ey + 4
        d.line([(x0 + 1, y0), (x1 - 1, y0)], fill=c)
        d.line([(x0 + 1, y1), (x1 - 1, y1)], fill=c)
        d.line([(x0, y0 + 1), (x0, y1 - 1)], fill=c)
        d.line([(x1, y0 + 1), (x1, y1 - 1)], fill=c)
    # The bridge, high on the nose. Level with the middle of the lens it reads
    # as a nosepiece resting on a mouth.
    d.line([(ex - 1, ey - 1), (ex + 1, ey - 1)], fill=c)
    # Temples, two pixels each, running off into the edge of the face. Longer,
    # they reach the hair and stop being wire.
    for sx in (-1, 1):
        d.line([(ex + sx * 9, ey), (ex + sx * 10, ey)], fill=c)


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

# The radius of the corner on a square head. Five of twenty-one across: less
# and he is a brick, more and he is the ellipse everyone else has.
JAW_R = 5


def head_shape(d, s, x0, y0, x1, y1, **kw):
    """The outline of the head, and the mask everything drawn on it is clipped
    to. Both go through here so they cannot disagree — they did, once, and the
    fringe was clipped to an ellipse the face no longer was."""
    if s.head == "square":
        d.rounded_rectangle([x0, y0, x1, y1], radius=JAW_R, **kw)
    else:
        d.ellipse([x0, y0, x1, y1], **kw)


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
    # A lashed eye is five rows tall against the plain eye's three, and the
    # bottom rim of a pair of glasses lands on the cheek at exactly the height
    # the plain blush wants. Both push it down by the same two rows.
    dy = 5 if (s.lashes or s.glasses != "none") else 3
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
    head_shape(ImageDraw.Draw(mask), s, x0 + 1, top + 1, x1 - 1, top + bh - 1, fill=255)
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


def desk(d, pal):
    """The surface the laptop stands on, in `working` and nowhere else.

    Two rows, because that is what is left: her head takes rows 8 to 27 and
    everything below it fits in the eight that remain. It stops short of both
    frame edges on purpose — run all the way across, it reads as a floor she is
    standing behind rather than a desk she is sitting at.

    Only `working` gets one. It was tried in `thinking` too, to give a propped
    elbow something to rest on, and that pose never read at this size; the desk
    was holding up a gesture that did not work rather than earning its own
    place. Here it earns it: a laptop resting on nothing was the thing that
    looked wrong.
    """
    if not pal.desk:
        return
    rect(d, 4, GROUND, W - 5, GROUND + 1, pal.desk)
    d.line([(4, GROUND), (W - 5, GROUND)], fill=pal.desk_edge or pal.desk)


# The bicycle is anchored to the ground, not to the rider. He bobs; it does
# not, any more than the laptop does — a bicycle that breathes on the beat is
# the one thing in the frame that cannot.
# It is drawn low, past GROUND and into the rows the ground shadow would have
# used: the wheels are what touches the floor here, and every row they can be
# given down there is a row the curls on his head do not lose at the top.
HUB_Y = 32          # the hub of the one wheel there is
BAR_Y = 26          # the handlebars, and therefore his hands


def bicycle(d, i, pal):
    """The bike head on, coming toward you.

    It was drawn from the side first, two circles and a diamond, which is the
    most recognisable bicycle there is — and wrong, because the rider on it has
    a face and two hands drawn from the front. Two wheels side by side put the
    viewer at the kerb; symmetrical hands and a symmetrical face put the viewer
    in front of the bike. One of the two had to go, and it could not be the
    face: the other nine states are that face.

    So the wheel is edge on and there is only one of it, the fork straddles it
    where a front view puts both blades, and the bars run across with a grip at
    each end for the two hands that were already there.

    Drawn after the rider. Head on, the front wheel and the bars are the
    nearest things in the frame — his knees pass behind them, which is also
    what stops the wheel being a shape floating between his legs.
    """
    # Rubber is black on every bicycle ever made, so the tyre is a literal
    # rather than a palette entry, the way the lit panel of a screen is.
    tyre = (46, 44, 48, 255)
    tread = (146, 144, 154, 255)
    rim = (168, 172, 184, 255)
    paint = pal.bike or pal.body_dark
    metal = pal.shell or pal.body_dark
    top = HUB_Y - 5

    # The fork: two blades either side of the wheel, and the crown across them.
    # This is most of what says the bicycle is pointing at you rather than
    # standing beside you — from the kerb the two blades are one line.
    for sx in (-1, 1):
        d.line([(CX + sx * 3, top + 1), (CX + sx * 3, HUB_Y + 2)], fill=paint)
    d.line([(CX - 3, top + 1), (CX + 3, top + 1)], fill=paint)
    # The stem, up the middle to the bar. Without it the handlebars hang in
    # front of his chest with nothing holding them.
    d.line([(CX, BAR_Y + 1), (CX, top + 1)], fill=metal)

    # The wheel: five across and twelve down. A bicycle tyre seen head on is a
    # slot, and at seven across this was a motorbike. A dark tyre with the lit
    # edge of the rim down the middle of it — one value cannot be seen against
    # both a light desktop and a dark one, so it carries two, the way the
    # thinking dots do.
    d.ellipse([CX - 2, top, CX + 2, HUB_Y + 6], fill=tyre)
    d.line([(CX, top + 2), (CX, HUB_Y + 4)], fill=rim)
    px(d, CX, HUB_Y, metal)

    # Tread, and the tread is what turns.
    #
    # The spokes cannot do it: they are edge on, and there is no rim feature to
    # follow either. But the tyre has a pattern on it, the pattern is regular,
    # and the surface facing the viewer travels downward as the bike comes at
    # you — so blocks marching down the tyre a row a frame are a wheel rolling,
    # from exactly the angle where nothing else can say so.
    #
    # The period is four rows and there are four frames. Any other pairing and
    # the pattern lands where it started before the strip does, and the wheel
    # stutters once a cycle for ever.
    # Eight rows of tyre, four frames, one block every four rows: each frame
    # shows exactly two blocks. Over any other span the count flickers between
    # two and three and the tread pulses instead of rolling.
    #
    # The top row of the eight is where the tyre narrows to three pixels, so
    # the block narrows with it. Drawn at the full width up there it lands on
    # nothing and hangs off the side of the wheel.
    for y in range(HUB_Y - 3, HUB_Y + 5):
        if (y - i) % 4:
            continue
        for dx in ((-2, -1, 1, 2) if y > HUB_Y - 3 else (-1, 1)):
            px(d, CX + dx, y, tread)


def feet(i):
    """How high each foot is this frame.

    Head on, a crank is a circle seen edge on: the near half of it points at
    the viewer and foreshortens to nothing, so a foot going round travels up
    and down and hardly at all across. Two pixels of rise is the whole of the
    animation, and with the wheel unable to show that it is turning it is also
    the whole of the motion in the state.
    """
    rise = (2, 0, -2, 0)
    return HUB_Y + 1 + rise[i % 4], HUB_Y + 1 + rise[(i + 2) % 4]


def writing(d, i, pal):
    """A sheet of paper lying on the desk, and the writing appearing on it.

    What `working` means for a seven-year-old. The laptop belongs to the two
    adults and the bicycle to one of them; the state says the agent is busy,
    and what busy looks like is the character's own.

    It was a sketchpad stood upright first, which was a mistake of exactly the
    kind this file keeps making: an upright rectangle with a border, lit
    lighter than its surroundings, sitting on a desk in front of somebody is a
    screen. It does not matter what is drawn on it — the silhouette had already
    said laptop before the scribble said crayon.

    So it lies flat, and it is a trapezoid rather than a rectangle: wider along
    the near edge than the far one, which is what a sheet on a table looks like
    from where the viewer is standing and what no screen ever looks like.
    """
    edge = pal.shell or pal.body_dark
    far, near = GROUND - 4, GROUND - 1
    # Wider than he is. A sheet no broader than his shoulders is hidden behind
    # them and his hands: all that showed was a white band across his middle,
    # which reads as an apron. Running out past him on both sides is what makes
    # it a surface he is leaning over rather than a thing he is wearing.
    d.polygon([(CX - 8, far), (CX + 8, far), (CX + 11, near), (CX - 11, near)],
              fill=pal.white, outline=edge)
    # Marks between his hands, one more each frame. Not lines of text: his
    # hands take everything either side of centre and what is left is five
    # pixels across, which is room for a word and not a sentence.
    for k in range(i % 4 + 1):
        y = far + 1 + k % 2
        x = CX - 2 + (k // 2) * 3
        d.line([(x, y), (x + 1, y)], fill=pal.line)


def pencil(d, i, pal):
    """The pencil, held over the page.

    Drawn last and not in a hand: at this size a hand holding a stick is four
    pixels of skin with one coloured pixel inside it, and the stick is the half
    worth keeping. It crosses down to the paper from the upper right, which is
    where his hand already is, so the hand reads as holding it.

    Yellow is a literal here, the way black rubber is on the bicycle: it is
    what a pencil is, in any palette, and a pencil in a character's accent
    colour is a stick.
    """
    body, rubber = (240, 194, 92, 255), (232, 140, 140, 255)
    tip_x, tip_y = CX + 4, GROUND - 2
    d.line([(CX + 9, GROUND - 7), (tip_x, tip_y)], fill=body, width=2)
    px(d, CX + 9, GROUND - 7, rubber)
    px(d, tip_x, tip_y, pal.line)


def hand(d, x, y, pal):
    """One hand. Outlined in `line` it is a dark blot at four pixels across, so
    it takes the shadow tone instead."""
    d.ellipse([x - 2, y - 2, x + 2, y + 2], fill=pal.body, outline=pal.body_dark)


def hands_at(state, i, sh, hip, bike=False):
    """Where the two hands are this frame.

    Shared, because the arm and the hand on the end of it are drawn at
    different depths: the arm before the dress so the dress sleeves it, the
    hand after the laptop so it rests on top. They must agree.
    """
    out = []
    for sx in (-1, 1):
        if bike and state == "working":
            # Anchored to the bar, which is anchored to the ground. His
            # shoulders rise and fall a pixel on the bob and the arms take up
            # the difference, which is what arms do.
            out.append((CX + sx * 8, BAR_Y))
            continue
        if state == "celebrate":
            hx, hy = CX + sx * 9, sh - 2
        elif state == "working":
            hx, hy = CX + sx * 5, GROUND - 2
        else:
            hx, hy = CX + sx * 8, hip - 2
        if state in ("celebrate", "working"):
            # Two hands tapping out of phase. The only asymmetry in her that is
            # meant to be there.
            hy += (i + (sx > 0)) % 2
        out.append((hx, hy))
    return out


def torso(img, s, state, i, top, bw, bh, cy, pal):
    """A small body under a big head.

    The proportions are not a style choice. At 40px the head has to stay big
    enough to carry an expression, because the expression is the entire signal
    this program sends — so what is left for a body is eight rows. That is
    enough for shoulders, two arms and a pair of feet, and the body's job is to
    hang the clothes and the necklace on and to give the hair somewhere to fall
    past. It does not act; the face does.

    The dress flares. A straight one is six rows of white rectangle and reads
    as an apron; the flare is what makes a silhouette out of it, and it is
    almost the only thing at this size that says the body is a body. The
    t-shirt cannot have it and buys the same thing with width instead — see
    the garment below.

    Drawn before the head, so the chin overlaps the shoulders rather than the
    shoulders cutting a line across the jaw. It hangs off the head rather than
    standing on the ground, which is what makes the whole character leave the
    floor on `celebrate` instead of stretching a neck.
    """
    if s.torso != "chibi":
        return
    d = ImageDraw.Draw(img)
    sh = top + bh                  # the shoulder line, right under the chin
    biking = s.work == "bike" and state == "working"
    # On the bike he is lifted clear of the ground to make room for it, so the
    # hem has to follow the shoulders instead of the floor. Left on GROUND the
    # shirt stretched to ten rows and he wore a nightshirt to work.
    hip = sh + 5 if biking else GROUND - 3
    skin, dark, line = pal.body, pal.body_dark, pal.line


    # Legs, behind the hem. Two pixels each: at three they touch and become a
    # block, and the gap between them is what reads as legs at all.
    #
    # Every bound goes through sx. The one that did not — a bare `- 1` on the
    # shoe's inner edge — widened the right shoe inward to three pixels and
    # collapsed the left onto a single column, which is the sort of asymmetry
    # that is invisible in the code and obvious the moment you look at her.
    #
    # Not at the desk: seated behind one, her legs are under it. Left in, they
    # poked out below the surface and turned the desk into a shelf she was
    # standing behind.
    leg, foot = pal.pants or skin, pal.shoe or dark
    if state != "working":
        for sx in (-1, 1):
            inner, outer = CX + sx * 2, CX + sx * 3
            rect(d, inner, hip, outer, GROUND - 1, leg)
            # The shoe is one pixel wider than the leg, on the outside.
            rect(d, inner, GROUND - 1, outer + sx, GROUND, foot)

    # Arms, posed by state — the body has no other way to join in. They hang
    # outside the dress, which is the only place there is room for them.
    #
    # The hand goes down with its arm, under the dress, in every state but
    # `working`: drawn after the dress instead, it stops being a hand at the
    # end of a sleeve and becomes a disc pasted on top of her. `working` is the
    # exception because there the hand has to sit on the laptop, and the laptop
    # is drawn after the dress for reasons of its own.
    for sx, (hx, hy) in zip((-1, 1), hands_at(state, i, sh, hip, biking)):
        d.line([(CX + sx * 4, sh + 1), (hx, hy)], fill=skin, width=3)
        if state != "working":
            hand(d, hx, hy, pal)

    # The garment: shoulders, then either a waist and a flare or neither.
    #
    # The dress flare is what makes a silhouette out of six rows of white. A
    # t-shirt must not have it — flared, a tee is a dress, and the only thing
    # in the frame saying which of the two people this is would be saying the
    # wrong one. It gets width instead: two pixels broader at the shoulder,
    # straight down to a hem that hangs a little loose.
    if s.garment == "tee":
        shirt = [(CX - 6, sh), (CX + 6, sh), (CX + 6, sh + 3), (CX + 6, hip),
                 (CX - 6, hip), (CX - 6, sh + 3)]
    else:
        shirt = [(CX - 5, sh), (CX + 5, sh), (CX + 4, sh + 3), (CX + 6, hip),
                 (CX - 6, hip), (CX - 4, sh + 3)]
    d.polygon(shirt, fill=pal.cloth or skin)
    edge = pal.cloth_line or line
    d.line(shirt[1:5], fill=edge)
    d.line([shirt[5], shirt[4]], fill=edge)

    if s.garment == "tee":
        # Sleeves: the cloth carried a little way down each arm, so the arm
        # starts at an elbow rather than at a shoulder seam. Without them the
        # shirt is a bib with two bare arms pinned to it.
        for sx in (-1, 1):
            d.polygon([(CX + sx * 4, sh), (CX + sx * 8, sh + 1),
                       (CX + sx * 7, sh + 4), (CX + sx * 3, sh + 3)],
                      fill=pal.cloth or skin)

    # The neck opening. What hangs in it is torso_front's, not this function's.
    # A crew neck is a shallow curve; the V the dress cuts is three rows deep
    # and on a t-shirt it reads as a shirt worn open.
    if s.garment == "tee":
        d.polygon([(CX - 3, sh - 1), (CX, sh + 1), (CX + 3, sh - 1)], fill=skin)
    else:
        d.polygon([(CX - 4, sh - 1), (CX, sh + 3), (CX + 4, sh - 1)], fill=skin)

    if biking:
        # A thigh out to a knee and a shin back in to a pedal, per side. The
        # knees are wider than the feet because that is what a cyclist seen
        # head on looks like, and with the wheel edge on they are the only
        # thing in the state that moves.
        #
        # Drawn after the shirt rather than before it — these come out from
        # under a hem instead of hanging behind one, and behind it the hem
        # swallowed them whole.
        for sx, fy in zip((-1, 1), feet(i)):
            knee = (CX + sx * 6, fy - 4)
            d.line([(CX + sx * 3, hip - 2), knee], fill=leg, width=3)
            d.line([knee, (CX + sx * 4, fy)], fill=leg, width=3)
            rect(d, CX + sx * 4 - 1, fy, CX + sx * 4 + 1, fy + 1, foot)



def torso_front(d, s, state, i, top, bw, bh, cy, pal):
    """Everything that belongs in front of the chin, in the order it overlaps.

    The head is drawn after the body, so anything sitting high on the chest —
    the necklace, a laptop held up to type on — has to be put down after it or
    the chin paints over it. The necklace learned that the hard way: drawn with
    the body, two of every three of its pixels were behind her jaw.

    Order here is the whole point. Necklace, then laptop over it, then hands
    over that. She is holding the laptop, so it hides the chain — which is what
    holding a laptop does.
    """
    if s.torso != "chibi":
        return
    sh = top + bh
    hip = sh + 5 if (s.work == "bike" and state == "working") else GROUND - 3
    skin, dark, line = pal.body, pal.body_dark, pal.line

    if pal.gold:
        necklace(d, CX, sh + 1, pal)

    if state == "working" and s.work == "bike":
        bicycle(d, i, pal)
        # The bar, its grips, then the hands over them: he is holding it, and
        # hands drawn under it are two discs peeping out from behind a stick.
        # The grips are what makes it a handlebar rather than a rail — a bar
        # with nothing on the ends is the one he had, and he appeared to be
        # holding a broom.
        metal = pal.shell or dark
        d.line([(CX - 10, BAR_Y), (CX + 10, BAR_Y)], fill=metal)
        for sx in (-1, 1):
            rect(d, CX + sx * 10, BAR_Y - 1, CX + sx * 9, BAR_Y + 1, (46, 44, 48, 255))
        for hx, hy in hands_at(state, i, sh, hip, True):
            hand(d, hx, hy, pal)

    elif state == "working" and s.work == "write":
        desk(d, pal)
        writing(d, i, pal)
        # Hands after the paper and before the pencil: they rest on the sheet,
        # and the pencil is in one of them.
        for hx, hy in hands_at(state, i, sh, hip):
            hand(d, hx, hy, pal)
        pencil(d, i, pal)

    elif state == "working":
        desk(d, pal)

        # Hands first, then the laptop over them. She is sitting behind it, so
        # the lid hides all but the outer edge of each hand — drawn after, they
        # punched skin-coloured holes in her own screen, which is the sort of
        # thing you only see once you look at it.
        for hx, hy in hands_at(state, i, sh, hip):
            hand(d, hx, hy, pal)

        # A laptop. The lid stops one row under her chin — the face is the
        # whole signal this state sends and nothing may climb into it — and
        # runs down to the base, which is the only way to get a screen tall
        # enough to read as a screen rather than as a bar.
        shell = pal.shell or pal.cloth_line or line
        # Both ends anchored to the ground, not to her shoulders. Hung off `sh`
        # the lid grew and shrank a pixel on the bob, and a laptop that breathes
        # is the one thing in the frame that cannot.
        base = GROUND - 1
        lid = base - 6
        rect(d, CX - 6, lid, CX + 6, base - 1, shell)
        # A dark panel with the glow on it. A pale screen the same value as the
        # shell around it leaves the whole thing one flat shape; the contrast
        # between panel and bezel is what makes it a screen. Same literal the
        # robot's face uses.
        rect(d, CX - 5, lid + 1, CX + 5, base - 2, (38, 46, 58, 255))
        # Two rows of glow whose lengths change every frame: text arriving,
        # which is what she is doing. The screen has to carry the animation now
        # that her hands are behind the lid — while they showed, the tapping
        # was the motion and the screen could sit still.
        for k, w in enumerate(((6, 3), (8, 5), (4, 7), (7, 4))[i % 4]):
            d.line([(CX - 4, lid + 2 + k * 2), (CX - 4 + w, lid + 2 + k * 2)],
                   fill=pal.accent)
        # The base, wider than the lid. That overhang is most of what says
        # laptop rather than television.
        rect(d, CX - 8, base, CX + 8, base + 1, shell)
        d.line([(CX - 8, base), (CX + 8, base)], fill=dark)




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
    if s.hair not in ("side", "curly", "crop"):
        return
    x0, x1 = CX - bw // 2, CX + bw // 2

    if s.hair == "crop":
        # Hair cut close to the head: two rows proud of the skull and no more.
        # A child's crop has no volume to draw, so what says it is the low
        # hairline the fringe puts on the forehead, not anything up here.
        d.ellipse([x0 - 1, top - 2, x1 + 1, top + 9], fill=pal.hair)
        light = pal.hair_light or pal.hair
        d.line([(CX - 5, top - 1), (CX - 2, top - 2)], fill=light)
        d.line([(CX + 2, top - 2), (CX + 5, top - 1)], fill=light)
        return

    if s.hair == "curly":
        # One smooth mass, and nothing standing on top of it.
        #
        # It was built out of overlapping lumps for a while — the wave along
        # the top edge was the only thing in the drawing saying the hair was
        # anything but straight. Five rows of it are ever seen, though, and a
        # wave inside five rows is a decoration on a shape that has no room for
        # one. The name says curly; the silhouette does not have to shout it.
        #
        # Its top is five rows above the skull and no higher: `celebrate` lifts
        # him four, and hair that leaves the top of the window in one state out
        # of ten is hair with a bug in it. The right edge stops at the head's
        # own, the way Peach's crown does and for her reason — that corner is
        # the prop column, and the Z of `sleeping` is drawn dark and drawn last,
        # so one lost in black hair leaves the state with no signal at all.
        d.ellipse([x0 - 3, top - 5, x1 + 1, top + 10], fill=pal.hair)

        # Two lit ticks, and small ones. Five rows of hair cannot carry a
        # highlight of any size: at three arcs it was a light band across the
        # head and the whole thing read as a straw hat, and at one arc per lump
        # it read as speckled. Both were tried, in that order.
        light = pal.hair_light or pal.hair
        for ax, ay in ((CX - 7, top - 3), (CX + 3, top - 4)):
            d.line([(ax, ay), (ax + 2, ay)], fill=light)
            px(d, ax + 3, ay + 1, light)
        return

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
    if s.hair not in ("side", "curly", "crop"):
        return
    overlay = Image.new("RGBA", (W, H), (0, 0, 0, 0))
    o = ImageDraw.Draw(overlay)
    x0, x1 = CX - bw // 2, CX + bw // 2

    if s.hair == "crop":
        # The fringe, and it comes down low. He has no brows to keep clear of —
        # a seven-year-old's are faint enough that drawing them at this size
        # makes him frown — so the hair takes the forehead the brows would have
        # had, which is most of what makes the face read as a child's.
        rect(o, x0 - 3, top - 4, x1 + 3, top + 3, pal.hair)
        # Cut with a comb rather than a ruler: three spikes down out of the
        # fringe, uneven and off centre. A straight edge on hair this short is
        # a swimming cap.
        for sx_, w in ((-6, 2), (-1, 3), (5, 2)):
            rect(o, CX + sx_, top + 3, CX + sx_ + w, top + 4, pal.hair)
        # Down the sides in front of the ears, and further down than an adult's
        # — short hair on a round head has nowhere else to be.
        rect(o, x0, top + 3, x0 + 1, top + bh - 9, pal.hair)
        rect(o, x1 - 1, top + 3, x1, top + bh - 9, pal.hair)
        light = pal.hair_light or pal.hair
        o.line([(CX - 4, top + 1), (CX + 1, top)], fill=light)

        mask = Image.new("L", (W, H), 0)
        head_shape(ImageDraw.Draw(mask), s, x0 + 1, top + 1, x1 - 1, top + bh - 1, fill=255)
        overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
        img.alpha_composite(overlay)
        return

    if s.hair == "curly":
        # The fringe, and the sideburns. Clipped to the head like everything
        # else drawn on it.
        #
        # It covers three rows of the head and no more. Hairline, brows and the
        # top of the frame are three dark bands stacked in seven rows of
        # forehead, and letting any pair of them touch fuses them into one band
        # across the head — a scowl he then wears in all ten states, the happy
        # ones included. A row of skin between each is what there is room for.
        #
        # It makes for a high hairline. That is the price of the brows, and
        # they are worth it: they are the only part of this face that moves
        # besides the eyes, and the eyes are three pixels wide behind glass.
        #
        # The edge is straight. It was scalloped while the mass above it was
        # made of lumps, so that the two agreed; with the mass smooth, a row of
        # bumps down here is the only curl left on the character and reads as
        # a mistake rather than as hair.
        rect(o, x0 - 3, top - 4, x1 + 3, top + 2, pal.hair)
        # Sideburns, the same length on both sides. Nothing about him is
        # asymmetrical: Peach carries hers in her hair, and he was given a
        # jumper over one shoulder for a while to carry his, but a plain
        # charcoal shirt is what he wears and symmetry is what that leaves.
        rect(o, x0, top + 2, x0 + 1, top + bh - 10, pal.hair)
        rect(o, x1 - 1, top + 2, x1, top + bh - 10, pal.hair)

        mask = Image.new("L", (W, H), 0)
        head_shape(ImageDraw.Draw(mask), s, x0 + 1, top + 1, x1 - 1, top + bh - 1, fill=255)
        overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
        img.alpha_composite(overlay)
        return

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
    head_shape(ImageDraw.Draw(mask), s, x0 + 1, top + 1, x1 - 1, top + bh - 1, fill=255)
    overlay.putalpha(ImageChops.multiply(overlay.getchannel("A"), mask))
    img.alpha_composite(overlay)


def necklace(d, cx, y, pal):
    """A fine gold chain with a drop, in the open neck.

    `y` is the first row clear of the jaw. Everything above that is behind her
    chin and cannot be seen, which leaves three rows of skin and then the dress
    — so the chain is short and the drop does the work, hanging onto the white
    where the contrast is best.

    The gold is deeper than gold looks like it should be. Straight yellow is
    almost exactly as light as her skin — 204 against 215 — so a chain in it
    disappears into her neck however many pixels it is given. What makes
    jewellery read at this size is the step down in value, not the hue.
    """
    g = pal.gold
    gl = pal.gold_light or g
    gd = pal.gold_dark or g
    for k in range(2):
        px(d, cx - 2 + k, y + k, g)
        px(d, cx + 2 - k, y + k, g)
    # A lit top, a wide middle, a shaded foot. Two pixels of drop is one more
    # link of chain; three with a highlight is a pendant.
    px(d, cx, y + 2, gl)
    d.line([(cx - 1, y + 3), (cx + 1, y + 3)], fill=g)
    px(d, cx, y + 4, gd)


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
    # A rider sits above his bicycle, so `working` lifts him — as far as it can
    # before the curls on top of his head meet the top of the window, and one
    # row past that, which the frame is welcome to have.
    #
    # And he stops bobbing on it. The bob is a body bouncing on its own; on a
    # saddle the bouncing is done by the legs, and the frame the bob carried an
    # extra row upward was the one that lost a quarter of his hair.
    lift = 4 if (s.work == "bike" and state == "working") else 0
    if lift:
        bob = 0
    cy = 23 + bob + s.head_dy - lift
    bw, bh = s.body_w, s.body_h

    # Squash and stretch. A body that only translates looks like a sticker;
    # one that deforms slightly on the beat looks alive.
    if bob < 0:
        bw, bh = bw - 1, bh + 1
    elif bob > 0:
        bw, bh = bw + 1, bh - 1

    # ground shadow — shrinks as the pet rises, which is what sells the hop.
    # A character with a body casts it from the feet, not from the head, or a
    # big head throws a shadow wider than anything touching the floor.
    #
    # None at the desk: the floor it would fall on is behind the desk and out of
    # sight, so it read as her hovering just in front of the surface.
    if not (s.torso == "chibi" and state == "working"):
        shadow_w = (12 if s.torso == "chibi" else bw - 2) + bob
        d.ellipse([CX - shadow_w // 2, GROUND, CX + shadow_w // 2, GROUND + 3],
                  fill=(0, 0, 0, 60))

    top = cy - bh // 2

    # --- the body, then the hair over it: she wears her hair forward, so it
    #     falls across the shoulder rather than behind it
    torso(img, s, state, i, top, bw, bh, cy, pal)

    # --- hair, behind the head: it is the silhouette, so it goes down first
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
    head_shape(d, s, CX - bw // 2, top, CX + bw // 2, top + bh,
               fill=pal.body, outline=pal.line)
    if s.torso != "none":
        # Soften the jaw. The head's outline is there to part the silhouette
        # from the background, but along the bottom the head meets her own
        # shoulders — nothing to part — and `line` drawn there is a hard brown
        # bar directly under the mouth. The arc stops short of the sides, which
        # do overhang the body and still need the real outline.
        if s.head == "square":
            # A straight jaw takes a straight softening, inset by the corner
            # radius so the two corners keep the outline that shapes them.
            d.line([(CX - bw // 2 + JAW_R, top + bh), (CX + bw // 2 - JAW_R, top + bh)],
                   fill=pal.jaw or pal.body_dark)
        else:
            d.arc([CX - bw // 2, top, CX + bw // 2, top + bh], 55, 125,
                  fill=pal.jaw or pal.body_dark)
    markings(img, s, top, bw, bh, cy, pal)
    torso_front(d, s, state, i, top, bw, bh, cy, pal)
    hairline(img, s, top, bw, bh, cy, pal)
    if s.accessory == "bow":
        # On the tucked side: the other one is under the fall of the hair, and
        # a bow you cannot see is three pixels of nothing.
        bow(d, CX - bw // 2 + 2, top + 4, pal)

    # White socks, on a cat that has them.
    paw = pal.white if s.markings == "tortie" else pal.body

    # --- limbs that must sit on top of the body
    if s.torso == "chibi":
        pass
    elif state == "celebrate":
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
    # Props are placed against the frame, not against the character: the
    # top-right corner is reserved for them, and a head that rides higher would
    # otherwise push the Z's of `sleeping` off the top edge entirely.
    pcy = cy - s.head_dy + lift
    ey = cy - 4 + s.eye_dy
    my = cy + 4

    if s.face == "human":
        # A face, not a muzzle: nothing is drawn between the eyes and the
        # mouth.
        my = cy + 2 + s.mouth_dy
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
    if s.soft_blush and state != "sleeping":
        blush(d, s, CX, ey, pal, soft=True)

    # Whiskers sit under the expression, so a brow or a wide eye still wins.
    if s.markings == "tortie" and state != "sleeping":
        whiskers(d, face_cx, my, pal)

    # Brows first: every eye kind below is drawn inside the lens, and the brows
    # are outside and above it, so nothing overlaps and the order is free. They
    # go first because that is the order a face is built in.
    if s.brows:
        eyebrows(d, face_cx, ey - 5, BROWS.get(state, "flat"), pal)

    if state == "idle":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal,
             lashes=s.lashes, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "cat", pal, mstyle)
    elif state == "thinking":
        eyes(d, face_cx, ey, "closed" if blink else "dot", pal, look=(-1, -1),
             lashes=s.lashes, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        dots(d, PROP_X + 1, pcy - 10, pal.think or pal.accent, pal.line, i)
    elif state == "working":
        eyes(d, face_cx, ey, "squint", pal, lashes=s.lashes, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        if s.work == "bike":
            # Sweat, and no sparkles.
            #
            # The sparkles say something is going well at the desk, and they
            # only say it while there is a desk: beside a bicycle the same two
            # marks are an orange dot nobody can name. This state already has a
            # bicycle in it to say what he is doing, so the props can say how
            # hard it is instead.
            #
            # Four drops off both temples, each falling two rows and then
            # giving way to the next, so two are in the air at any moment. One
            # pair alternating in place reads as a face with two blue marks
            # stuck to it, which is what he had.
            #
            # They alternate sides down the list, and that is load-bearing: the
            # two in the air are always consecutive, so listing both left ones
            # first put both of them over the same temple on half the frames,
            # where they touched and fused into one long drip.
            for k, (dx, dy) in enumerate(((-11, -9), (9, -10), (-13, -4), (11, -5))):
                phase = (i - k) % 4
                if phase < 2:
                    sweat(d, CX + dx, pcy + dy + phase * 2, (118, 190, 236, 255))
        else:
            for k in range(2):
                if (i + k) % 2 == 0:
                    sparkle(d, PROP_X + 3 + k * 5, pcy - 9 + k * 4, pal.accent, 1)
    elif state == "attention":
        eyes(d, face_cx, ey, "wide", pal, lashes=s.lashes, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "o", pal, mstyle)
        bang(d, PROP_X + 4, pcy - 16 + i % 2, (236, 78, 78, 255))
    elif state == "confused":
        eyes(d, face_cx, ey, "confused", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "wobble", pal, mstyle)
        question(d, PROP_X + 4, pcy - 14 - (i % 2), pal.line)
    elif state == "worried":
        eyes(d, face_cx, ey, "worried", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "wobble", pal, mstyle)
        sweat(d, PROP_X + 2, pcy - 9 + (i % 2) * 2, (118, 190, 236, 255))
    elif state == "happy":
        eyes(d, face_cx, ey, "happy", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "wide_smile", pal, mstyle)
        blush(d, s, CX, ey, pal)
    elif state == "celebrate":
        eyes(d, face_cx, ey, "sparkle", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "wide_smile", pal, mstyle)
        for k, (sx, sy) in enumerate([(13, -14), (-13, -12), (16, -6), (-15, -3)]):
            if (i + k) % 3 != 2:
                sparkle(d, CX + sx, pcy + sy, pal.accent, 1 + (i + k) % 2)
    elif state == "sleeping":
        eyes(d, face_cx, ey + 1, "closed", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "flat", pal, mstyle)
        zzz(d, PROP_X + 2, pcy - 12, pal.line, i)
    elif state == "heart":
        eyes(d, face_cx, ey, "happy", pal, own_brows=s.brows, big=s.big_eyes)
        mouth(d, face_cx, my, "smile", pal, mstyle)
        blush(d, s, CX, ey, pal)
        heart(d, PROP_X + 4, pcy - 13 - (i % 2), (236, 84, 116, 255))
        if i % 2 == 0:
            heart(d, PROP_X - 1, pcy - 7, (236, 132, 152, 210), big=False)

    # Glass goes on last of everything the face has, so a pupil that dilates
    # past the rim in `attention` ends up behind it.
    if s.glasses != "none":
        spectacles(d, face_cx, ey, pal, s.glasses)

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
