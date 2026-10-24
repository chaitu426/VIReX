"""Transport-independent service logic, shared by the gRPC and HTTP front ends."""
from __future__ import annotations

import logging
import time
from dataclasses import dataclass
from typing import Optional, Sequence

import numpy as np

from .backends.base import Det, Detector, Line, ModelInfo, TextReader
from .config import Config

log = logging.getLogger(__name__)


class NotReady(RuntimeError):
    """The requested service is not hosted by this container or has not loaded yet."""


@dataclass(frozen=True)
class Detected:
    detections: list[Det]
    model: ModelInfo
    inference_ms: float


@dataclass(frozen=True)
class Read:
    lines: list[Line]
    model: ModelInfo
    inference_ms: float


class Core:
    """Holds the loaded models and applies the server-side defaults."""

    def __init__(self, cfg: Config, detector: Optional[Detector] = None, reader: Optional[TextReader] = None):
        self.cfg = cfg
        self.detector = detector
        self.reader = reader

    @classmethod
    def load(cls, cfg: Config) -> "Core":
        """Load every model this container's role needs. Slow: call once at start-up."""
        from .backends import make_detector, make_reader

        det = rdr = None
        t0 = time.perf_counter()
        if cfg.serves_detector:
            det = make_detector(cfg)
            log.info("detector ready: %s (%s, %s)", det.info.name, det.info.version, det.info.device)
        if cfg.serves_ocr:
            rdr = make_reader(cfg)
            log.info("ocr ready: %s (%s, %s)", rdr.info.name, rdr.info.version, rdr.info.device)
        log.info("models loaded in %.1fs", time.perf_counter() - t0)
        return cls(cfg, det, rdr)

    @property
    def ready(self) -> bool:
        return (not self.cfg.serves_detector or self.detector is not None) and (
            not self.cfg.serves_ocr or self.reader is not None
        )

    def detect(
        self,
        rgb: np.ndarray,
        min_conf: float = 0.0,
        max_det: int = 0,
        classes: Sequence[str] = (),
        imgsz: int = 0,
    ) -> Detected:
        if self.detector is None:
            raise NotReady("this container does not host the object detector")
        t0 = time.perf_counter()
        dets = self.detector.detect(
            rgb,
            min_conf or self.cfg.detect_conf,
            max_det or self.cfg.detect_max,
            tuple(classes),
            imgsz or self.cfg.detect_size,
        )
        return Detected(dets, self.detector.info, (time.perf_counter() - t0) * 1000)

    def read(self, rgb: np.ndarray, min_conf: float = 0.0, languages: Sequence[str] = ()) -> Read:
        if self.reader is None:
            raise NotReady("this container does not host the text reader")
        t0 = time.perf_counter()
        lines = self.reader.read(rgb, min_conf or self.cfg.ocr_conf, tuple(languages))
        return Read(lines, self.reader.info, (time.perf_counter() - t0) * 1000)

    def describe(self) -> dict:
        """What /health and /info report."""
        out: dict = {"status": "ok" if self.ready else "loading", "role": self.cfg.role}
        for key, m in (("detector", self.detector), ("ocr", self.reader)):
            if m is not None:
                i = m.info
                out[key] = {"name": i.name, "version": i.version, "backend": i.backend, "device": i.device}
        return out
