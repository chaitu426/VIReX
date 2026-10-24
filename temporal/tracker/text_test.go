package tracker

import "testing"

func TestSimilarity(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
		max  float64
	}{
		{"Intro to ML", "intro to ml!", 1, 1},
		{"Intro to Compression", "lntro to Compression", 0.9, 0.99},
		{"Agenda", "Summary", 0, 0.4},
		{"", "", 0, 0},
		{"abc", "", 0, 0},
	}
	for _, c := range cases {
		got := Similarity(c.a, c.b)
		if got < c.min || got > c.max {
			t.Errorf("Similarity(%q,%q)=%v, want in [%v,%v]", c.a, c.b, got, c.min, c.max)
		}
	}
}

func lineAt(text string, conf float64) []TextLine {
	return []TextLine{{Text: text, Conf: conf, Box: Box{0.1, 0.1, 0.5, 0.08}}}
}

func TestOCRFlickerBecomesOneTextTrack(t *testing.T) {
	tt := NewText(TextConfig{})
	readings := []struct {
		text string
		conf float64
	}{
		{"Intro to Compression", 0.95},
		{"lntro to Compression", 0.55}, // l for I
		{"Intro to Compression", 0.93},
		{"Intro to Compresslon", 0.50},
		{"Intro to Compression", 0.96},
	}
	for i, r := range readings {
		if err := tt.Update(Frame{ID: i, Time: float64(i)}, lineAt(r.text, r.conf)); err != nil {
			t.Fatal(err)
		}
	}
	tracks := tt.Finish()
	if len(tracks) != 1 {
		t.Fatalf("%d text tracks, want 1", len(tracks))
	}
	tr := tracks[0]
	if tr.Text != "Intro to Compression" || len(tr.Obs) != 5 || tr.First().Time != 0 || tr.Last().Time != 4 {
		t.Fatalf("track: %q obs=%d", tr.Text, len(tr.Obs))
	}
	if tr.Best().Conf != 0.96 {
		t.Fatalf("best %v", tr.Best().Conf)
	}
}

func TestTextChangeStartsANewTrack(t *testing.T) {
	tt := NewText(TextConfig{})
	for i := 0; i < 4; i++ {
		text := "Agenda"
		if i >= 2 {
			text = "Summary"
		}
		_ = tt.Update(Frame{ID: i, Time: float64(i)}, lineAt(text, 0.9))
	}
	tracks := tt.Finish()
	if len(tracks) != 2 || tracks[0].Text != "Agenda" || tracks[1].Text != "Summary" {
		t.Fatalf("%+v", tracks)
	}
	if tracks[0].ID != 1 || tracks[1].ID != 2 {
		t.Fatal("ids")
	}
}

func TestSingleReadingIsNoise(t *testing.T) {
	tt := NewText(TextConfig{})
	_ = tt.Update(Frame{ID: 0, Time: 0}, lineAt("x#@!", 0.5))
	_ = tt.Update(Frame{ID: 1, Time: 1}, nil)
	_ = tt.Update(Frame{ID: 2, Time: 2}, nil)
	if n := len(tt.Finish()); n != 0 {
		t.Fatalf("%d", n)
	}
}

func TestTwoLinesStayTwoTracks(t *testing.T) {
	tt := NewText(TextConfig{})
	for i := 0; i < 3; i++ {
		_ = tt.Update(Frame{ID: i, Time: float64(i)}, []TextLine{
			{Text: "Title", Conf: 0.9, Box: Box{0.1, 0.1, 0.4, 0.08}},
			{Text: "Bullet one", Conf: 0.9, Box: Box{0.1, 0.3, 0.4, 0.06}},
		})
	}
	tracks := tt.Finish()
	if len(tracks) != 2 || tracks[0].Text != "Title" {
		t.Fatalf("%+v", tracks)
	}
}

func TestSameTextElsewhereIsAnotherTrack(t *testing.T) {
	tt := NewText(TextConfig{})
	for i := 0; i < 3; i++ {
		_ = tt.Update(Frame{ID: i, Time: float64(i)}, []TextLine{
			{Text: "Exit", Conf: 0.9, Box: Box{0.05, 0.1, 0.1, 0.05}},
			{Text: "Exit", Conf: 0.9, Box: Box{0.8, 0.8, 0.1, 0.05}},
		})
	}
	if n := len(tt.Finish()); n != 2 {
		t.Fatalf("%d", n)
	}
}

func TestShortReadingsAreIgnored(t *testing.T) {
	tt := NewText(TextConfig{MinChars: 3})
	for i := 0; i < 3; i++ {
		_ = tt.Update(Frame{ID: i, Time: float64(i)}, lineAt("a", 0.9))
	}
	if n := len(tt.Finish()); n != 0 {
		t.Fatalf("%d", n)
	}
}

func TestTextGapSplitsTracks(t *testing.T) {
	tt := NewText(TextConfig{MaxGapSeconds: 2})
	times := []float64{0, 1, 2, 10, 11, 12}
	for i, tm := range times {
		_ = tt.Update(Frame{ID: i, Time: tm}, lineAt("Hello world", 0.9))
	}
	if n := len(tt.Finish()); n != 2 {
		t.Fatalf("%d, want 2 (text left and came back)", n)
	}
}
