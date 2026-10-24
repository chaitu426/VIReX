"""PaddleOCR text reader (optional: build the image with INSTALL_PADDLE=1).

Written against the PaddleOCR 3.x API (`predict`), with a fallback for the 2.x `ocr`
call. The two disagree on result shape, and this backend could not be run when it was
written, so test it before trusting it."""
from __future__ import annotations

import logging
from typing import Sequence

import numpy as np

from ..imaging import to_bgr
from .base import Box, Line, ModelInfo, Serialised

log = logging.getLogger(__name__)


class PaddleOCRReader(Serialised):
    def __init__(self, languages: Sequence[str], device: str) -> None:
        super().__init__()
        import paddleocr

        self._lang = languages[0] if languages else "en"
        try:  # 3.x
            self._ocr = paddleocr.PaddleOCR(
                lang=self._lang, use_doc_orientation_classify=False, use_doc_unwarping=False,
                use_textline_orientation=False, device="gpu" if device.startswith("cuda") else "cpu",
            )
            self._v3 = True
        except TypeError:  # 2.x
            self._ocr = paddleocr.PaddleOCR(lang=self._lang, use_angle_cls=False, show_log=False,
                                            use_gpu=device.startswith("cuda"))
            self._v3 = False
        self.info = ModelInfo(f"paddleocr-{self._lang}", f"paddleocr {paddleocr.__version__}", "paddleocr", device)

    def read(self, rgb: np.ndarray, min_conf: float, languages: Sequence[str]):
        h, w = rgb.shape[:2]
        bgr = to_bgr(rgb)
        found: list[tuple[list, str, float]] = []
        with self._lock:
            if self._v3:
                for page in self._ocr.predict(bgr):
                    polys = page["rec_polys"] if "rec_polys" in page else page["dt_polys"]
                    for poly, text, score in zip(polys, page["rec_texts"], page["rec_scores"]):
                        found.append((np.asarray(poly).tolist(), text, float(score)))
            else:
                for page in self._ocr.ocr(bgr, cls=False) or []:
                    for poly, (text, score) in page or []:
                        found.append((poly, text, float(score)))
        out: list[Line] = []
        for poly, text, score in found:
            text = text.strip()
            if not text or score < min_conf:
                continue
            xs = [float(p[0]) for p in poly]
            ys = [float(p[1]) for p in poly]
            out.append(Line(text, score, Box.from_xyxy(min(xs), min(ys), max(xs), max(ys), w, h)))
        return out
