"""Backend registry. Imports are lazy so a container only needs the libraries of the
backends it actually runs."""
from __future__ import annotations

from ..config import Config
from .base import Detector, TextReader


def make_detector(cfg: Config) -> Detector:
    if cfg.detector_backend == "fake":
        from .fake import FakeDetector

        return FakeDetector()
    if cfg.detector_backend == "ultralytics":
        from .yolo import UltralyticsDetector

        return UltralyticsDetector(cfg.detector_model, cfg.model_dir, cfg.device, cfg.torch_threads)
    raise ValueError(f"unknown VIREX_DETECTOR_BACKEND {cfg.detector_backend!r} (want ultralytics or fake)")


def make_reader(cfg: Config) -> TextReader:
    if cfg.ocr_backend == "fake":
        from .fake import FakeTextReader

        return FakeTextReader()
    if cfg.ocr_backend == "easyocr":
        from .ocr_easyocr import EasyOCRReader

        return EasyOCRReader(cfg.ocr_languages, cfg.model_dir, cfg.device, cfg.ocr_download)
    if cfg.ocr_backend == "paddleocr":
        from .ocr_paddle import PaddleOCRReader

        return PaddleOCRReader(cfg.ocr_languages, cfg.device)
    raise ValueError(f"unknown VIREX_OCR_BACKEND {cfg.ocr_backend!r} (want easyocr, paddleocr or fake)")
