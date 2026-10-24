import io

import numpy as np
from PIL import Image


def frame(width=320, height=180, boxes=()):
    """White frame with solid rectangles: boxes = [((r, g, b), x1, y1, x2, y2), ...] in pixels."""
    img = np.full((height, width, 3), 255, np.uint8)
    for (rgb, x1, y1, x2, y2) in boxes:
        img[y1:y2, x1:x2] = rgb
    return img


def jpeg(arr, quality=95) -> bytes:
    b = io.BytesIO()
    Image.fromarray(arr).save(b, "JPEG", quality=quality)
    return b.getvalue()


def png(arr) -> bytes:
    b = io.BytesIO()
    Image.fromarray(arr).save(b, "PNG")
    return b.getvalue()


RED, BLUE, GREEN = (230, 20, 20), (20, 20, 230), (20, 230, 20)
