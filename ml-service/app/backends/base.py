"""The two interfaces a model backend implements. Nothing else in the service knows
which library is underneath."""
from __future__ import annotations

import threading
from dataclasses import dataclass
from typing import Protocol, Sequence

import numpy as np


@dataclass(frozen=True)
class ModelInfo:
    name: str
    version: str
    backend: str
    device: str
    classes: tuple[str, ...] = ()


@dataclass(frozen=True)
class Box:
    """Normalised 0..1, origin top-left; x/y is the top-left corner."""

    x: float
    y: float
    w: float
    h: float

    @staticmethod
    def from_xyxy(x1: float, y1: float, x2: float, y2: float, width: int, height: int) -> "Box":
        """Pixel corners -> clamped normalised box."""
        x1, x2 = sorted((min(max(x1, 0.0), width), min(max(x2, 0.0), width)))
        y1, y2 = sorted((min(max(y1, 0.0), height), min(max(y2, 0.0), height)))
        return Box(x1 / width, y1 / height, (x2 - x1) / width, (y2 - y1) / height)


@dataclass(frozen=True)
class Det:
    class_name: str
    class_id: int
    confidence: float
    box: Box


@dataclass(frozen=True)
class Line:
    text: str
    confidence: float
    box: Box


class Detector(Protocol):
    info: ModelInfo

    def detect(
        self,
        rgb: np.ndarray,
        min_conf: float,
        max_det: int,
        classes: Sequence[str],
        imgsz: int,
    ) -> list[Det]: ...


class TextReader(Protocol):
    info: ModelInfo

    def read(self, rgb: np.ndarray, min_conf: float, languages: Sequence[str]) -> list[Line]: ...


class Serialised:
    """Mixin: model objects are not generally thread safe, so inference is one at a time.
    The gRPC thread pool still overlaps network transfer and image decoding."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
