package tracker

import (
	"math"
	"math/rand"
	"testing"
)

func TestAssignMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 300; trial++ {
		n, m := 1+rng.Intn(5), 1+rng.Intn(5)
		cost := make([][]float64, n)
		for i := range cost {
			cost[i] = make([]float64, m)
			for j := range cost[i] {
				cost[i][j] = float64(rng.Intn(20))
			}
		}
		got := Assign(cost)
		gotCost, assigned := 0.0, 0
		seen := map[int]bool{}
		for i, j := range got {
			if j < 0 {
				continue
			}
			if seen[j] {
				t.Fatalf("column %d used twice: %v", j, got)
			}
			seen[j] = true
			gotCost += cost[i][j]
			assigned++
		}
		if assigned != min(n, m) {
			t.Fatalf("assigned %d of %d: %v", assigned, min(n, m), got)
		}
		if want := brute(cost); math.Abs(gotCost-want) > 1e-9 {
			t.Fatalf("cost %v want %v for %v -> %v", gotCost, want, cost, got)
		}
	}
}

// brute finds the cheapest assignment of min(n,m) pairs by trying everything.
func brute(cost [][]float64) float64 {
	n, m := len(cost), len(cost[0])
	best := math.Inf(1)
	var rec func(i int, used int, sum float64, count int)
	rec = func(i int, used int, sum float64, count int) {
		if count == min(n, m) {
			best = math.Min(best, sum)
			return
		}
		if i == n {
			return
		}
		rec(i+1, used, sum, count) // leave row i unassigned (only matters when n > m)
		for j := 0; j < m; j++ {
			if used&(1<<j) == 0 {
				rec(i+1, used|1<<j, sum+cost[i][j], count+1)
			}
		}
	}
	rec(0, 0, 0, 0)
	return best
}

func TestAssignEmpty(t *testing.T) {
	if Assign(nil) != nil {
		t.Fatal("nil in, nil out")
	}
	if got := Assign([][]float64{{}}); len(got) != 1 || got[0] != -1 {
		t.Fatalf("no columns: %v", got)
	}
}

func TestIoU(t *testing.T) {
	a := Box{0, 0, 0.5, 0.5}
	if IoU(a, a) != 1 {
		t.Fatal("same box")
	}
	if IoU(a, Box{0.5, 0.5, 0.2, 0.2}) != 0 {
		t.Fatal("touching corners")
	}
	if got := IoU(a, Box{0.25, 0, 0.5, 0.5}); math.Abs(got-1.0/3) > 1e-9 {
		t.Fatalf("half overlap: %v", got)
	}
	if IoU(Box{}, a) != 0 {
		t.Fatal("empty box")
	}
}

// walker returns a person box at time t moving right at speed (fraction of frame/second).
func walker(x0, speed, t float64) Box { return Box{x0 + speed*t, 0.3, 0.1, 0.4} }

func feed(t *testing.T, tr *Tracker, times []float64, boxes func(i int, tm float64) []Det) {
	t.Helper()
	for i, tm := range times {
		if err := tr.Update(Frame{ID: i, Ticks: int64(tm * 1000), Time: tm}, boxes(i, tm)); err != nil {
			t.Fatal(err)
		}
	}
}

func seconds(from, to, step float64) []float64 {
	var out []float64
	for t := from; t <= to+1e-9; t += step {
		out = append(out, t)
	}
	return out
}

func TestOnePersonIsOneEntity(t *testing.T) {
	tr := New(Config{})
	feed(t, tr, seconds(0, 10, 1), func(i int, tm float64) []Det {
		return []Det{{Class: "person", Conf: 0.9, Box: walker(0.1, 0.05, tm)}}
	})
	tracks := tr.Finish()
	if len(tracks) != 1 {
		t.Fatalf("%d tracks, want 1", len(tracks))
	}
	got := tracks[0]
	if got.ID != 1 || got.Seq != 1 || got.Name() != "person_01" || len(got.Obs) != 11 || got.Duration() != 10 {
		t.Fatalf("track: id=%d name=%s obs=%d dur=%v", got.ID, got.Name(), len(got.Obs), got.Duration())
	}
}

func TestFastWalkerKeepsIdentityAcrossOneSecondGaps(t *testing.T) {
	// 0.12 of the frame per second with a 0.1-wide box: consecutive boxes barely
	// overlap (IoU < 0.3), so only the centre-distance gate and prediction hold the track.
	tr := New(Config{})
	feed(t, tr, seconds(0, 8, 1), func(i int, tm float64) []Det {
		return []Det{{Class: "person", Conf: 0.9, Box: walker(0.05, 0.12, tm)}}
	})
	if n := len(tr.Finish()); n != 1 {
		t.Fatalf("%d tracks, want 1", n)
	}
}

func TestTwoPeopleKeepSeparateIdentities(t *testing.T) {
	tr := New(Config{})
	feed(t, tr, seconds(0, 6, 0.5), func(i int, tm float64) []Det {
		return []Det{
			{Class: "person", Conf: 0.9, Box: Box{0.1 + 0.02*tm, 0.3, 0.1, 0.4}},
			{Class: "person", Conf: 0.9, Box: Box{0.8 - 0.02*tm, 0.3, 0.1, 0.4}},
		}
	})
	tracks := tr.Finish()
	if len(tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(tracks))
	}
	if tracks[0].Seq != 1 || tracks[1].Seq != 2 || tracks[0].Name() != "person_01" || tracks[1].Name() != "person_02" {
		t.Fatalf("names %s %s", tracks[0].Name(), tracks[1].Name())
	}
	for _, trk := range tracks {
		if len(trk.Obs) != 13 {
			t.Fatalf("%s has %d obs", trk.Name(), len(trk.Obs))
		}
	}
}

func TestPeopleCrossingKeepIdentity(t *testing.T) {
	// Two people walk towards each other and pass; with prediction they must not swap.
	tr := New(Config{})
	feed(t, tr, seconds(0, 10, 0.5), func(i int, tm float64) []Det {
		return []Det{
			{Class: "person", Conf: 0.9, Box: Box{0.05 + 0.08*tm, 0.30, 0.08, 0.35}},
			{Class: "person", Conf: 0.9, Box: Box{0.85 - 0.08*tm, 0.32, 0.08, 0.35}},
		}
	})
	tracks := tr.Finish()
	if len(tracks) != 2 {
		t.Fatalf("%d tracks, want 2", len(tracks))
	}
	for _, trk := range tracks {
		// Each track must move consistently in one direction.
		dir := trk.Obs[1].Box.X - trk.Obs[0].Box.X
		for i := 1; i < len(trk.Obs); i++ {
			if (trk.Obs[i].Box.X-trk.Obs[i-1].Box.X)*dir <= 0 {
				t.Fatalf("%s changed direction at obs %d: identity swap", trk.Name(), i)
			}
		}
	}
}

func TestFlickerIsDropped(t *testing.T) {
	tr := New(Config{})
	feed(t, tr, seconds(0, 5, 1), func(i int, tm float64) []Det {
		if i == 2 {
			return []Det{{Class: "chair", Conf: 0.4, Box: Box{0.5, 0.5, 0.1, 0.1}}}
		}
		return nil
	})
	if n := len(tr.Finish()); n != 0 {
		t.Fatalf("a one-frame detection became %d tracks", n)
	}
}

func TestMissedFramesAreBridgedButLongGapsSplit(t *testing.T) {
	tr := New(Config{MaxGapSeconds: 1.5})
	present := func(tm float64) bool { return tm < 3 || (tm > 4 && tm < 8) || tm > 14 }
	feed(t, tr, seconds(0, 20, 1), func(i int, tm float64) []Det {
		if !present(tm) {
			return nil
		}
		return []Det{{Class: "person", Conf: 0.9, Box: Box{0.4, 0.3, 0.1, 0.4}}}
	})
	tracks := tr.Finish()
	// Last seen at 2 and next at 5 is a 3 s gap, more than 1.5 s: different tracks.
	if len(tracks) != 3 {
		t.Fatalf("%d tracks, want 3", len(tracks))
	}
	tr2 := New(Config{MaxGapSeconds: 2.5})
	feed(t, tr2, seconds(0, 6, 1), func(i int, tm float64) []Det {
		if i == 3 { // a single missed frame
			return nil
		}
		return []Det{{Class: "person", Conf: 0.9, Box: Box{0.4, 0.3, 0.1, 0.4}}}
	})
	if got := tr2.Finish(); len(got) != 1 || len(got[0].Obs) != 6 {
		t.Fatalf("a single missed frame split the track: %d tracks", len(got))
	}
}

func TestClassesAreSeparate(t *testing.T) {
	tr := New(Config{})
	feed(t, tr, seconds(0, 4, 1), func(i int, tm float64) []Det {
		return []Det{
			{Class: "person", Conf: 0.9, Box: Box{0.4, 0.3, 0.1, 0.4}},
			{Class: "cell phone", Conf: 0.8, Box: Box{0.42, 0.5, 0.04, 0.06}},
		}
	})
	tracks := tr.Finish()
	if len(tracks) != 2 || tracks[0].Class == tracks[1].Class {
		t.Fatalf("tracks: %+v", tracks)
	}
	names := map[string]bool{tracks[0].Name(): true, tracks[1].Name(): true}
	if !names["person_01"] || !names["cell_phone_01"] {
		t.Fatalf("names %v", names)
	}
}

func TestEnterAndLeaveTimesAreTheFirstAndLastDetection(t *testing.T) {
	tr := New(Config{})
	feed(t, tr, seconds(0, 20, 1), func(i int, tm float64) []Det {
		if tm < 5 || tm > 12 {
			return nil
		}
		return []Det{{Class: "person", Conf: 0.9, Box: walker(0.0, 0.05, tm-5)}}
	})
	got := tr.Finish()
	if len(got) != 1 || got[0].First().Time != 5 || got[0].Last().Time != 12 {
		t.Fatalf("%+v", got)
	}
}

func TestTimeMustNotGoBackwards(t *testing.T) {
	tr := New(Config{})
	if err := tr.Update(Frame{ID: 1, Time: 5}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(Frame{ID: 0, Time: 4}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestBreakOnCutEndsTracks(t *testing.T) {
	run := func(cut bool) int {
		tr := New(Config{BreakOnCut: cut})
		feed(t, tr, seconds(0, 6, 1), func(i int, tm float64) []Det {
			return []Det{{Class: "person", Conf: 0.9, Box: Box{0.4, 0.3, 0.1, 0.4}}}
		})
		// replay with a cut flag at frame 3
		tr = New(Config{BreakOnCut: cut})
		for i, tm := range seconds(0, 6, 1) {
			_ = tr.Update(Frame{ID: i, Time: tm, Cut: i == 3}, []Det{{Class: "person", Conf: 0.9, Box: Box{0.4, 0.3, 0.1, 0.4}}})
		}
		return len(tr.Finish())
	}
	if run(false) != 1 || run(true) != 2 {
		t.Fatalf("no-break %d, break %d", run(false), run(true))
	}
}

func TestDeterministic(t *testing.T) {
	build := func() []string {
		tr := New(Config{})
		feed(t, tr, seconds(0, 5, 0.5), func(i int, tm float64) []Det {
			return []Det{
				{Class: "person", Conf: 0.9, Box: Box{0.1, 0.3, 0.1, 0.4}},
				{Class: "chair", Conf: 0.9, Box: Box{0.6, 0.6, 0.1, 0.2}},
				{Class: "person", Conf: 0.8, Box: Box{0.7, 0.3, 0.1, 0.4}},
			}
		})
		var out []string
		for _, trk := range tr.Finish() {
			out = append(out, trk.Name())
		}
		return out
	}
	a, b := build(), build()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("%v vs %v", a, b)
		}
	}
}
