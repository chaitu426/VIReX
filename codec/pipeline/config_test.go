package pipeline

import "testing"

func TestPhase2ConfigLoads(t *testing.T) {
	cfg, err := LoadConfig("../../configs/phase2.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Semantic != SemanticML || cfg.Sampler.MaxGapSeconds != 1 || cfg.ML.DetectAddr != DefaultDetectAddr ||
		cfg.ML.OCRAddr != DefaultOCRAddr || cfg.Engine.Tracker.MaxGapSeconds != 3 || cfg.Engine.SVIR.BBoxDelta != 0.02 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestDefaultConfigStillLoads(t *testing.T) {
	if _, err := LoadConfig("../../configs/default.json"); err != nil {
		t.Fatal(err)
	}
}
