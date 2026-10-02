// printframes prints "frame number, pts, time" for every frame of a video, using the
// real packet timestamps from ffprobe (not frame number / fps).
// Usage: go run ./scripts/printframes clip.mp4
package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"virex/codec/encoder"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: printframes clip.mp4")
		os.Exit(2)
	}
	ctx := context.Background()
	vi, err := encoder.Probe(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pkts, err := encoder.Packets(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pts := make([]int64, len(pkts))
	for i, p := range pkts {
		pts[i] = p.PTS
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i] < pts[j] })
	tb := float64(vi.TimeBaseNum) / float64(vi.TimeBaseDen)
	fmt.Println("frame,pts,time_s")
	for i, p := range pts {
		fmt.Printf("%d,%d,%.6f\n", i, p, float64(p)*tb)
	}
}
