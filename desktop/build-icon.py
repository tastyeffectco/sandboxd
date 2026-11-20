#!/usr/bin/env python3
"""Generate the native app icons from sandboxd's brand mark (the sandboxd
Boxbot on its tile, console/public/favicon.svg), so the app, its window and
the console's browser tab all show the same icon.

Maintainer-only: python3 -m pip install Pillow cairosvg; python3 desktop/build-icon.py
Generated assets are checked in; packaging needs no Python dependencies.
"""
import io
from pathlib import Path

import cairosvg
from PIL import Image

here = Path(__file__).parent
svg = here.parent / 'console' / 'public' / 'favicon.svg'
root = here / 'build'
root.mkdir(exist_ok=True)
png = cairosvg.svg2png(url=str(svg), output_width=1024, output_height=1024)
image = Image.open(io.BytesIO(png)).convert('RGBA')
image.save(root / 'icon.png')
image.save(root / 'icon.icns')
image.save(root / 'icon.ico', sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
