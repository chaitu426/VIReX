# VIReX ML service image (object detector + OCR).
#
# Build context is the REPOSITORY ROOT, because the gRPC contract lives in /proto and is
# shared with the Go code:
#
#   docker build -f ml-service/Dockerfile -t virex-ml .
#
# Build args:
#   TORCH_INDEX   wheel index for torch. Default is the CPU-only index (small image).
#                 For NVIDIA GPUs use https://download.pytorch.org/whl/cu124 (see compose gpu profile).
#   INSTALL_PADDLE=1  also install PaddleOCR (optional backend, off by default).
#   DETECTOR_MODEL    YOLO weights baked into the image (default yolov8n.pt).
#   OCR_LANGS         comma separated EasyOCR languages baked into the image (default en).
#   BAKE_MODELS=0     skip downloading weights at build time (offline build; mount /models instead).
#
# One image serves every role: VIREX_ROLE=detector | ocr | all  (see docker-compose.yml).

ARG PYTHON_VERSION=3.11
FROM python:${PYTHON_VERSION}-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    VIREX_MODEL_DIR=/models \
    YOLO_CONFIG_DIR=/models/ultralytics

# libgomp1: OpenMP runtime used by torch. libglib2.0-0: needed by opencv.
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /srv

# ---- dependencies (cached until a requirements file changes) ----
COPY ml-service/requirements.txt ml-service/requirements-models.txt ./
ARG TORCH_INDEX=https://download.pytorch.org/whl/cpu
ARG INSTALL_PADDLE=0
RUN pip install -r requirements.txt grpcio-tools \
    && pip install torch torchvision --index-url ${TORCH_INDEX} \
    && pip install -r requirements-models.txt \
    && if [ "${INSTALL_PADDLE}" = "1" ]; then pip install paddlepaddle paddleocr; fi

# ---- generate the gRPC stubs from the shared contract ----
COPY proto/ml/v1/ml.proto proto/ml/v1/ml.proto
COPY ml-service/scripts ml-service/scripts
COPY ml-service/app ml-service/app
RUN python ml-service/scripts/gen_proto.py && pip uninstall -y grpcio-tools

# ---- bake the model weights in, so containers start offline and reproducibly ----
ARG BAKE_MODELS=1
ARG DETECTOR_MODEL=yolov8n.pt
ARG OCR_LANGS=en
RUN mkdir -p /models/ultralytics /models/easyocr \
    && if [ "${BAKE_MODELS}" = "1" ]; then \
         cd /models \
         && python -c "from ultralytics import YOLO; YOLO('${DETECTOR_MODEL}')" \
         && test -f "/models/${DETECTOR_MODEL}" \
         && python -c "import easyocr; easyocr.Reader('${OCR_LANGS}'.split(','), gpu=False, model_storage_directory='/models/easyocr', user_network_directory='/models/easyocr/user_network', verbose=False)"; \
       fi

# ---- run as an unprivileged user ----
RUN useradd --system --uid 10001 --home /srv virex \
    && chown -R virex:virex /models /srv
USER virex

WORKDIR /srv/ml-service
ENV PYTHONPATH=/srv/ml-service \
    VIREX_GRPC_PORT=50051 \
    VIREX_HTTP_PORT=8000
EXPOSE 50051 8000

# Models load before the port opens, so "healthy" really means "ready for frames".
HEALTHCHECK --interval=15s --timeout=5s --start-period=120s --retries=5 \
    CMD python -c "import os,urllib.request as u; u.urlopen('http://127.0.0.1:%s/health' % os.environ.get('VIREX_HTTP_PORT','8000'), timeout=4)"

ENTRYPOINT ["python", "-m", "app.main"]
