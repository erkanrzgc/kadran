"""Outline three locally installed fonts for a KΛDRΛN typography comparison."""
from pathlib import Path
from html import escape
import json

from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.boundsPen import BoundsPen

ROOT = Path(__file__).resolve().parent
TEXT = "KΛDRΛN"
VARIANTS = [
    ("corbel", "Corbel Light", "Zarif · yumuşak", "C:/Windows/Fonts/corbell.ttf", {}, 0.035),
    ("bahnschrift", "Bahnschrift Light SemiCondensed", "Modern · kompakt", "C:/Windows/Fonts/bahnschrift.ttf", {"wght": 300, "wdth": 87.5}, 0.07),
    ("constantia", "Constantia", "Karakterli · rafine", "C:/Windows/Fonts/constan.ttf", {}, 0.025),
]


def artwork(path, axes, tracking):
    font = TTFont(path)
    if axes:
        font = instantiateVariableFont(font, axes, inplace=True)
    cmap = font.getBestCmap()
    if any(ord(character) not in cmap for character in TEXT):
        raise ValueError("Every character must be native to the selected font")
    glyphs = font.getGlyphSet()
    cap_pen = BoundsPen(glyphs)
    glyphs[cmap[ord("H")]].draw(cap_pen)
    cap_height = cap_pen.bounds[3] - cap_pen.bounds[1]
    scale = 80 / cap_height
    gap = font["head"].unitsPerEm * tracking
    cursor = 0
    commands, bounds = [], []
    prior = None
    kern = {}
    if "kern" in font:
        for table in font["kern"].kernTables:
            kern.update(getattr(table, "kernTable", {}))
    for character in TEXT:
        name = cmap[ord(character)]
        if prior:
            cursor += kern.get((prior, name), 0)
        pen = SVGPathPen(glyphs)
        glyphs[name].draw(pen)
        bb = BoundsPen(glyphs)
        glyphs[name].draw(bb)
        xmin, ymin, xmax, ymax = bb.bounds
        bounds.append((cursor + xmin, ymin, cursor + xmax, ymax))
        commands.append(f'<path d="{pen.getCommands()}" transform="translate({cursor:g} 0)"/>')
        cursor += font["hmtx"].metrics[name][0] + gap
        prior = name
    xmin = min(box[0] for box in bounds)
    xmax = max(box[2] for box in bounds)
    ymin = min(box[1] for box in bounds)
    ymax = max(box[3] for box in bounds)
    width, height = (xmax - xmin) * scale, (ymax - ymin) * scale
    outline = ''.join(commands)
    return outline, xmin, ymax, width, height, scale


def group(art, x, y, color="#EDF1F5", extra_scale=1):
    outline, xmin, ymax, _, _, scale = art
    return f'<g transform="translate({x:g} {y:g}) scale({extra_scale:g})"><g fill="{color}" transform="scale({scale:g} {-scale:g}) translate({-xmin:g} {-ymax:g})">{outline}</g></g>'


cards, metadata = [], []
for index, (key, name, description, path, axes, tracking) in enumerate(VARIANTS, 1):
    art = artwork(path, axes, tracking)
    width, height = art[3:5]
    logo = group(art, (586 - width) / 2, (176 - height) / 2)
    for suffix, color in [("", "#EDF1F5"), ("-dark", "#18232E")]:
        content = logo.replace('#EDF1F5', color)
        svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="2344" height="704" viewBox="0 0 586 176" role="img" aria-labelledby="title"><title id="title">KΛDRΛN — {escape(name)}</title>{content}</svg>\n'
        (ROOT / f"kadran-font-{key}{suffix}.svg").write_text(svg, encoding="utf-8")
    y = 105 + (index - 1) * 210
    fit = min(1, 700 / width)
    card = f'<rect x="28" y="{y}" width="844" height="199" rx="18" fill="#151D26" stroke="#2B3948"/>'
    card += f'<text x="53" y="{y + 35}" class="name">0{index} / {escape(name.upper())}</text>'
    card += f'<text x="847" y="{y + 35}" class="description" text-anchor="end">{escape(description)}</text>'
    card += group(art, (900 - width * fit) / 2, y + 79, extra_scale=fit)
    cards.append(card)
    metadata.append({"id": index, "key": key, "font": name, "source": path, "axes": axes, "tracking_em": tracking, "text": TEXT, "native_lambda": True})

board = '<svg xmlns="http://www.w3.org/2000/svg" width="1800" height="1500" viewBox="0 0 900 750"><style>.name{font:600 13px Segoe UI,sans-serif;letter-spacing:1px;fill:#BBC9D8}.description{font:13px Segoe UI,sans-serif;fill:#93A5B8}</style><rect width="900" height="750" fill="#0C1219"/><text x="30" y="46" fill="#EDF1F5" font-family="Segoe UI,sans-serif" font-size="24" font-weight="600">KΛDRΛN · font seçimi</text><text x="30" y="75" fill="#93A5B8" font-family="Segoe UI,sans-serif" font-size="14">Üç farklı karakter · aynı harf yüksekliği · efektsiz tipografi</text>' + ''.join(cards) + '<text x="30" y="742" fill="#8294A7" font-family="Segoe UI,sans-serif" font-size="12">Kart zeminleri yalnız karşılaştırma içindir. Tekil PNG ve SVG logolar transparandır.</text></svg>\n'
(ROOT / "font-comparison.svg").write_text(board, encoding="utf-8")
(ROOT / "font-study.json").write_text(json.dumps(metadata, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(json.dumps(metadata, ensure_ascii=True))
