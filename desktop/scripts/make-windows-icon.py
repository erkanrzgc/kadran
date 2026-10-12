"""Convert the outlined K native icon PNG to Windows ICO.

Run with Python 3 and Pillow 12.2.0. The generated ICO is committed, so
desktop build/packaging does not depend on Python or Pillow at runtime.
"""

import hashlib
import json
from pathlib import Path

import PIL
from PIL import Image

DESKTOP = Path(__file__).resolve().parents[1]
MASTER = DESKTOP / "assets" / "kadran-icon.svg"
SOURCE = DESKTOP / "public" / "kadran-icon.png"
OUTPUT = DESKTOP / "assets" / "kadran.ico"
SIZES = [(size, size) for size in (16, 24, 32, 48, 64, 128, 256)]

source_bytes = SOURCE.read_bytes()
with Image.open(SOURCE) as source:
    if source.width != source.height:
        raise ValueError("The native icon must be square; cropping is not allowed")
    rgba = source.convert("RGBA")
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    # Pillow's ICO encoder uses LANCZOS for its size-only downsampling and
    # embeds RGBA PNG frames, preserving transparency in every icon size.
    rgba.save(OUTPUT, format="ICO", sizes=SIZES, bitmap_format="png")

assert SOURCE.read_bytes() == source_bytes, "Native source PNG must remain unchanged"
manifest = {
    "master": "kadran-icon.svg",
    "master_sha256": hashlib.sha256(MASTER.read_bytes()).hexdigest(),
    "source": "../public/kadran-icon.png",
    "source_sha256": hashlib.sha256(source_bytes).hexdigest(),
    "ico_sha256": hashlib.sha256(OUTPUT.read_bytes()).hexdigest(),
    "sizes": [size[0] for size in SIZES],
    "encoder": f"Pillow {PIL.__version__} ICO with RGBA PNG frames",
    "font": "Bahnschrift Light SemiCondensed",
    "font_axes": {"wght": 300, "wdth": 87.5},
    "glyph": "K",
    "fill": "#5B96BA",
    "transform": "RGBA format conversion and LANCZOS resizing only; native source PNG unchanged",
}
(OUTPUT.parent / "icon-provenance.json").write_text(
    json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
)
print(f"Windows icon: {OUTPUT}")
