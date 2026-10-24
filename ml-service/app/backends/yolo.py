"""Ultralytics YOLO detector (COCO classes, no training).

Licence note: Ultralytics is AGPL-3.0 (enterprise licence available). Fine for research
and for a service that stays inside your own deployment; read it before distributing."""
from __future__ import annotations

import logging
import os
from typing import Sequence

import numpy as np

from ..imaging import to_bgr
from .base import Box, Det, ModelInfo, Serialised

log = logging.getLogger(__name__)


def resolve_device(device: str) -> str:
    if device != "auto":
        return device
    import torch  # imported lazily: the fake backend must work without torch

    return "cuda:0" if torch.cuda.is_available() else "cpu"


class UltralyticsDetector(Serialised):
    def __init__(self, model: str, model_dir: str, device: str, torch_threads: int = 0) -> None:
        super().__init__()
        import torch
        import ultralytics
        from ultralytics import YOLO

        if torch_threads > 0:
            torch.set_num_threads(torch_threads)
        path = model if os.path.isabs(model) or os.sep in model else os.path.join(model_dir, model)
        if not os.path.exists(path):
            # A bare name like "yolov8n.pt" is downloaded by ultralytics; containers bake
            # the weights in at build time so this branch only runs on a dev machine.
            log.warning("weights %s not found, letting ultralytics fetch %s", path, model)
            path = model
        self._device = resolve_device(device)
        self._model = YOLO(path)
        self._names: dict[int, str] = dict(self._model.names)
        self._ids = {n: i for i, n in self._names.items()}
        self.info = ModelInfo(
            name=os.path.splitext(os.path.basename(model))[0],
            version=f"ultralytics {ultralytics.__version__}, torch {torch.__version__}",
            backend="ultralytics",
            device=self._device,
            classes=tuple(self._names[i] for i in sorted(self._names)),
        )
        # Warm up so the first real frame does not pay for lazy initialisation.
        self.detect(np.zeros((64, 64, 3), np.uint8), 0.5, 1, (), 64)

    def detect(self, rgb: np.ndarray, min_conf: float, max_det: int, classes: Sequence[str], imgsz: int):
        ids = None
        if classes:
            unknown = [c for c in classes if c not in self._ids]
            if unknown:
                raise ValueError(f"unknown classes {unknown}; model has {len(self._ids)} classes")
            ids = [self._ids[c] for c in classes]
        h, w = rgb.shape[:2]
        with self._lock:
            res = self._model.predict(
                to_bgr(rgb), imgsz=imgsz, conf=min_conf, max_det=max_det, classes=ids,
                device=self._device, verbose=False,
            )[0]
        b = res.boxes
        if b is None or len(b) == 0:
            return []
        xyxy = b.xyxy.cpu().numpy()
        conf = b.conf.cpu().numpy()
        cls = b.cls.cpu().numpy().astype(int)
        return [
            Det(self._names[int(c)], int(c), float(p), Box.from_xyxy(*map(float, xy), w, h))
            for xy, p, c in zip(xyxy, conf, cls)
        ]
