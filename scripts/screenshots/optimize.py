"""Reduce screenshots to 256-color PNGs, about a quarter of the size.

usage: optimize.py OUT_DIR IMAGE.png...
"""
import os
import sys

from PIL import Image

out_dir = sys.argv[1]
for path in sys.argv[2:]:
    image = Image.open(path).quantize(colors=256, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.NONE)
    image.save(os.path.join(out_dir, os.path.basename(path)), optimize=True)
