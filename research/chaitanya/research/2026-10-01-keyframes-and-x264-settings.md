# Key frames, x264 settings, quality and Windows

| Field | Value |
|---|---|
| **Topic** | ffprobe key-frame listing, CRF/preset/GOP trade-offs measured with SSIM and PSNR, Windows pitfalls, recommended defaults |
| **Date** | 2026-10-01 |
| **Author** | Claude, running Aniket's task (Aniket has not started it) |
| **Task** | Tasks — Chaitanya, notes 1–4 |
| **Status** | draft, needs a human review |

## 1. Question
1. Which `ffprobe` command lists key frames with times and positions?
2. What do CRF, preset and GOP size control, and what are sensible values?
3. How do we measure SSIM and PSNR with FFmpeg?
4. What goes wrong on Windows, and what defaults should VIReX use?

## 2. Short answer
- Key frames: `ffprobe -v error -select_streams v:0 -show_entries packet=pts_time,dts_time,pos,flags -of csv=p=0 FILE`, keep lines whose flags start with `K`.
- Default for VIReX: **libx264, CRF 18, preset medium, GOP = 2 seconds, yuv420p**. On all three test clips CRF 18 gave SSIM >= 0.990 and PSNR >= 40.7 dB against the source.
- If the source is already H.264 8-bit yuv420p, **stream copy gives zero quality loss**. VIReX offers it as a mode.

## 3. Sources
- Own experiments below (ffmpeg 9.0.2 gyan.dev full build, libx264, Windows 10).
- FFmpeg documentation for the `ssim` and `psnr` filters and the x264 options (`-crf`, `-preset`, `-g`) — standard options, checked by running them.

## 4. Findings

### 4.1 Key frames with ffprobe
An I-frame (key frame) is a frame stored on its own, so decoding can start there.

```text
ffprobe -v error -select_streams v:0 -show_entries packet=pts_time,dts_time,pos,flags -of csv=p=0 c04_cuts_360p.mp4
```

Key-frame lines only (flags `K__`):

```text
0.000000,-0.066667,48,K__
2.000000,1.933333,423575,K__
2.500000,2.433333,539590,K__
4.500000,4.433333,1422824,K__
5.000000,4.933333,1740187,K__
7.000000,6.933333,1742936,K__
7.533333,7.466667,1903518,K__
9.533333,9.466667,5604098,K__
```

Fields: presentation time, decode time, byte position in the file, flags. The clip has hard cuts at 2.5, 5.0 and 7.5 s; x264 placed key frames at the cuts (2.5, 5.0, about 7.53) on top of the regular GOP key frames (0, 2, 4.5, 7, 9.5). Note the first DTS is negative: B-frames make decode time run before display time.

Alternative that decodes frame headers: `-skip_frame nokey -show_entries frame=pts_time,pict_type,pkt_pos`. It is slower and gives the same key frames. VIReX uses the packet form (`codec/encoder.Packets`) because it never decodes.

### 4.2 What the settings control
| Setting | Controls | Trade-off |
|---|---|---|
| **CRF** | Quality target. Lower number = better quality, bigger file | 18 is close to visually lossless, 23 is the x264 default, 28 is clearly softer |
| **Preset** | Encoder effort (speed vs compression efficiency) | Slower = smaller file at the same quality, but longer encode |
| **GOP size** (`-g`) | Maximum distance between key frames | Short GOP: faster seeking, larger file. Long GOP: smaller file, slower seeking |

### 4.3 Measuring quality
```text
ffmpeg -i distorted.mp4 -i reference.mp4 -lavfi "[0:v][1:v]ssim;[0:v][1:v]psnr" -f null -
```
The summary lines print `SSIM ... All:0.99xxxx` and `PSNR ... average:xx.xx`. The distorted file is the first input. `-f null -` works on Windows (no `/dev/null` or `NUL` needed). SSIM 1.0 is identical. PSNR above about 40 dB is hard to tell from the source by eye.

### 4.4 Experiment
Reference: the test clips (H.264 CRF 12, made by `scripts/make-testclips.ps1`). Each row is one encode of one clip with `-an -pix_fmt yuv420p`. Script: run in the session, results below. Timings are from a single run on a busy laptop, so treat them as indicative only (the same setting varied by 2x between runs of different clips).

**CRF sweep (preset medium, GOP 60)**
| Clip | CRF | Size MB (source MB) | SSIM | PSNR dB | Time s |
|---|---|---|---|---|---|
| c01 mandelbrot 720p | 18 | 12.14 (19.77) | 0.9941 | 44.27 | 15.5 |
| | 20 | 9.95 | 0.9927 | 42.50 | 14.4 |
| | 23 | 7.21 | 0.9896 | 39.93 | 13.8 |
| | 28 | 3.44 | 0.9795 | 35.62 | 10.8 |
| c03 life 360p | 18 | 8.33 (11.39) | 0.9903 | 40.75 | 8.5 |
| | 20 | 7.21 | 0.9861 | 38.86 | 7.4 |
| | 23 | 5.76 | 0.9766 | 36.13 | 7.0 |
| | 28 | 3.49 | 0.9447 | 31.36 | 5.5 |
| c02 testsrc2 360p | 18 | 1.49 (2.14) | 0.9981 | 48.97 | 1.7 |
| | 20 | 1.29 | 0.9973 | 47.14 | 1.5 |
| | 23 | 1.01 | 0.9951 | 44.18 | 1.4 |
| | 28 | 0.56 | 0.9868 | 39.41 | 1.3 |

**Preset sweep (CRF 23, GOP 60)**
| Clip | Preset | Size MB | SSIM | PSNR dB | Time s |
|---|---|---|---|---|---|
| c01 | ultrafast | 17.55 | 0.9900 | 43.84 | 2.8 |
| | veryfast | 6.11 | 0.9859 | 38.15 | 5.6 |
| | medium | 7.21 | 0.9896 | 39.93 | 13.8 |
| | slow | 7.10 | 0.9897 | 39.97 | 21.8 |
| c03 | ultrafast | 10.65 | 0.9725 | 37.91 | 1.7 |
| | veryfast | 5.11 | 0.9581 | 33.68 | 2.2 |
| | medium | 5.76 | 0.9766 | 36.13 | 7.0 |
| | slow | 5.74 | 0.9773 | 36.21 | 7.6 |
| c02 | ultrafast | 2.43 | 0.9975 | 49.69 | 0.5 |
| | veryfast | 0.93 | 0.9931 | 42.05 | 0.9 |
| | medium | 1.01 | 0.9951 | 44.18 | 1.4 |
| | slow | 0.97 | 0.9952 | 44.17 | 2.0 |

At the same CRF the fast presets spend bits differently: `ultrafast` makes much bigger files (sometimes bigger than the source) with higher scores, and `veryfast` makes smaller files with lower scores. Comparing presets at one CRF therefore mixes size and quality. `slow` is only 1–2 % smaller than `medium` for 1.5x the time.

**GOP sweep (CRF 23, preset medium)**
| Clip | GOP | Key frames in 10 s | Size MB | SSIM |
|---|---|---|---|---|
| c01 | 30 | 10 | 7.53 | 0.9898 |
| | 60 | 5 | 7.21 | 0.9896 |
| | 250 | 2 | 7.08 | 0.9894 |
| c03 | 30 | 10 | 6.01 | 0.9777 |
| | 60 | 5 | 5.76 | 0.9766 |
| | 250 | 2 | 5.66 | 0.9732 |
| c02 | 30 | 10 | 1.05 | 0.9953 |
| | 60 | 5 | 1.01 | 0.9951 |
| | 250 | 2 | 0.99 | 0.9951 |

GOP 30 to 250 changes size by about 6 %, so a 1–2 s GOP costs little and gives good random access. Scene cuts add extra key frames on top.

### 4.5 Windows checklist (all seen or verified on this machine)
- [ ] ffmpeg and ffprobe installed with WinGet (`Gyan.FFmpeg`). Open a **new** terminal after install so PATH updates. Check with `ffmpeg -version`.
- [ ] Check `libx264` exists: `ffmpeg -encoders | findstr 264`.
- [ ] ffprobe prints **CRLF** line endings (`\r\n`). Trim lines before parsing; do not split on `\n` only.
- [ ] Use `exec.Command("ffmpeg", args...)` with separate arguments. Never build one command string; paths with spaces and the `select='...'` quotes break otherwise.
- [ ] Use `-f null -` for discarded output. `NUL` and `/dev/null` are not portable.
- [ ] Pass `-nostdin` when ffmpeg runs from a program that owns stdin, and `-v error` to keep stderr small.
- [ ] Read ffmpeg stdout through a pipe with a large buffer (1 MiB used) and drain stderr separately, or the process can block.
- [ ] `-race` needs CGO and a C compiler; it is not available by default here.
- [ ] Use `-fps_mode passthrough` (not the old `-vsync`) when frames must keep their timestamps.

## 5. Experiments
See 4.4. Reproduce with the commands in 4.1 and 4.3 and `ffmpeg -i SRC -an -c:v libx264 -crf N -preset P -g G -pix_fmt yuv420p OUT`.

## 6. Decision / recommendation
Defaults for VIReX (`codec/pixel`):

| Setting | Value | Reason |
|---|---|---|
| Codec | libx264, yuv420p, 8-bit | Universal playback |
| CRF | **18** | SSIM >= 0.990 and PSNR >= 40.7 dB on every test clip; about 60–75 % of the size of an already-compressed source |
| Preset | **medium** | `slow` gains 1–2 % for 1.5x time |
| GOP | **2 seconds** (`round(fps * 2)`) | About 6 % size cost versus a long GOP, good seeking |
| B-frames, scene cut | x264 defaults | Timestamps handle B-frames (PTS and DTS are stored) |
| Mode `copy` | available | If the source is H.264 yuv420p, stream copy has **no generation loss** and exact timestamps for free |

Rejected: CRF 23 as default (SSIM 0.977 on the crisp-pattern clip), `ultrafast`/`veryfast` (they change the size/quality balance in ways that make comparisons unfair), AV1 (slow, only a later experiment).

## 7. Open questions
1. These clips are synthetic. Re-run on real lecture and screen-recording footage before the numbers go in the paper.
2. Do we want 10-bit or 4:4:4 input to survive? x264 here is forced to 8-bit 4:2:0.
3. A human should check CRF 18 visually on real footage.

## 8. Glossary
| Short | Full form |
|---|---|
| CRF | Constant Rate Factor |
| GOP | Group of Pictures |
| SSIM | Structural Similarity Index Measure |
| PSNR | Peak Signal-to-Noise Ratio |
| PTS / DTS | Presentation / Decode Time Stamp |
| CRLF | Carriage Return + Line Feed (Windows line ending) |
