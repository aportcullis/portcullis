"""Encode readable step walkthroughs from real application captures (Pillow)."""

import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


root = Path(__file__).resolve().parents[2]
groups = json.loads((root / ".test-docker/readme-media/frames.json").read_text())
font = ImageFont.load_default(size=20)
for name, records in groups.items():
    frames = []
    for record in records:
        screenshot = Image.open(record["path"]).convert("RGB")
        canvas = Image.new("RGB", (screenshot.width, screenshot.height + 56), "#102A36")
        canvas.paste(screenshot, (0, 56))
        ImageDraw.Draw(canvas).text((24, 17), record["caption"], font=font, fill="#F5F4EF")
        frames.append(canvas.quantize(colors=128))
    destination = root / "docs/media" / (name + ".gif")
    frames[0].save(destination, save_all=True, append_images=frames[1:],
                   duration=[record["duration"] for record in records], loop=0,
                   optimize=True, disposal=2)
    print(f"{destination.name}: {destination.stat().st_size / 1024:.0f} KiB")
