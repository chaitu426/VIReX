"""Turn a wire Image (or raw HTTP bytes) into an RGB numpy array."""
from __future__ import annotations

import io

import numpy as np
from PIL import Image as PILImage

# Refuse absurd frames before allocating anything: 16384x16384 RGB is already 800 MB.
MAX_SIDE = 16384


class BadImage(ValueError):
    """The caller sent an image we cannot use (maps to gRPC INVALID_ARGUMENT)."""


def from_raw_rgb(data: bytes, width: int, height: int) -> np.ndarray:
    if width <= 0 or height <= 0 or width > MAX_SIDE or height > MAX_SIDE:
        raise BadImage(f"bad image size {width}x{height}")
    want = width * height * 3
    if len(data) != want:
        raise BadImage(f"RGB24 {width}x{height} needs {want} bytes, got {len(data)}")
    # frombuffer is read-only and zero-copy; backends that need to write copy themselves.
    return np.frombuffer(data, dtype=np.uint8).reshape(height, width, 3)


def from_encoded(data: bytes) -> np.ndarray:
    if not data:
        raise BadImage("empty image data")
    try:
        with PILImage.open(io.BytesIO(data)) as im:
            im.load()
            if im.width > MAX_SIDE or im.height > MAX_SIDE:
                raise BadImage(f"bad image size {im.width}x{im.height}")
            return np.asarray(im.convert("RGB"))
    except BadImage:
        raise
    except Exception as e:  # PIL raises many types for corrupt data
        raise BadImage(f"cannot decode image: {e}") from e


def to_bgr(rgb: np.ndarray) -> np.ndarray:
    """OpenCV-style channel order; ultralytics and easyocr treat ndarrays as BGR."""
    return np.ascontiguousarray(rgb[:, :, ::-1])
