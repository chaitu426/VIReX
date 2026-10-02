# Test clips

Video files are not committed. List each clip here with its source, licence and length.

| File | Type | Length | Source / licence |
|---|---|---|---|
| Semantic_Video_Codec.mp4 | real footage (content not reviewed), 1280x720, 24 fps H.264 + AAC audio | 9 min 30 s | added by the team; source and licence to be filled in |

The synthetic clips c01 to c08 were removed. `pwsh scripts/make-testclips.ps1` recreates them. Several tests (`sampler`, `scheduler`, `tests/integration`) look for those synthetic clips by name and **skip** when they are missing, so with only the real clip they check less. **Still missing:** real lecture, screen-recording, street and indoor clips. Add them here with their source and licence.
