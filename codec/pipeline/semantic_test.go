package pipeline_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"virex/codec/container"
	"virex/codec/pipeline"
	"virex/cost"
	"virex/semantic/mltest"
	"virex/svir/schema"
)

// makeClip renders a 14 s, 10 fps clip with solid rectangles the fake ML model understands:
//
//	red   "person": walks in from the left edge at t=3, stops at x=120 at t=7, vanishes at t=10
//	blue  "phone":  in the person's box from t=5 to t=9
//	green "text":   on screen t=0..8
//	the background turns grey at t=7 (a scene cut)
func makeClip(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	graph := strings.Join([]string{
		"color=c=white:s=320x180:r=10:d=14[bg]",
		"color=c=0xE61414:s=40x80:r=10:d=14[p]",
		"color=c=0x1414E6:s=16x24:r=10:d=14[ph]",
		"color=c=0x14E614:s=120x20:r=10:d=14[tx]",
		"[bg]drawbox=x=0:y=0:w=iw:h=ih:color=gray:t=fill:enable='gte(t,7)'[b1]",
		"[b1][p]overlay=x='if(lt(t,7),(t-3)*30,120)':y=50:enable='between(t,3,10)'[b2]",
		"[b2][ph]overlay=x=130:y=80:enable='between(t,5,9)'[b3]",
		"[b3][tx]overlay=x=180:y=20:enable='between(t,0,8)'[v]",
	}, ";")
	out := filepath.Join(dir, "clip.mp4")
	args := []string{"-v", "error", "-y", "-filter_complex", graph, "-map", "[v]", "-t", "14",
		"-c:v", "libx264", "-crf", "12", "-pix_fmt", "yuv420p", "-g", "20", out}
	if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, b)
	}
	return out
}

func readBack(t *testing.T, virex string) (*schema.SVIRDocument, pipeline.Metadata, *container.Reader) {
	t.Helper()
	f, err := os.Open(virex)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	st, _ := f.Stat()
	rd, err := container.NewReader(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	if err := rd.VerifyAll(); err != nil {
		t.Fatal(err)
	}
	var docs []*schema.SVIRDocument
	for _, e := range rd.FindRange(container.SectionSemantic, container.LayerAll, 0, 0) {
		b, err := rd.ReadSection(e)
		if err != nil {
			t.Fatal(err)
		}
		d, err := schema.Unmarshal(b)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		e, ok := rd.FindType(container.SectionSemantic)
		if !ok {
			t.Fatal("no semantic section")
		}
		b, _ := rd.ReadSection(e)
		d, err := schema.Unmarshal(b)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, d)
	}
	me, _ := rd.FindType(container.SectionMetadata)
	mb, _ := rd.ReadSection(me)
	var meta pipeline.Metadata
	if err := json.Unmarshal(mb, &meta); err != nil {
		t.Fatal(err)
	}
	return docs[0], meta, rd
}

func mlConfig(addr string) pipeline.Config {
	cfg := pipeline.DefaultConfig()
	cfg.Semantic = pipeline.SemanticML
	cfg.Sampler.MaxGapSeconds = 1
	cfg.ML.DetectAddr, cfg.ML.OCRAddr = addr, addr
	cfg.ML.BackoffMS = 5
	cfg.ML.ConnectTimeoutSeconds = 2
	return cfg
}

func TestEncodeWithML(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	srv := mltest.Start(t)
	out := filepath.Join(dir, "clip.virex")
	var costBuf strings.Builder
	rep, err := pipeline.Encode(context.Background(), clip, out, mlConfig(srv.Addr), cost.New(&costBuf, "test"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Semantic == nil || rep.Sampled < 10 {
		t.Fatalf("report: %+v", rep)
	}
	for _, w := range rep.Warnings {
		t.Logf("warning: %s", w)
	}

	doc, meta, _ := readBack(t, out)
	if err := schema.Validate(doc, true); err != nil {
		t.Fatal(err)
	}

	// A person in the clip is ONE entity with a lifetime, not one detection per frame.
	var people, phones int
	var person *schema.Entity
	for _, e := range doc.Entities {
		switch e.Type {
		case "person":
			people++
			person = e
		case "cell phone":
			phones++
		}
	}
	if people != 1 || phones != 1 {
		t.Fatalf("entities: %d people, %d phones: %+v", people, phones, doc.Entities)
	}
	first, last := float64(person.FirstSeen), float64(person.LastSeen)
	scale := float64(rep.Timescale)
	if first/scale < 2.5 || first/scale > 3.6 || last/scale < 9 || last/scale > 10.2 {
		t.Fatalf("person lifetime %.2fs..%.2fs, want about 3..10", first/scale, last/scale)
	}
	if n := len(doc.Objects); n > 40 {
		t.Fatalf("%d object blocks: tracking did not collapse detections", n)
	}

	types := map[string]int{}
	for _, e := range doc.Events {
		types[e.Type]++
	}
	for _, want := range []string{"person_entered", "scene_changed", "text_appeared"} {
		if types[want] == 0 {
			t.Errorf("missing %s; events: %v", want, types)
		}
	}
	if len(types) < 3 {
		t.Fatalf("event types: %v", types)
	}
	var holding bool
	for _, r := range doc.Relations {
		if r.Relation == "holding" {
			holding = true
		}
	}
	if !holding {
		t.Errorf("no holding relation: %+v", doc.Relations)
	}
	if len(doc.Texts) == 0 || doc.Texts[0].Content != "FAKE TEXT" {
		t.Fatalf("texts: %+v", doc.Texts)
	}

	// Every block has a layer (C3) and a pixel_ref (C4): Validate enforces it; check the
	// frame ids point at sampled frames.
	sampled := map[int64]bool{}
	for _, s := range meta.Samples {
		sampled[int64(s.FrameID)] = true
	}
	for _, e := range doc.Entities {
		if e.PixelRef.FrameId == nil || !sampled[*e.PixelRef.FrameId] {
			t.Fatalf("entity %d pixel_ref frame %v is not a sampled frame", e.Id, e.PixelRef.FrameId)
		}
	}

	// Metadata names the models, and the cost log has one record per ML call (C1).
	if len(meta.Models) != 2 || meta.Models[0].Role != "detector" || meta.Models[0].Name != "fake-detector" {
		t.Fatalf("models: %+v", meta.Models)
	}
	if meta.Semantic == nil || meta.Semantic.DetectCalls != rep.Sampled {
		t.Fatalf("semantic meta: %+v (sampled %d)", meta.Semantic, rep.Sampled)
	}
	var detect, ocrCalls, reused int
	for _, line := range strings.Split(strings.TrimSpace(costBuf.String()), "\n") {
		var r cost.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		switch {
		case r.Stage == "ml_detect" && r.ModelCalls == 1 && r.Model != "":
			detect++
		case r.Stage == "ml_ocr" && r.ModelCalls == 1:
			ocrCalls++
		case r.Stage == "ml_ocr" && r.ModelCalls == 0:
			reused++
		}
	}
	if detect != rep.Sampled || ocrCalls+reused != rep.Sampled {
		t.Fatalf("cost log: %d detect, %d ocr, %d reused records for %d sampled frames", detect, ocrCalls, reused, rep.Sampled)
	}
	if int64(srv.DetectCalls.Load()) != int64(detect) || int(srv.ReadCalls.Load()) != ocrCalls {
		t.Fatalf("server saw %d detect / %d read calls", srv.DetectCalls.Load(), srv.ReadCalls.Load())
	}
	if reused == 0 {
		t.Errorf("a static slide for 8 s should have reused OCR at least once")
	}
}

func TestEncodeWithMLSplitLayers(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	srv := mltest.Start(t)
	cfg := mlConfig(srv.Addr)
	cfg.SplitLayers = true
	out := filepath.Join(dir, "split.virex")
	if _, err := pipeline.Encode(context.Background(), clip, out, cfg, nil); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(out)
	defer f.Close()
	st, _ := f.Stat()
	rd, err := container.NewReader(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	var docs []*schema.SVIRDocument
	for l := container.Layer(1); l <= 4; l++ {
		if e, ok := rd.Find(container.SectionSemantic, l); ok {
			b, err := rd.ReadSection(e)
			if err != nil {
				t.Fatal(err)
			}
			d, err := schema.Unmarshal(b)
			if err != nil {
				t.Fatal(err)
			}
			docs = append(docs, d)
		}
	}
	if len(docs) < 3 {
		t.Fatalf("%d layer sections, want L0, L1 and L2", len(docs))
	}
	if err := schema.Validate(schema.Merge(docs...), true); err != nil {
		t.Fatalf("merged layers: %v", err)
	}
}

func TestEncodeFailsFastWhenMLIsDown(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	cfg := mlConfig("127.0.0.1:1")
	cfg.ML.ConnectTimeoutSeconds = 1
	start := time.Now()
	out := filepath.Join(dir, "x.virex")
	_, err := pipeline.Encode(context.Background(), clip, out, cfg, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if time.Since(start) > 15*time.Second {
		t.Fatalf("took %v to notice the container is down", time.Since(start))
	}
	if !strings.Contains(err.Error(), "unreachable") && !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("unhelpful error: %v", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("a failed encode left an output file")
	}
}

func TestEncodeWaitsForModelToLoad(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	srv := mltest.Start(t)
	srv.SetReady(false)
	go func() { time.Sleep(1500 * time.Millisecond); srv.SetReady(true) }()
	out := filepath.Join(dir, "late.virex")
	if _, err := pipeline.Encode(context.Background(), clip, out, mlConfig(srv.Addr), nil); err != nil {
		t.Fatal(err)
	}
}

func TestMLRejectsSkipSampling(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	cfg := mlConfig("127.0.0.1:1")
	cfg.SkipSampling = true
	if _, err := pipeline.Encode(context.Background(), clip, filepath.Join(dir, "x.virex"), cfg, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestSparseSamplingWarns(t *testing.T) {
	dir := t.TempDir()
	clip := makeClip(t, dir)
	srv := mltest.Start(t)
	cfg := mlConfig(srv.Addr)
	cfg.Sampler.MaxGapSeconds = 10
	rep, err := pipeline.Encode(context.Background(), clip, filepath.Join(dir, "w.virex"), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, w := range rep.Warnings {
		if strings.Contains(w, "split into several entities") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("no sampling warning in %v", rep.Warnings)
	}
}
