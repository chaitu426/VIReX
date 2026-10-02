# Generates the synthetic test clips into tests/testdata/clips (not committed).
# Needs ffmpeg on PATH. Real footage (lecture, screen recording, street, indoor)
# should be added by hand; see tests/testdata/clips/README.md.
$ErrorActionPreference = 'Stop'
$out = Join-Path $PSScriptRoot '..\tests\testdata\clips'
New-Item -ItemType Directory -Force $out | Out-Null

# The "source" clips are high-quality H.264 (CRF 12), like a good camera/export file.
$enc = @('-c:v', 'libx264', '-crf', '12', '-preset', 'medium', '-pix_fmt', 'yuv420p', '-bf', '2', '-g', '60')

function Make($name, $inputs, $extra) {
    $path = Join-Path $out $name
    $args = @('-v', 'error', '-y') + $inputs + $extra + $enc + @($path)
    & ffmpeg @args
    if ($LASTEXITCODE -ne 0) { throw "ffmpeg failed for $name" }
    Write-Host "made $name"
}

# c01: detailed motion, 720p
Make 'c01_mandelbrot_720p.mp4' @('-f','lavfi','-i','mandelbrot=size=1280x720:rate=30') @('-t','10')
# c02: test pattern with moving elements, constant frame rate
Make 'c02_testsrc2_360p.mp4' @('-f','lavfi','-i','testsrc2=size=640x360:rate=30') @('-t','10')
# c03: crisp pixel patterns, like a screen recording
Make 'c03_life_360p.mp4' @('-f','lavfi','-i','life=size=640x360:rate=30:mold=10:ratio=0.1:random_seed=7:life_color=#00ff00:death_color=#aa0000') @('-t','10')
# c04: hard scene cuts at 2.5 s, 5.0 s, 7.5 s (ground truth for the scene-change sampler)
Make 'c04_cuts_360p.mp4' @(
    '-f','lavfi','-t','2.5','-i','testsrc2=size=640x360:rate=30',
    '-f','lavfi','-t','2.5','-i','mandelbrot=size=640x360:rate=30',
    '-f','lavfi','-t','2.5','-i','smptehdbars=size=640x360:rate=30',
    '-f','lavfi','-t','2.5','-i','life=size=640x360:rate=30:random_seed=3') @('-filter_complex','[0:v][1:v][2:v][3:v]concat=n=4:v=1[v]','-map','[v]')
# c05: variable frame rate (irregular gaps, original timestamps kept)
Make 'c05_vfr_360p.mp4' @('-f','lavfi','-i','testsrc2=size=640x360:rate=30') @('-t','10','-vf',"select='not(eq(mod(n,7),3))*not(eq(mod(n,11),5))'",'-fps_mode','vfr')
# c06: with an audio track (audio is not stored in .virex yet; see docs)
Make 'c06_audio_360p.mp4' @('-f','lavfi','-i','testsrc2=size=640x360:rate=30','-f','lavfi','-i','sine=frequency=440:sample_rate=44100') @('-t','10','-c:a','aac','-shortest')
# c07: noisy gradient, hard to compress
Make 'c07_noise_360p.mp4' @('-f','lavfi','-i','gradients=size=640x360:rate=30:speed=0.02') @('-t','10','-vf','noise=alls=20:allf=t')
# c08: almost static, very easy to compress
Make 'c08_static_360p.mp4' @('-f','lavfi','-i','smptebars=size=640x360:rate=30') @('-t','10')
