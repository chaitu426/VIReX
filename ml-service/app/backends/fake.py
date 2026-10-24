"""Test doubles. They find coloured rectangles instead of real objects so the whole
Go <-> Python path can be exercised without torch or model weights.

  red   (R>170, G<90, B<90)   -> "person"
  blue  (B>170, R<90, G<90)   -> "cell phone"
  green (G>170, R<90, B<90)   -> text line (content from VIREX_FAKE_OCR_TEXT)

Never use these for results you want to keep."""
from __future__ import annotations

import os
from collections import deque
from typing import Callable, Sequence

import numpy as np

from .base import Box, Det, Line, ModelInfo, Serialised

_STEP = 4  # look at one pixel in 16
_MIN_CELLS = 4


def _components(mask: np.ndarray) -> list[tuple[int, int, int, int]]:
    """4-connected components of a small boolean grid -> (x1, y1, x2, y2) in grid cells."""
    h, w = mask.shape
    seen = np.zeros_like(mask, dtype=bool)
    out: list[tuple[int, int, int, int]] = []
    for sy in range(h):
        for sx in range(w):
            if not mask[sy, sx] or seen[sy, sx]:
                continue
            q = deque([(sx, sy)])
            seen[sy, sx] = True
            x1 = x2 = sx
            y1 = y2 = sy
            n = 0
            while q:
                x, y = q.popleft()
                n += 1
                x1, x2, y1, y2 = min(x1, x), max(x2, x), min(y1, y), max(y2, y)
                for nx, ny in ((x + 1, y), (x - 1, y), (x, y + 1), (x, y - 1)):
                    if 0 <= nx < w and 0 <= ny < h and mask[ny, nx] and not seen[ny, nx]:
                        seen[ny, nx] = True
                        q.append((nx, ny))
            if n >= _MIN_CELLS:
                out.append((x1, y1, x2 + 1, y2 + 1))
    return out


def _boxes(rgb: np.ndarray, pred: Callable[[np.ndarray, np.ndarray, np.ndarray], np.ndarray]) -> list[Box]:
    small = rgb[::_STEP, ::_STEP]
    r, g, b = (small[:, :, i].astype(np.int16) for i in range(3))
    gh, gw = small.shape[:2]
    return [
        Box(x1 / gw, y1 / gh, (x2 - x1) / gw, (y2 - y1) / gh)
        for x1, y1, x2, y2 in _components(pred(r, g, b))
    ]


class FakeDetector(Serialised):
    def __init__(self) -> None:
        super().__init__()
        self.info = ModelInfo("fake-colour-detector", "1", "fake", "cpu", ("person", "cell phone"))

    def detect(self, rgb, min_conf, max_det, classes: Sequence[str], imgsz):
        rules = (
            ("person", 0, lambda r, g, b: (r > 170) & (g < 90) & (b < 90)),
            ("cell phone", 67, lambda r, g, b: (b > 170) & (r < 90) & (g < 90)),
        )
        out: list[Det] = []
        with self._lock:
            for name, cid, pred in rules:
                if classes and name not in classes:
                    continue
                out += [Det(name, cid, 0.9, bx) for bx in _boxes(rgb, pred)]
        return out[:max_det]


class FakeTextReader(Serialised):
    def __init__(self) -> None:
        super().__init__()
        self.info = ModelInfo("fake-colour-ocr", "1", "fake", "cpu")
        self._text = os.environ.get("VIREX_FAKE_OCR_TEXT", "FAKE TEXT")

    def read(self, rgb, min_conf, languages):
        with self._lock:
            return [Line(self._text, 0.9, bx) for bx in _boxes(rgb, lambda r, g, b: (g > 170) & (r < 90) & (b < 90))]
