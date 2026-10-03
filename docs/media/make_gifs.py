"""Encode readable step walkthroughs from real application captures (Pillow)."""

import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


root = Path(__file__).resolve().parents[2]
groups = json.loads((root / ".test-docker/readme-media/frames.json").read_text())
font = ImageFont.load_default(size=20)
for name, records in groups.items():
    frames = []
    screenshots = [Image.open(record["path"]).convert("RGB") for record in records]
    width = max(screen.width for screen in screenshots)
    height = max(screen.height for screen in screenshots)
    for record, screenshot in zip(records, screenshots):
        canvas = Image.new("RGB", (width, height + 56), "white")
        ImageDraw.Draw(canvas).rectangle((0, 0, width, 56), fill="#102A36")
        canvas.paste(screenshot, (0, 56))
        ImageDraw.Draw(canvas).text((24, 17), record["caption"], font=font, fill="#F5F4EF")
        frames.append(canvas.quantize(colors=128))
    destination = root / "docs/media" / (name + ".gif")
    frames[0].save(destination, save_all=True, append_images=frames[1:],
                   duration=[record["duration"] for record in records], loop=0,
                   optimize=True, disposal=2)
    print(f"{destination.name}: {destination.stat().st_size / 1024:.0f} KiB")
