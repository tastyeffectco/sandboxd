#!/usr/bin/env python3
"""Generate native icons from the same simple cube used by the console.

Maintainer-only: python3 -m pip install Pillow; python3 desktop/build-icon.py
Generated assets are checked in; packaging needs no Python dependencies.
"""
from pathlib import Path
from PIL import Image, ImageDraw

root = Path(__file__).parent / 'build'
root.mkdir(exist_ok=True)
image = Image.new('RGBA', (1024, 1024))
draw = ImageDraw.Draw(image)
draw.rounded_rectangle((48, 48, 976, 976), radius=225, fill='#214c3b')
points = [(512, 245), (744, 379), (744, 645), (512, 779), (280, 645), (280, 379), (512, 245)]
draw.line(points, fill='#eef5e9', width=30, joint='curve')
draw.line([(280, 379), (512, 513), (744, 379)], fill='#eef5e9', width=30, joint='curve')
draw.line([(512, 513), (512, 779)], fill='#eef5e9', width=30)
image.save(root / 'icon.png')
image.save(root / 'icon.icns')
image.save(root / 'icon.ico', sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
