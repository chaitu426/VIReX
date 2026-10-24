"""HTTP front end: /health for orchestrators, /detect and /ocr for debugging with curl.

The Go core talks gRPC; this API exists so a person can try the models by hand:

    curl -s -X POST --data-binary @frame.jpg -H 'Content-Type: image/jpeg' localhost:8000/detect
"""
from __future__ import annotations

from typing import Optional

from fastapi import FastAPI, HTTPException, Query, Request, Response

from . import __version__
from .core import Core, NotReady
from .imaging import BadImage, from_encoded


def _box(b) -> dict:
    return {"x": b.x, "y": b.y, "w": b.w, "h": b.h}


def create_app(core: Optional[Core] = None) -> FastAPI:
    """Build the app. With core=None it starts empty and reports "loading" (used by
    `uvicorn app.main:app` before any model is attached)."""
    app = FastAPI(title="VIReX ML service", version=__version__)
    holder = {"core": core}

    def need() -> Core:
        c = holder["core"]
        if c is None:
            raise HTTPException(503, "models are not loaded")
        return c

    @app.get("/health")
    def health(response: Response):
        c = holder["core"]
        if c is None or not c.ready:
            response.status_code = 503
            return {"status": "loading"}
        return {**c.describe(), "service_version": __version__}

    @app.get("/info")
    def info():
        return {**need().describe(), "service_version": __version__}

    async def image_of(request: Request):
        try:
            return from_encoded(await request.body())
        except BadImage as e:
            raise HTTPException(400, str(e))

    @app.post("/detect")
    async def detect(
        request: Request,
        min_confidence: float = Query(0.0, ge=0, le=1),
        max_detections: int = Query(0, ge=0, le=1000),
        classes: str = Query("", description="comma separated class names"),
    ):
        rgb = await image_of(request)
        try:
            r = need().detect(rgb, min_confidence, max_detections, [c for c in classes.split(",") if c])
        except NotReady as e:
            raise HTTPException(503, str(e))
        except ValueError as e:
            raise HTTPException(400, str(e))
        return {
            "image": {"width": rgb.shape[1], "height": rgb.shape[0]},
            "model": r.model.name, "version": r.model.version, "inference_ms": round(r.inference_ms, 2),
            "detections": [
                {"class": d.class_name, "class_id": d.class_id, "confidence": round(d.confidence, 4), "box": _box(d.box)}
                for d in r.detections
            ],
        }

    @app.post("/ocr")
    async def ocr(request: Request, min_confidence: float = Query(0.0, ge=0, le=1)):
        rgb = await image_of(request)
        try:
            r = need().read(rgb, min_confidence)
        except NotReady as e:
            raise HTTPException(503, str(e))
        except ValueError as e:
            raise HTTPException(400, str(e))
        return {
            "image": {"width": rgb.shape[1], "height": rgb.shape[0]},
            "model": r.model.name, "version": r.model.version, "inference_ms": round(r.inference_ms, 2),
            "lines": [{"text": l.text, "confidence": round(l.confidence, 4), "box": _box(l.box)} for l in r.lines],
        }

    app.state.holder = holder
    return app
