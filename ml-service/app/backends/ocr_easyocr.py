"""EasyOCR text reader. Shares PyTorch with the detector, so the image stays small."""
from __future__ import annotations

import logging
import os
from typing import Sequence

import numpy as np

from ..imaging import to_bgr
from .base import Box, Line, ModelInfo, Serialised
from .yolo import resolve_device

log = logging.getLogger(__name__)


class EasyOCRReader(Serialised):
    def __init__(self, languages: Sequence[str], model_dir: str, device: str, download: bool) -> None:
        super().__init__()
        import easyocr
        import torch

        self._languages = tuple(languages)
        dev = resolve_device(device)
        store = os.path.join(model_dir, "easyocr")
        os.makedirs(store, exist_ok=True)
        self._reader = easyocr.Reader(
            list(self._languages), gpu=dev.startswith("cuda"), model_storage_directory=store,
            user_network_directory=os.path.join(store, "user_network"),
            download_enabled=download, verbose=False,
        )
        self.info = ModelInfo(
            name="easyocr-" + "+".join(self._languages),
            version=f"easyocr {easyocr.__version__}, torch {torch.__version__}",
            backend="easyocr", device=dev,
        )

    def read(self, rgb: np.ndarray, min_conf: float, languages: Sequence[str]):
        if languages and tuple(languages) != self._languages:
            raise ValueError(f"this container reads {list(self._languages)}, not {list(languages)}")
        h, w = rgb.shape[:2]
        with self._lock:
            raw = self._reader.readtext(to_bgr(rgb))
        out: list[Line] = []
        for pts, text, conf in raw:
            text = text.strip()
            if not text or conf < min_conf:
                continue
            xs = [float(p[0]) for p in pts]
            ys = [float(p[1]) for p in pts]
            out.append(Line(text, float(conf), Box.from_xyxy(min(xs), min(ys), max(xs), max(ys), w, h)))
        return out
