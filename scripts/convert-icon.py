#!/usr/bin/env python3
"""Convert a transparent-background/black-glyph icon PNG (the usual export
from an icon library) into this project's icon convention: an opaque black
background with the glyph drawn bright, since internal/images assets are
composited with draw.Src, which needs an opaque source.

The source alpha channel becomes the destination grayscale value, so
antialiasing is preserved as a brightness gradient instead of a transparency
gradient.

Usage:
    scripts/convert-icon.py source.png [output.png]

output.png defaults to source.png, overwriting it in place. Run this before
embedding a new icon into internal/images (see data.go/images.go for how
existing icons are registered).
"""
import sys

from PIL import Image


def convert(src_path: str, dst_path: str) -> None:
    with Image.open(src_path) as im:
        im = im.convert("RGBA")
        px = im.load()
        out = Image.new("RGBA", im.size, (0, 0, 0, 255))
        opx = out.load()
        for y in range(im.height):
            for x in range(im.width):
                _, _, _, a = px[x, y]
                opx[x, y] = (a, a, a, 255)
    out.save(dst_path)


def main() -> None:
    if len(sys.argv) not in (2, 3):
        print(__doc__)
        sys.exit(1)
    src = sys.argv[1]
    dst = sys.argv[2] if len(sys.argv) == 3 else src
    convert(src, dst)


if __name__ == "__main__":
    main()
