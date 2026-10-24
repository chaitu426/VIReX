// virex-encode turns a video into a .virex file.
//
//	virex-encode input.mp4 -o out.virex [flags]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"virex/codec/pipeline"
	"virex/codec/pixel"
	"virex/codec/sampler"
	"virex/cost"
)

const usage = "usage: virex-encode input.mp4 -o out.virex [flags]"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("virex-encode", flag.ContinueOnError)
	var (
		out      = fs.String("o", "", "output .virex file (default: input name with .virex)")
		cfgPath  = fs.String("config", "", "JSON config file (see configs/default.json)")
		mode     = fs.String("pixel", "", "pixel mode: auto, copy or reencode")
		crf      = fs.Int("crf", 0, "x264 CRF (default 18)")
		preset   = fs.String("preset", "", "x264 preset (default medium)")
		gop      = fs.Float64("gop", 0, "max seconds between key frames (default 2)")
		sample   = fs.String("sample", "", "sampling: scene_change, every_n or every_seconds")
		n        = fs.Int("n", 0, "every_n: frames between samples")
		secs     = fs.Float64("seconds", 0, "every_seconds: seconds between samples")
		noSample = fs.Bool("no-sample", false, "skip the sampling pass")
		semantic = fs.String("semantic", "", "SVIR content: empty, fake or ml (real detection/OCR, needs the ML containers)")
		mlDetect = fs.String("ml-detect", "", "ml: detector address host:port (default 127.0.0.1:50051)")
		mlOCR    = fs.String("ml-ocr", "", "ml: OCR address host:port (default 127.0.0.1:50052)")
		mlWork   = fs.Int("ml-workers", 0, "ml: frames analysed in parallel (default 4)")
		noOCR    = fs.Bool("ml-no-ocr", false, "ml: do not run OCR")
		noDetect = fs.Bool("ml-no-detect", false, "ml: do not run object detection")
		split    = fs.Bool("split-layers", false, "one SEMANTIC section per layer")
		costLog  = fs.String("cost-log", "", "append JSON-lines cost records to this file")
		quiet    = fs.Bool("q", false, "print nothing but errors")
	)
	valued := map[string]bool{"o": true, "config": true, "pixel": true, "crf": true, "preset": true, "gop": true,
		"sample": true, "n": true, "seconds": true, "semantic": true, "cost-log": true,
		"ml-detect": true, "ml-ocr": true, "ml-workers": true}
	input, rest := splitInput(args, valued)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if input == "" && fs.NArg() == 1 {
		input = fs.Arg(0)
	} else if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	if input == "" {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}

	cfg := pipeline.DefaultConfig()
	if *cfgPath != "" {
		c, err := pipeline.LoadConfig(*cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cfg = c
	}
	if *mode != "" {
		cfg.Pixel.Mode = pixel.Mode(*mode)
	}
	if *crf != 0 {
		cfg.Pixel.CRF = *crf
	}
	if *preset != "" {
		cfg.Pixel.Preset = *preset
	}
	if *gop != 0 {
		cfg.Pixel.GOPSeconds = *gop
	}
	if *sample != "" {
		cfg.Sampler.Mode = *sample
	}
	if *n != 0 {
		cfg.Sampler.N = *n
	}
	if *secs != 0 {
		cfg.Sampler.Seconds = *secs
	}
	switch cfg.Sampler.Mode {
	case sampler.SceneChange, sampler.EveryN, sampler.EverySeconds:
	default:
		fmt.Fprintf(os.Stderr, "unknown sampling mode %q\n", cfg.Sampler.Mode)
		return 2
	}
	if *noSample {
		cfg.SkipSampling = true
	}
	if *semantic != "" {
		cfg.Semantic = *semantic
	}
	if *mlDetect != "" {
		cfg.ML.DetectAddr = *mlDetect
	}
	if *mlOCR != "" {
		cfg.ML.OCRAddr = *mlOCR
	}
	if *mlWork != 0 {
		cfg.ML.Workers = *mlWork
	}
	cfg.ML.NoOCR = cfg.ML.NoOCR || *noOCR
	cfg.ML.NoDetect = cfg.ML.NoDetect || *noDetect
	if cfg.Semantic == pipeline.SemanticML && *cfgPath == "" && *sample == "" && *n == 0 && *secs == 0 {
		// Tracking needs a frame at least every second or so; the Phase 1 default allows 10 s.
		cfg.Sampler.MaxGapSeconds = 1
	}
	if *split {
		cfg.SplitLayers = true
	}

	if *out == "" {
		*out = strings.TrimSuffix(input, filepath.Ext(input)) + ".virex"
	}
	var log *cost.Logger
	if *costLog != "" {
		l, closer, err := cost.Open(*costLog, fmt.Sprintf("%s-%d", filepath.Base(input), time.Now().Unix()))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer closer.Close()
		log = l
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	start := time.Now()
	rep, err := pipeline.Encode(ctx, input, *out, cfg, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "virex-encode:", err)
		return 1
	}
	if !*quiet {
		for _, w := range rep.Warnings {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
		fmt.Printf("%s -> %s\n", rep.Source, rep.Output)
		fmt.Printf("  video      %dx%d, %d frames, %d key frames, timescale %d\n", rep.Width, rep.Height, rep.Frames, rep.KeyFrames, rep.Timescale)
		fmt.Printf("  pixel      %s mode\n", rep.PixelMode)
		if !cfg.SkipSampling {
			fmt.Printf("  sampled    %d frames (%s)\n", rep.Sampled, cfg.Sampler.Mode)
		}
		if sm := rep.Semantic; sm != nil {
			for _, m := range sm.Models {
				fmt.Printf("  model      %s: %s (%s, %s)\n", m.Role, m.Name, m.Version, m.Device)
			}
			fmt.Printf("  semantic   %d entities, %d events, %d actions, %d relations, %d text blocks, %d shots\n",
				sm.Entities, sm.Events, sm.Actions, sm.Relations, sm.Texts, sm.Shots)
			fmt.Printf("  ml calls   %d detect, %d ocr (%d reused), %.1fs model time\n",
				sm.DetectCalls, sm.OCRCalls, sm.OCRReused, sm.InferenceMS/1000)
		}
		fmt.Printf("  size       source %s, pixel stream %s, .virex %s\n", mb(rep.SourceBytes), mb(rep.PixelBytes), mb(rep.OutputBytes))
		fmt.Printf("  time       %.2fs\n", time.Since(start).Seconds())
	}
	return 0
}

// splitInput pulls the first non-flag argument out, so "in.mp4 -o x" works as well as
// "-o x in.mp4". valued lists the flags that take a separate value.
func splitInput(args []string, valued map[string]bool) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return a, append(append([]string{}, args[:i]...), args[i+1:]...)
		}
		name := strings.TrimLeft(a, "-")
		if valued[name] && !strings.Contains(name, "=") {
			i++ // skip this flag's value
		}
	}
	return "", args
}

func mb(b int64) string { return fmt.Sprintf("%.2f MB", float64(b)/1e6) }
