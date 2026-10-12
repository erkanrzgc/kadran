"""Promote font option 02 to standalone theme assets and a font-free React SVG."""
from pathlib import Path
import runpy

ROOT = Path(__file__).resolve().parents[2]
BRAND = Path(__file__).resolve().parent
helpers = runpy.run_path(str(BRAND / "generate-font-study.py"))
art = helpers["artwork"]("C:/Windows/Fonts/bahnschrift.ttf", {"wght": 300, "wdth": 87.5}, 0.07)
width, height = art[3:5]
viewbox = f"0 0 {width + 12:g} {height + 12:g}"
for folder in [ROOT / "docs/assets", ROOT / "desktop/public"]:
    folder.mkdir(parents=True, exist_ok=True)
    for suffix, color in [("", "#EDF1F5"), ("-dark", "#18232E")]:
        shapes = helpers["group"](art, 6, 6, color=color)
        svg = f'<svg xmlns="http://www.w3.org/2000/svg" width="2344" height="560" viewBox="{viewbox}" role="img" aria-labelledby="title"><title id="title">KΛDRΛN</title>{shapes}</svg>\n'
        (folder / f"kadran-wordmark{suffix}.svg").write_text(svg, encoding="utf-8")

shapes = helpers["group"](art, 6, 6, color="currentColor")
component = f'''/** Selected Bahnschrift outlines; rendering never depends on an installed font. */
export function BrandWordmark({{ label }}: {{ label: string }}) {{
  return (
    <svg className="brand-logo" viewBox="{viewbox}" role="img" aria-label={{label}} data-testid="brand-logo">
      {shapes}
    </svg>
  );
}}
'''
(ROOT / "desktop/src/renderer/components/BrandWordmark.tsx").write_text(component, encoding="utf-8")
print("Selected font02: standalone SVG assets and currentColor React wordmark generated")
