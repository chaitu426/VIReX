"""Process entry point.

    python -m app.main            run the gRPC server (+ HTTP API) from environment settings
    uvicorn app.main:app          HTTP API only, for quick local poking (no models attached)
"""
from __future__ import annotations

import logging
import signal
import sys
import threading

from .config import Config
from .http_app import create_app

# `uvicorn app.main:app` keeps working from Phase 1; it just has no models attached.
app = create_app(None)


def _serve_http(core, port: int) -> None:
    import uvicorn

    server = uvicorn.Server(uvicorn.Config(create_app(core), host="0.0.0.0", port=port, log_level="warning"))
    threading.Thread(target=server.run, name="http", daemon=True).start()


def main() -> int:
    cfg = Config.from_env()
    logging.basicConfig(level=cfg.log_level, format="%(asctime)s %(levelname)s %(name)s: %(message)s", stream=sys.stdout)
    log = logging.getLogger("virex.ml")
    log.info("starting role=%s grpc=%d http=%d", cfg.role, cfg.grpc_port, cfg.http_port)

    from .core import Core
    from .grpc_server import build_server

    core = Core.load(cfg)  # blocks until every model is warm; the health check passes after this
    grpc_server, port = build_server(core, cfg)
    grpc_server.start()
    if cfg.http_port:
        _serve_http(core, cfg.http_port)
    log.info("ready: gRPC on :%d", port)

    stop = threading.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: stop.set())
    stop.wait()
    log.info("shutting down")
    grpc_server.stop(grace=5).wait()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
