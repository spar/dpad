"""Embed the subset of JetBrains Mono that each SVG uses, so the images look
the same everywhere without depending on the viewer's fonts.

usage: font.py IMAGE.svg...

Set DP_FONT_DIR to the folder holding JetBrainsMono-Regular.ttf and
JetBrainsMono-Bold.ttf if they aren't in a usual font folder.
"""
import base64
import glob
import html
import io
import os
import re
import sys

from fontTools import subset
from fontTools.ttLib import TTFont

DIRS = [
    os.environ.get("DP_FONT_DIR", ""),
    "~/Library/Fonts", "/Library/Fonts", "~/.local/share/fonts", "~/.fonts",
    "/usr/share/fonts", "/usr/local/share/fonts", os.path.expandvars("$LOCALAPPDATA/Microsoft/Windows/Fonts"),
    "C:/Windows/Fonts",
]


def find(name):
    for d in filter(None, DIRS):
        hits = glob.glob(os.path.join(os.path.expanduser(d), "**", name), recursive=True)
        if hits:
            return hits[0]
    sys.exit("font: %s not found; install JetBrains Mono or set DP_FONT_DIR" % name)


FACES = [(400, find("JetBrainsMono-Regular.ttf")), (700, find("JetBrainsMono-Bold.ttf"))]


def woff2(path, text):
    font = TTFont(path)
    options = subset.Options()
    options.flavor = "woff2"
    options.layout_features = []
    options.name_IDs = []
    options.notdef_outline = True
    sub = subset.Subsetter(options)
    sub.populate(text=text)
    sub.subset(font)
    out = io.BytesIO()
    font.flavor = "woff2"
    font.save(out)
    return out.getvalue(), set(font.getBestCmap())


for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as f:
        svg = f.read()
    text = "".join(html.unescape(t) for t in re.findall(r"<text[^>]*>([^<]*)</text>", svg))
    chars = set(text) - {" "}
    rules = []
    for weight, ttf in FACES:
        data, covered = woff2(ttf, "".join(chars))
        rules.append(
            "@font-face{font-family:'dp mono';font-weight:%d;src:url(data:font/woff2;base64,%s) format('woff2')}"
            % (weight, base64.b64encode(data).decode())
        )
    missing = sorted(c for c in chars if ord(c) not in covered)
    if missing:
        print("font: %s: no glyph for %s; the viewer's font draws them" % (path, " ".join(missing)), file=sys.stderr)
    with open(path, "w", encoding="utf-8") as f:
        f.write(svg.replace("/*fonts*/", "".join(rules), 1))
    print("%s: %d KiB" % (path, os.path.getsize(path) // 1024))
