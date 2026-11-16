"""Draw the sbgui icon and emit a macOS .iconset directory.

Usage: python3 icon_maker.py <out.iconset>   (requires Pillow)
Then:  iconutil -c icns <out.iconset> -o AppIcon.icns
"""
import sys
from PIL import Image, ImageDraw

S = 1024
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
inset, rad = 92, 190
top, bot = (38, 44, 92), (13, 88, 96)
grad = Image.new("RGBA", (1, S))
for y in range(S):
    t = y / S
    grad.putpixel((0, y), tuple(int(top[i] + (bot[i] - top[i]) * t) for i in range(3)) + (255,))
grad = grad.resize((S, S))
mask = Image.new("L", (S, S), 0)
md = ImageDraw.Draw(mask)
md.rounded_rectangle([inset, inset, S - inset, S - inset], radius=rad, fill=255)
img.paste(grad, (0, 0), mask)
d = ImageDraw.Draw(img)
for x, y, w, h in [(300, 640, 120, 210), (462, 560, 120, 290), (624, 470, 120, 380)]:
    d.rounded_rectangle([x, y, x + w, y + h], radius=34, fill=(255, 255, 255, 255))
d.ellipse([640, 268, 724, 352], fill=(255, 212, 92, 255))

import os
out = sys.argv[1]
os.makedirs(out, exist_ok=True)
spec = [
    ("icon_16x16.png", 16), ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32), ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128), ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256), ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512), ("icon_512x512@2x.png", 1024),
]
for name, size in spec:
    img.resize((size, size), Image.LANCZOS).save(os.path.join(out, name))
print("iconset written to", out)
