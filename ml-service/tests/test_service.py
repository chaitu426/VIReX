"""Service tests. They use the fake backends, so they need no torch and no weights."""
import grpc
import pytest
from fastapi.testclient import TestClient

from app.config import Config
from app.core import Core
from app.grpc_server import build_server
from app.http_app import create_app
from app.imaging import BadImage, from_encoded, from_raw_rgb

from .helpers import BLUE, GREEN, RED, frame, jpeg, png


@pytest.fixture(scope="module")
def cfg():
    return Config(role="all", grpc_port=0, http_port=0, detector_backend="fake", ocr_backend="fake")


@pytest.fixture(scope="module")
def core(cfg):
    return Core.load(cfg)


@pytest.fixture(scope="module")
def channel(core, cfg):
    server, port = build_server(core, cfg)
    server.start()
    with grpc.insecure_channel(f"127.0.0.1:{port}") as ch:
        yield ch
    server.stop(0)


# ---- imaging ----

def test_raw_rgb_roundtrip():
    f = frame(40, 30)
    out = from_raw_rgb(f.tobytes(), 40, 30)
    assert out.shape == (30, 40, 3) and (out == f).all()


@pytest.mark.parametrize("w,h,n", [(40, 30, 10), (0, 30, 0), (40, 0, 0), (100000, 10, 10)])
def test_raw_rgb_rejects_bad_sizes(w, h, n):
    with pytest.raises(BadImage):
        from_raw_rgb(b"\0" * n, w, h)


def test_encoded_decodes_jpeg_and_png_and_rejects_junk():
    f = frame(64, 48, [(RED, 10, 10, 30, 30)])
    assert from_encoded(png(f)).shape == (48, 64, 3)
    assert from_encoded(jpeg(f)).shape == (48, 64, 3)
    with pytest.raises(BadImage):
        from_encoded(b"not an image")
    with pytest.raises(BadImage):
        from_encoded(b"")


# ---- core ----

def test_core_defaults_and_describe(core):
    d = core.describe()
    assert d["status"] == "ok" and d["detector"]["backend"] == "fake" and d["ocr"]["backend"] == "fake"


def test_detector_only_container_refuses_ocr():
    c = Core.load(Config(role="detector", detector_backend="fake"))
    from app.core import NotReady
    with pytest.raises(NotReady):
        c.read(frame())


# ---- gRPC ----

def _stubs():
    from app.gen import ml_pb2, ml_pb2_grpc
    return ml_pb2, ml_pb2_grpc


def test_grpc_detect_raw_rgb(channel):
    pb, rpc = _stubs()
    f = frame(320, 180, [(RED, 32, 18, 96, 90), (BLUE, 200, 100, 260, 160)])
    stub = rpc.ObjectDetectorStub(channel)
    r = stub.Detect(pb.DetectRequest(image=pb.Image(format=pb.Image.RGB24, width=320, height=180, data=f.tobytes()),
                                     request_id="r1"))
    assert r.request_id == "r1" and (r.image_width, r.image_height) == (320, 180)
    by = {d.class_name: d for d in r.detections}
    assert set(by) == {"person", "cell phone"}
    p = by["person"].box
    assert abs(p.x - 0.1) < 0.03 and abs(p.y - 0.1) < 0.05 and abs(p.w - 0.2) < 0.04 and abs(p.h - 0.4) < 0.06
    assert r.model.backend == "fake" and r.inference_ms >= 0 and r.total_ms >= r.inference_ms


def test_grpc_detect_class_filter_and_jpeg(channel):
    pb, rpc = _stubs()
    f = frame(320, 180, [(RED, 32, 18, 96, 90), (BLUE, 200, 100, 260, 160)])
    stub = rpc.ObjectDetectorStub(channel)
    r = stub.Detect(pb.DetectRequest(image=pb.Image(format=pb.Image.JPEG, data=jpeg(f)), classes=["cell phone"]))
    assert [d.class_name for d in r.detections] == ["cell phone"]


def test_grpc_detect_bad_image_is_invalid_argument(channel):
    pb, rpc = _stubs()
    stub = rpc.ObjectDetectorStub(channel)
    with pytest.raises(grpc.RpcError) as e:
        stub.Detect(pb.DetectRequest(image=pb.Image(format=pb.Image.RGB24, width=10, height=10, data=b"short")))
    assert e.value.code() == grpc.StatusCode.INVALID_ARGUMENT
    with pytest.raises(grpc.RpcError) as e:
        stub.Detect(pb.DetectRequest(image=pb.Image(data=b"x")))  # format unspecified
    assert e.value.code() == grpc.StatusCode.INVALID_ARGUMENT


def test_grpc_read_and_info(channel):
    pb, rpc = _stubs()
    f = frame(320, 180, [(GREEN, 40, 20, 200, 50)])
    stub = rpc.TextReaderStub(channel)
    r = stub.Read(pb.ReadRequest(image=pb.Image(format=pb.Image.RGB24, width=320, height=180, data=f.tobytes())))
    assert [l.text for l in r.lines] == ["FAKE TEXT"]
    assert 0 < r.lines[0].box.w < 1
    info = stub.Info(pb.InfoRequest())
    assert info.ready and info.model.backend == "fake"


def test_grpc_unhosted_service_is_unimplemented():
    cfg = Config(role="detector", grpc_port=0, http_port=0, detector_backend="fake")
    server, port = build_server(Core.load(cfg), cfg)
    server.start()
    try:
        pb, rpc = _stubs()
        with grpc.insecure_channel(f"127.0.0.1:{port}") as ch:
            with pytest.raises(grpc.RpcError) as e:
                rpc.TextReaderStub(ch).Info(pb.InfoRequest())
            assert e.value.code() == grpc.StatusCode.UNIMPLEMENTED
    finally:
        server.stop(0)


def test_grpc_health(channel):
    from grpc_health.v1 import health_pb2, health_pb2_grpc
    stub = health_pb2_grpc.HealthStub(channel)
    for name in ("", "virex.ml.v1.ObjectDetector", "virex.ml.v1.TextReader"):
        assert stub.Check(health_pb2.HealthCheckRequest(service=name)).status == health_pb2.HealthCheckResponse.SERVING


# ---- HTTP ----

def test_http_health_detect_ocr(core):
    c = TestClient(create_app(core))
    h = c.get("/health")
    assert h.status_code == 200 and h.json()["status"] == "ok"
    f = frame(320, 180, [(RED, 32, 18, 96, 90), (GREEN, 150, 20, 300, 50)])
    r = c.post("/detect", content=png(f), headers={"Content-Type": "image/png"})
    assert r.status_code == 200 and [d["class"] for d in r.json()["detections"]] == ["person"]
    r = c.post("/ocr", content=png(f))
    assert r.json()["lines"][0]["text"] == "FAKE TEXT"
    assert c.post("/detect", content=b"junk").status_code == 400


def test_http_without_models_reports_loading():
    c = TestClient(create_app(None))
    assert c.get("/health").status_code == 503
    assert c.post("/detect", content=b"x").status_code in (400, 503)


# ---- config ----

def test_config_validation(monkeypatch):
    monkeypatch.setenv("VIREX_ROLE", "nonsense")
    with pytest.raises(ValueError):
        Config.from_env()
    monkeypatch.setenv("VIREX_ROLE", "ocr")
    monkeypatch.setenv("VIREX_GRPC_PORT", "50099")
    monkeypatch.setenv("VIREX_OCR_LANGS", "en, de")
    c = Config.from_env()
    assert c.serves_ocr and not c.serves_detector and c.grpc_port == 50099 and c.ocr_languages == ("en", "de")
