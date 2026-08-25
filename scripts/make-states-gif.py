#!/usr/bin/env python3
"""Render the pet's states to an animated GIF, straight from the shipped art.

This is the figure for the README: every state the pet can be in, at the speed
it actually runs. It reads the same manifest and the same sprite strips the app
does, so it cannot drift into showing something the pet does not do — the usual
failure of a hand-made demo image.

Each state plays whole animation cycles, so the loop has no seam, and GIF's
per-frame delays carry each state's real fps rather than resampling everything
onto one clock.

Every frame shares one palette. GIF stores a palette per frame but players
apply the first one to the whole file, so quantising each frame on its own
turns a tortoiseshell cat green from the second state onward.

  python3 scripts/make-states-gif.py [--pet momo] [--scale 6] [--out PATH]
"""
import argparse
import json
import pathlib
import sys

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    sys.exit("needs Pillow: pip install Pillow")

ROOT = pathlib.Path(__file__).resolve().parent.parent
PACKS = ROOT / "ui/dist/pets"

# Session order, not manifest order: this reads as an arc — waking up, working,
# needing you, going wrong, coming right, and settling — rather than a list.
ORDER = ["idle", "thinking", "working", "attention", "confused",
         "worried", "happy", "celebrate", "heart", "sleeping"]

BG = (22, 22, 26)
FG = (232, 232, 236)
DIM = (128, 128, 140)
MIN_HOLD = 0.8  # seconds; rounded up to whole cycles

FONTS = ["/System/Library/Fonts/SFNSMono.ttf",
         "/System/Library/Fonts/Menlo.ttc",
         "/System/Library/Fonts/Supplemental/Andale Mono.ttf"]


def load_font(size):
    for f in FONTS:
        if pathlib.Path(f).exists():
            try:
                return ImageFont.truetype(f, size)
            except OSError:
                continue
    return ImageFont.load_default()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--scale", type=int, default=6,
                    help="integer only: the art is pixel art and a fractional "
                         "resize turns it to mush")
    ap.add_argument("--pet", default="momo",
                    help="which shipped pack to render")
    # Named after the pet unless told otherwise, so rendering a second
    # character cannot quietly overwrite the first one's figure.
    ap.add_argument("--out", default=None)
    args = ap.parse_args()

    pack = PACKS / args.pet
    if not pack.is_dir():
        have = ", ".join(sorted(p.name for p in PACKS.iterdir() if p.is_dir()))
        sys.exit(f"no pack called {args.pet!r} in {PACKS} — have: {have}")
    if args.out is None:
        args.out = ("docs/media/states.gif" if args.pet == "momo"
                    else f"docs/media/states-{args.pet}.gif")

    man = json.loads((pack / "manifest.json").read_text())
    fw, fh = man["frame_width"], man["frame_height"]
    anims = man["animations"]

    pet = fw * args.scale
    pad = 24
    label_h = 34
    W = pet + pad * 2
    H = pet + pad + label_h

    big = load_font(17)
    small = load_font(12)

    frames, delays = [], []
    for name in ORDER:
        a = anims.get(name)
        if a is None:
            print(f"  skipping {name}: not in the manifest", file=sys.stderr)
            continue
        strip = Image.open(pack / a["file"]).convert("RGBA")
        n, fps = a["frames"], float(a["fps"])
        cycle = n / fps
        cycles = max(1, -(-MIN_HOLD // cycle))  # ceil
        per = 1.0 / fps

        for _ in range(int(cycles)):
            for i in range(n):
                cell = strip.crop((i * fw, 0, (i + 1) * fw, fh))
                # NEAREST is the whole point: anything else blurs the pixels.
                cell = cell.resize((pet, pet), Image.NEAREST)

                canvas = Image.new("RGBA", (W, H), BG + (255,))
                canvas.alpha_composite(cell, (pad, pad // 2))
                d = ImageDraw.Draw(canvas)
                d.text((W // 2, pet + pad // 2 + 12), name,
                       font=big, fill=FG, anchor="mm")
                d.text((W // 2, pet + pad // 2 + 30),
                       f"{n} frames · {fps:g} fps", font=small, fill=DIM, anchor="mm")
                frames.append(canvas.convert("RGB"))
                delays.append(int(round(per * 1000)))

    # One palette for every frame, derived from all of them at once. The
    # sprites use 20 colours; the rest of the budget is antialiased label text.
    tall = Image.new("RGB", (W, H * len(frames)))
    for i, f in enumerate(frames):
        tall.paste(f, (0, i * H))
    master = tall.quantize(colors=64, dither=Image.NONE)
    # dither=NONE throughout: dithering scatters pixels that are meant to be
    # flat, which on pixel art reads as dirt.
    frames = [f.quantize(palette=master, dither=Image.NONE) for f in frames]

    out = ROOT / args.out
    out.parent.mkdir(parents=True, exist_ok=True)
    frames[0].save(out, save_all=True, append_images=frames[1:],
                   duration=delays, loop=0, optimize=True)
    total = sum(delays) / 1000
    kb = out.stat().st_size / 1024
    print(f"{args.out}: {len(frames)} frames, {total:.1f}s loop, {kb:.0f} KB, {W}x{H}")


if __name__ == "__main__":
    main()
