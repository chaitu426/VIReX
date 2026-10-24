package tracker

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextLine is one OCR result in one frame.
type TextLine struct {
	Text string
	Conf float64
	Box  Box
}

// TextObs is one OCR line that was assigned to a text track.
type TextObs struct {
	Frame int
	Ticks int64
	Time  float64
	Text  string // as read in this frame, flicker and all
	Conf  float64
	Box   Box
}

// TextTrack is one piece of on-screen text followed over time. OCR flickers ("Intro",
// "lntro", "Intro to"), so Text is a confidence-weighted vote over every reading.
type TextTrack struct {
	ID   int
	Text string // consensus text
	Conf float64
	Obs  []TextObs

	votes map[string]float64
}

func (t *TextTrack) First() TextObs { return t.Obs[0] }
func (t *TextTrack) Last() TextObs  { return t.Obs[len(t.Obs)-1] }

// Best is the observation whose reading is the consensus text with the highest
// confidence: the representative frame and box.
func (t *TextTrack) Best() TextObs {
	best, found := t.Obs[0], false
	for _, o := range t.Obs {
		if o.Text == t.Text && (!found || o.Conf > best.Conf) {
			best, found = o, true
		}
	}
	return best
}

// Box is the box of the best reading; it is what new readings are matched against.
func (t *TextTrack) Box() Box { return t.Best().Box }

func (t *TextTrack) add(o TextObs) {
	t.Obs = append(t.Obs, o)
	if t.votes == nil {
		t.votes = map[string]float64{}
	}
	t.votes[o.Text] += o.Conf
	// Highest weight wins; ties go to the longer text, then alphabetically, so the
	// result does not depend on map order.
	best, bw := "", -1.0
	for s, w := range t.votes {
		switch {
		case w > bw, w == bw && len(s) > len(best), w == bw && len(s) == len(best) && s < best:
			best, bw = s, w
		}
	}
	t.Text = best
	var n int
	var sum float64
	for _, x := range t.Obs {
		if x.Text == best {
			n++
			sum += x.Conf
		}
	}
	t.Conf = sum / float64(n)
}

// TextConfig tunes the text tracker. Zero numeric fields take the defaults.
type TextConfig struct {
	// MinSimilarity is the text similarity (0..1, 1 = identical) needed to treat a new
	// reading as the same text (default 0.7).
	MinSimilarity float64 `json:"min_similarity"`
	// MinIoU is the box overlap needed as well (default 0.3).
	MinIoU float64 `json:"min_iou"`
	// MaxGapSeconds is how long a text track survives without a matching reading (default 2).
	MaxGapSeconds float64 `json:"max_gap_seconds"`
	// MinHits is how many frames must read the text before it counts (default 2).
	// OCR noise rarely repeats, so this is the main flicker filter.
	MinHits int `json:"min_hits"`
	// MinChars drops readings shorter than this after trimming (default 2).
	MinChars int `json:"min_chars"`
	// MinConf drops readings below this confidence (default 0: the server filters).
	MinConf float64 `json:"min_conf"`
}

func (c TextConfig) withDefaults() TextConfig {
	if c.MinSimilarity <= 0 {
		c.MinSimilarity = 0.7
	}
	if c.MinIoU <= 0 {
		c.MinIoU = 0.3
	}
	if c.MaxGapSeconds <= 0 {
		c.MaxGapSeconds = 2
	}
	if c.MinHits < 1 {
		c.MinHits = 2
	}
	if c.MinChars < 1 {
		c.MinChars = 2
	}
	return c
}

// TextTracker merges repeated OCR readings into text tracks. Not safe for concurrent use.
type TextTracker struct {
	cfg      TextConfig
	active   []*TextTrack
	done     []*TextTrack
	lastTime float64
	started  bool
}

// NewText builds a text tracker.
func NewText(cfg TextConfig) *TextTracker { return &TextTracker{cfg: cfg.withDefaults()} }

// Update feeds one frame's OCR lines. Frames must come in time order.
func (t *TextTracker) Update(f Frame, lines []TextLine) error {
	if t.started && f.Time < t.lastTime {
		return fmt.Errorf("tracker: frame %d at %.3fs is earlier than the previous frame (%.3fs)", f.ID, f.Time, t.lastTime)
	}
	t.started, t.lastTime = true, f.Time

	alive := t.active[:0]
	for _, tr := range t.active {
		if f.Time-tr.Last().Time <= t.cfg.MaxGapSeconds {
			alive = append(alive, tr)
		} else {
			t.retire(tr)
		}
	}
	t.active = alive

	type pair struct {
		li, ti int
		score  float64
	}
	var kept []TextLine
	for _, l := range lines {
		l.Text = strings.TrimSpace(l.Text)
		if utf8.RuneCountInString(l.Text) >= t.cfg.MinChars && l.Conf >= t.cfg.MinConf && l.Box.Area() > 0 {
			kept = append(kept, l)
		}
	}
	var pairs []pair
	for li, l := range kept {
		for ti, tr := range t.active {
			iou := IoU(l.Box, tr.Box())
			if iou < t.cfg.MinIoU {
				continue
			}
			sim := Similarity(l.Text, tr.Text)
			if sim < t.cfg.MinSimilarity {
				continue
			}
			pairs = append(pairs, pair{li, ti, sim + iou})
		}
	}
	// Greedy by score: with so few lines per frame the optimal assignment rarely differs.
	sort.Slice(pairs, func(a, b int) bool { return pairs[a].score > pairs[b].score })
	usedL, usedT := map[int]bool{}, map[int]bool{}
	for _, p := range pairs {
		if usedL[p.li] || usedT[p.ti] {
			continue
		}
		usedL[p.li], usedT[p.ti] = true, true
		l := kept[p.li]
		t.active[p.ti].add(TextObs{Frame: f.ID, Ticks: f.Ticks, Time: f.Time, Text: l.Text, Conf: l.Conf, Box: l.Box})
	}
	for li, l := range kept {
		if usedL[li] {
			continue
		}
		tr := &TextTrack{}
		tr.add(TextObs{Frame: f.ID, Ticks: f.Ticks, Time: f.Time, Text: l.Text, Conf: l.Conf, Box: l.Box})
		t.active = append(t.active, tr)
	}
	return nil
}

func (t *TextTracker) retire(tr *TextTrack) {
	if len(tr.Obs) >= t.cfg.MinHits {
		t.done = append(t.done, tr)
	}
}

// Finish ends every track and returns the confirmed ones in order of first appearance.
func (t *TextTracker) Finish() []*TextTrack {
	for _, tr := range t.active {
		t.retire(tr)
	}
	t.active = nil
	out := t.done
	t.done = nil
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].First(), out[j].First()
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		return a.Box.Y < b.Box.Y
	})
	for i, tr := range out {
		tr.ID = i + 1
	}
	return out
}

// Normalize lowercases, drops punctuation and collapses spaces, so "Intro to ML!" and
// "intro to  ml" compare equal.
func Normalize(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		case unicode.IsSpace(r):
			space = true
		}
	}
	return b.String()
}

// Similarity is 1 - edit distance / longer length over the normalised texts: 1 for equal
// texts, 0 for nothing in common. Two empty texts are not similar.
func Similarity(a, b string) float64 {
	x, y := []rune(Normalize(a)), []rune(Normalize(b))
	if len(x) == 0 && len(y) == 0 {
		return 0
	}
	n := max(len(x), len(y))
	return 1 - float64(levenshtein(x, y))/float64(n)
}

func levenshtein(a, b []rune) int {
	if len(a) < len(b) {
		a, b = b, a
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
