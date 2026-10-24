# Phase 2 test guide: run it step by step on another machine

Work top to bottom. Each step says what to run, what you should see, and what to do if you don't. **Stop at the first failing step** and send me the command and its full output.

**Already verified on the dev machine (Windows, no Docker):** Go tests, Python tests with fake backends, and the real Go encoder against the real Python gRPC server with fake backends.
**Never run before, so most likely to need fixes:** the Docker image build, the real YOLO and EasyOCR models, real footage. Those are steps 5 to 8.

Commands use `bash` syntax (Git Bash, Linux, macOS). PowerShell notes are given where they differ. Run everything from the repository root unless a step says otherwise.

---

## Step 0: Prerequisites

| Tool | Needed for | Check |
|---|---|---|
| Docker with Compose v2 | steps 4 to 8 | `docker --version` and `docker compose version` |
| Go 1.24 or newer | steps 2 to 8 | `go version` |
| FFmpeg and ffprobe on PATH, with libx264 | steps 2 to 8 | `ffmpeg -version` and `ffprobe -version` |
| Python 3.11 or newer | step 1 only | `python --version` |
| About 6 GB free disk, internet | image build | first build downloads about 2 GB |

Get the code:
```bash
git clone <your repo url> clproject && cd clproject
git checkout <the branch that has Phase 2>
```
Pass: all five tools print a version.

---

## Step 1: Python service tests (no Docker, no models)

```bash
cd ml-service
python -m venv .venv
source .venv/bin/activate            # PowerShell: .venv\Scripts\Activate.ps1
pip install -r requirements-dev.txt
pytest tests -q
cd ..
```
**Pass:** `17 passed`.
**If `pip install` times out:** the network blocks PyPI; retry with `pip install --default-timeout=120 --retries 5 -r requirements-dev.txt`.
**If a test fails:** send me the output. This suite uses fake backends, so a failure here is a real bug in the service.

---

## Step 2: Go build and tests (no Docker, no models)

```bash
go build -o bin/ ./cmd/...
go test -count=1 ./...
```
**Pass:** every package prints `ok` (packages with no tests print `no test files`); the whole run takes 1 to 2 minutes.
This includes the end-to-end test that encodes a generated ffmpeg clip through real gRPC against an in-process fake ML server.
**If tests are skipped or fail on ffmpeg:** ffmpeg is not on PATH or lacks libx264.

PowerShell users: `go build -o bin/ ./cmd/...` makes `bin\virex-encode.exe`; use that name in later steps.

---

## Step 3: Make a small test clip for the fake models

The fake models only find solid red (person), blue (cell phone) and green (text) rectangles, so they need this synthetic clip. A red box walks in from the left edge at t=3, stops at t=7, vanishes at t=10; green "text" is on screen until t=8.

```bash
mkdir -p tests/testdata/clips
ffmpeg -v error -y -filter_complex "color=c=white:s=320x180:r=10:d=14[bg];color=c=0xE61414:s=40x80:r=10:d=14[p];color=c=0x14E614:s=120x20:r=10:d=14[tx];[bg][p]overlay=x='if(lt(t,7),(t-3)*30,120)':y=50:enable='between(t,3,10)'[b2];[b2][tx]overlay=x=180:y=20:enable='between(t,0,8)'[v]" -map "[v]" -t 14 -c:v libx264 -crf 12 -pix_fmt yuv420p tests/testdata/clips/fake_demo.mp4
```
**Pass:** the file `tests/testdata/clips/fake_demo.mp4` exists (about 10 KB). Clips are git-ignored on purpose.

---

## Step 4: Plumbing check with fake models in Docker

This proves ports, the gRPC contract and the Go client across a real container boundary. It builds the image but loads **no** real models (the model download happens at build time, so this step already needs step 5's build to succeed; if you only want to test the plumbing first, use the no-Docker alternative below).

```bash
docker compose -f docker-compose.yml -f docker-compose.fake.yml up -d --build
docker compose ps
```
**Pass:** both `detector` and `ocr` show `healthy` after about a minute.

```bash
curl -s localhost:8001/health
curl -s localhost:8002/health
```
**Pass:** JSON with `"status":"ok"`, `"role":"detector"` / `"ocr"`, and backend `fake`.

Encode the fake clip:
```bash
bin/virex-encode tests/testdata/clips/fake_demo.mp4 -o fake_demo.virex -semantic ml -cost-log cost.jsonl
```
**Pass, expected output (numbers may differ slightly):**
```text
  sampled    14 frames (scene_change)
  model      detector: fake-colour-detector (1, cpu)
  model      ocr: fake-colour-ocr (1, cpu)
  semantic   1 entities, 3 events, 1 actions, 0 relations, 1 text blocks, 1 shots
  ml calls   14 detect, 8 ocr (6 reused), ...
```
Inspect the result:
```bash
bin/virex-decode fake_demo.virex -svir-json - | grep -E '"type"|"content"'
```
**Pass:** you see `person`, `person_entered`, `person_disappeared`, `text_appeared` and `FAKE TEXT`.

Clean up:
```bash
docker compose -f docker-compose.yml -f docker-compose.fake.yml down
```

**No-Docker alternative for this step** (tests the contract without containers):
```bash
cd ml-service && python scripts/gen_proto.py
VIREX_ROLE=all VIREX_GRPC_PORT=50061 VIREX_HTTP_PORT=0 VIREX_DETECTOR_BACKEND=fake VIREX_OCR_BACKEND=fake python -m app.main &
cd ..
bin/virex-encode tests/testdata/clips/fake_demo.mp4 -o fake_demo.virex -semantic ml -ml-detect 127.0.0.1:50061 -ml-ocr 127.0.0.1:50061
```
(PowerShell: set the variables with `$env:VIREX_ROLE="all"` etc. and start Python with `Start-Process`.)

**Fail table**
| Symptom | Cause / fix |
|---|---|
| build fails at `pip install` | network or resolver conflict: send me the log; the fix is pinning versions in `ml-service/requirements*.txt` |
| build fails at `test -f /models/yolov8n.pt` | Ultralytics saved the weights elsewhere (I could not verify the path). Send me `docker compose build` output |
| `unhealthy` or restarting | `docker compose logs detector` and send me the last 30 lines |
| encoder says "unreachable… try docker compose ps" | containers not healthy yet, or ports 50051/50052 are taken by something else |
| encoder says "does not host this service… VIREX_ROLE" | the two addresses are swapped |

---

## Step 5: Real models: build and start

```bash
docker compose down
docker compose build
docker compose up -d
docker compose ps
```
**Pass:** after 1 to 3 minutes (models load before the port opens) both containers are `healthy`.
**Check the loaded models:**
```bash
curl -s localhost:8001/health
curl -s localhost:8002/health
```
Expect the detector to report backend `ultralytics` (`yolov8n`) and OCR `easyocr`, device `cpu`.

---

## Step 6: Try the real models by hand on one picture

Use any photo with a person (and one with printed text for OCR). Save as `frame.jpg`.
```bash
curl -s -X POST --data-binary @frame.jpg -H "Content-Type: image/jpeg" localhost:8001/detect
curl -s -X POST --data-binary @frame.jpg -H "Content-Type: image/jpeg" localhost:8002/ocr
```
**Pass:**
- `/detect` returns detections, e.g. `"class":"person"` with a confidence and a `box` whose x, y, w, h are between 0 and 1. Note the `inference_ms`.
- `/ocr` returns `lines` with the text you can see. **Note whether the text is read correctly**: this is the check for the open question about EasyOCR colour order. If the text is wrong or empty on a clearly readable image, tell me.

Write down `inference_ms` for both. They answer "how fast is it on this CPU".

---

## Step 7: Encode a real video

Put a short clip (1 to 3 minutes; a lecture or a clip with a person entering and leaving is best) at `tests/testdata/clips/real.mp4`.
```bash
time bin/virex-encode tests/testdata/clips/real.mp4 -o real.virex -config configs/phase2.json -cost-log cost.jsonl
```
**Pass:** it finishes and prints models, entities, events, relations and `ml calls` counts. A person should be a **small number** of entities, not hundreds.
If it seems stuck, it is probably OCR on CPU. Check in another terminal: `docker compose logs -f ocr`. To skip OCR for a first test, add `-ml-no-ocr`.

Look at the result:
```bash
bin/virex-decode real.virex -info
bin/virex-decode real.virex -svir-json real.json
```
Open `real.json` and check, in this order:
1. `entities`: do people and objects look right? Is one real person split into several `person` entities (a tracking failure)?
2. `events`: types such as `person_entered`, `person_left`, `scene_changed`, `text_appeared`, `text_changed`. Do the times match what you see in the video?
3. `texts`: is the on-screen text read well?
4. `actions`: `walking` and `standing` where expected?
5. `relations`: `holding` and `near`. Expect some false `holding`.

Optional decode check (the video must still play):
```bash
bin/virex-decode real.virex -o real.out.mp4
```

---

## Step 8: Read the cost log (speed and accuracy of the pipeline)

```bash
grep '"ml_detect"' cost.jsonl | head -3
grep '"ml_ocr"' cost.jsonl | head -3
```
Each line is one ML call: `wall_ms` (round trip), `model`, `bytes_in`, `note` (frame, detections, `server_ms`). A line with `"model_calls":0` in `ml_ocr` is an OCR call that was skipped because the slide had not changed.

Quick summary (needs `jq`):
```bash
jq -s '[.[]|select(.stage=="ml_detect")]|{calls:length, avg_ms:(map(.wall_ms)|add/length)}' cost.jsonl
jq -s '[.[]|select(.stage=="ml_ocr" and .model_calls==1)]|{calls:length, avg_ms:(map(.wall_ms)|add/length)}' cost.jsonl
```
Record: detect average ms, OCR average ms, number of reused OCR calls, total encode time.

---

## Step 9 (optional): GPU, other machines, tuning

```bash
docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build   # NVIDIA container toolkit needed
```
Containers on another machine: `-ml-detect host:50051 -ml-ocr host:50052`, and add `"format": "jpeg"` under `ml` in the config to cut network traffic. There is no authentication on the gRPC port; only do this on a private network.

Tuning knobs are all under `engine` in `configs/phase2.json` (rules and numbers: `docs/phase-2-rules.md`). The usual first changes: `engine.tracker.max_gap_seconds` if people split into several entities, `ml.static_levels` if OCR misses a changed slide (lower it) or runs too often (raise it).

---

## Stop and clean up

```bash
docker compose down            # add --rmi local to also delete the image
```

---

## What to send back

Copy this and fill it in:

```text
Machine: OS / CPU cores / RAM / GPU?
Step 1 pytest:            passed / failed (output)
Step 2 go test:           passed / failed (output)
Step 4 fake in Docker:    passed / failed (output)
Step 5 build time + size: ___ min, image ___ GB    (docker images virex-ml)
Step 6 detect ms: ___     ocr ms: ___     ocr text read correctly? yes/no
Step 7 clip: length ___ s, encode time ___ s
       entities ___ events ___ relations ___ (and anything that looks wrong)
Step 8 avg detect ms ___, avg ocr ms ___, ocr reused ___ of ___
Anything surprising:
```
Send the first failing command with its full output, plus `cost.jsonl` and `real.json` (or the first 200 lines) from steps 7 and 8. With that I can fix the build or tune the thresholds.
