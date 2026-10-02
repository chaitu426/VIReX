// framebench compares ways to move decoded frames from ffmpeg into Go:
// raw RGB24, JPEG and PNG, on the same clip. It reports wall time, bytes through
// the pipe and peak Go heap. Usage: go run ./scripts/framebench clip.mp4
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"time"

	"virex/codec/encoder"
)

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: framebench clip.mp4")
		os.Exit(2)
	}
	path := os.Args[1]
	vi, err := encoder.Probe(context.Background(), path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("clip %s  %dx%d\n", path, vi.Width, vi.Height)
	fmt.Printf("%-6s %8s %10s %10s %10s %10s\n", "format", "frames", "wall s", "fps", "pipe MB", "peak heap MB")

	type variant struct {
		name string
		args []string
		read func(r *bufio.Reader, w, h int) (int, error)
	}
	rgb := func(r *bufio.Reader, w, h int) (int, error) {
		n := 0
		for {
			buf := make([]byte, w*h*3)
			if _, err := io.ReadFull(r, buf); err != nil {
				if err == io.EOF {
					return n, nil
				}
				return n, err
			}
			n++
		}
	}
	dec := func(decode func(io.Reader) (image.Image, error)) func(*bufio.Reader, int, int) (int, error) {
		return func(r *bufio.Reader, w, h int) (int, error) {
			n := 0
			for {
				if _, err := r.Peek(1); err == io.EOF {
					return n, nil
				}
				if _, err := decode(r); err != nil {
					return n, err
				}
				n++
			}
		}
	}
	for _, v := range []variant{
		{"rgb24", []string{"-f", "rawvideo", "-pix_fmt", "rgb24"}, rgb},
		{"jpeg", []string{"-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "2", "-pix_fmt", "yuvj420p"}, dec(decodeJPEG)},
		{"png", []string{"-f", "image2pipe", "-c:v", "png"}, dec(png.Decode)},
	} {
		runtime.GC()
		var peak uint64
		var stop atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			var m runtime.MemStats
			for !stop.Load() {
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		args := append([]string{"-v", "error", "-nostdin", "-i", path, "-map", "0:v:0", "-fps_mode", "passthrough"}, v.args...)
		args = append(args, "pipe:1")
		cmd := exec.Command("ffmpeg", args...)
		out, _ := cmd.StdoutPipe()
		start := time.Now()
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cr := &countingReader{r: out}
		n, rerr := v.read(bufio.NewReaderSize(cr, 1<<20), vi.Width, vi.Height)
		_, _ = io.Copy(io.Discard, out) // never leave ffmpeg blocked on a full pipe
		_ = cmd.Wait()
		wall := time.Since(start).Seconds()
		stop.Store(true)
		<-done
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", v.name, rerr)
		}
		fmt.Printf("%-6s %8d %10.2f %10.1f %10.1f %10.1f\n", v.name, n, wall, float64(n)/wall,
			float64(cr.n)/1e6, float64(peak)/1e6)
	}
}

// decodeJPEG reads one JPEG by scanning for the end-of-image marker (FF D9). Inside
// entropy-coded data every FF byte is stuffed as FF 00, so FF D9 only ends an image.
// Decoding straight from the pipe lost frame boundaries on detailed 720p frames.
func decodeJPEG(r io.Reader) (image.Image, error) {
	br := r.(*bufio.Reader)
	var buf []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		buf = append(buf, b)
		if n := len(buf); n >= 2 && buf[n-2] == 0xFF && buf[n-1] == 0xD9 {
			return jpeg.Decode(bytes.NewReader(buf))
		}
	}
}
