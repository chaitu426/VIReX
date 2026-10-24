"""Service configuration, read once from environment variables.

Everything that decides *which model runs* lives here, so swapping a model is a
container-environment change and never a Go change.
"""
from __future__ import annotations

import os
from dataclasses import dataclass


def _env(name: str, default: str) -> str:
    v = os.environ.get(name)
    return default if v is None or v == "" else v


def _env_int(name: str, default: int) -> int:
    try:
        return int(_env(name, str(default)))
    except ValueError as e:
        raise ValueError(f"{name} must be an integer") from e


def _env_float(name: str, default: float) -> float:
    try:
        return float(_env(name, str(default)))
    except ValueError as e:
        raise ValueError(f"{name} must be a number") from e


ROLES = ("detector", "ocr", "all")


@dataclass(frozen=True)
class Config:
    # Which services this container hosts. One image, several containers (see compose).
    role: str = "all"
    grpc_port: int = 50051
    http_port: int = 8000  # 0 turns the HTTP API off
    grpc_workers: int = 4
    max_message_mb: int = 64

    model_dir: str = "/models"
    device: str = "cpu"  # cpu | cuda | cuda:0 | auto
    torch_threads: int = 0  # 0 = library default

    detector_backend: str = "ultralytics"  # ultralytics | fake
    detector_model: str = "yolov8n.pt"
    detect_conf: float = 0.35
    detect_max: int = 100
    detect_size: int = 640

    ocr_backend: str = "easyocr"  # easyocr | paddleocr | fake
    ocr_languages: tuple[str, ...] = ("en",)
    ocr_conf: float = 0.4
    ocr_download: bool = False  # allow downloading weights at runtime (off in containers)

    log_level: str = "INFO"

    @property
    def serves_detector(self) -> bool:
        return self.role in ("detector", "all")

    @property
    def serves_ocr(self) -> bool:
        return self.role in ("ocr", "all")

    @property
    def max_message_bytes(self) -> int:
        return self.max_message_mb * 1024 * 1024

    def validate(self) -> "Config":
        if self.role not in ROLES:
            raise ValueError(f"VIREX_ROLE must be one of {ROLES}, got {self.role!r}")
        if not 0 <= self.grpc_port <= 65535 or not 0 <= self.http_port <= 65535:
            raise ValueError("ports must be 0..65535")
        if self.grpc_workers < 1:
            raise ValueError("VIREX_GRPC_WORKERS must be >= 1")
        if not 0 <= self.detect_conf <= 1 or not 0 <= self.ocr_conf <= 1:
            raise ValueError("confidence thresholds must be within 0..1")
        return self

    @classmethod
    def from_env(cls) -> "Config":
        langs = tuple(s.strip() for s in _env("VIREX_OCR_LANGS", "en").split(",") if s.strip())
        return cls(
            role=_env("VIREX_ROLE", "all"),
            grpc_port=_env_int("VIREX_GRPC_PORT", 50051),
            http_port=_env_int("VIREX_HTTP_PORT", 8000),
            grpc_workers=_env_int("VIREX_GRPC_WORKERS", 4),
            max_message_mb=_env_int("VIREX_MAX_MESSAGE_MB", 64),
            model_dir=_env("VIREX_MODEL_DIR", "/models"),
            device=_env("VIREX_DEVICE", "cpu"),
            torch_threads=_env_int("VIREX_TORCH_THREADS", 0),
            detector_backend=_env("VIREX_DETECTOR_BACKEND", "ultralytics"),
            detector_model=_env("VIREX_DETECTOR_MODEL", "yolov8n.pt"),
            detect_conf=_env_float("VIREX_DETECT_CONF", 0.35),
            detect_max=_env_int("VIREX_DETECT_MAX", 100),
            detect_size=_env_int("VIREX_DETECT_SIZE", 640),
            ocr_backend=_env("VIREX_OCR_BACKEND", "easyocr"),
            ocr_languages=langs or ("en",),
            ocr_conf=_env_float("VIREX_OCR_CONF", 0.4),
            ocr_download=_env("VIREX_OCR_DOWNLOAD", "0") in ("1", "true", "yes"),
            log_level=_env("VIREX_LOG_LEVEL", "INFO").upper(),
        ).validate()
