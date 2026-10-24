"""gRPC front end: ObjectDetector and TextReader, as defined in proto/ml/v1/ml.proto.

The generated stubs live in app/gen (created by scripts/gen_proto.py, never committed).
"""
from __future__ import annotations

import logging
import time
from concurrent import futures

import grpc
from grpc_health.v1 import health, health_pb2, health_pb2_grpc

from . import __version__
from .backends.base import ModelInfo
from .config import Config
from .core import Core, NotReady
from .gen import ml_pb2, ml_pb2_grpc
from .imaging import BadImage, from_encoded, from_raw_rgb

log = logging.getLogger(__name__)

DETECTOR_SERVICE = "virex.ml.v1.ObjectDetector"
TEXT_SERVICE = "virex.ml.v1.TextReader"


def decode_image(img: "ml_pb2.Image"):
    f = img.format
    if f == ml_pb2.Image.RGB24:
        return from_raw_rgb(img.data, img.width, img.height)
    if f in (ml_pb2.Image.JPEG, ml_pb2.Image.PNG):
        return from_encoded(img.data)
    raise BadImage("image.format must be RGB24, JPEG or PNG")


def _model(m: ModelInfo) -> "ml_pb2.ModelInfo":
    return ml_pb2.ModelInfo(name=m.name, version=m.version, backend=m.backend, device=m.device, classes=list(m.classes))


def _box(b) -> "ml_pb2.BBox":
    return ml_pb2.BBox(x=b.x, y=b.y, w=b.w, h=b.h)


def _guard(ctx: grpc.ServicerContext, what: str):
    """Context manager mapping our errors to gRPC status codes."""

    class _G:
        def __enter__(self):
            self.t0 = time.perf_counter()
            return self

        def __exit__(self, et, ev, tb):
            if ev is None:
                return False
            if isinstance(ev, (BadImage, ValueError)):
                ctx.abort(grpc.StatusCode.INVALID_ARGUMENT, str(ev))
            if isinstance(ev, NotReady):
                ctx.abort(grpc.StatusCode.UNAVAILABLE, str(ev))
            log.exception("%s failed", what)
            ctx.abort(grpc.StatusCode.INTERNAL, f"{what} failed: {type(ev).__name__}: {ev}")
            return True

        def ms(self) -> float:
            return (time.perf_counter() - self.t0) * 1000

    return _G()


class DetectorServicer(ml_pb2_grpc.ObjectDetectorServicer):
    def __init__(self, core: Core):
        self.core = core

    def Info(self, request, context):
        if self.core.detector is None:
            return ml_pb2.InfoResponse(ready=False, service_version=__version__)
        return ml_pb2.InfoResponse(model=_model(self.core.detector.info), service_version=__version__, ready=True)

    def Detect(self, request, context):
        with _guard(context, "detect") as g:
            rgb = decode_image(request.image)
            r = self.core.detect(rgb, request.min_confidence, request.max_detections, request.classes, request.inference_size)
            return ml_pb2.DetectResponse(
                detections=[
                    ml_pb2.Detection(class_name=d.class_name, class_id=d.class_id, confidence=d.confidence, box=_box(d.box))
                    for d in r.detections
                ],
                model=_model(r.model), inference_ms=r.inference_ms, total_ms=g.ms(),
                request_id=request.request_id, image_width=rgb.shape[1], image_height=rgb.shape[0],
            )


class TextServicer(ml_pb2_grpc.TextReaderServicer):
    def __init__(self, core: Core):
        self.core = core

    def Info(self, request, context):
        if self.core.reader is None:
            return ml_pb2.InfoResponse(ready=False, service_version=__version__)
        return ml_pb2.InfoResponse(model=_model(self.core.reader.info), service_version=__version__, ready=True)

    def Read(self, request, context):
        with _guard(context, "ocr") as g:
            rgb = decode_image(request.image)
            r = self.core.read(rgb, request.min_confidence, request.languages)
            return ml_pb2.ReadResponse(
                lines=[ml_pb2.TextLine(text=l.text, confidence=l.confidence, box=_box(l.box)) for l in r.lines],
                model=_model(r.model), inference_ms=r.inference_ms, total_ms=g.ms(),
                request_id=request.request_id, image_width=rgb.shape[1], image_height=rgb.shape[0],
            )


def build_server(core: Core, cfg: Config) -> tuple[grpc.Server, int]:
    """Create (but do not start) the gRPC server. Returns it and the bound port."""
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=cfg.grpc_workers, thread_name_prefix="grpc"),
        options=[
            ("grpc.max_receive_message_length", cfg.max_message_bytes),
            ("grpc.max_send_message_length", cfg.max_message_bytes),
        ],
    )
    hs = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(hs, server)
    hs.set("", health_pb2.HealthCheckResponse.SERVING)
    if cfg.serves_detector:
        ml_pb2_grpc.add_ObjectDetectorServicer_to_server(DetectorServicer(core), server)
        hs.set(DETECTOR_SERVICE, health_pb2.HealthCheckResponse.SERVING)
    if cfg.serves_ocr:
        ml_pb2_grpc.add_TextReaderServicer_to_server(TextServicer(core), server)
        hs.set(TEXT_SERVICE, health_pb2.HealthCheckResponse.SERVING)
    port = server.add_insecure_port(f"[::]:{cfg.grpc_port}")
    if port == 0:
        raise RuntimeError(f"cannot bind gRPC port {cfg.grpc_port}")
    return server, port
