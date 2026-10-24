package tracker

import "math"

// forbidden is the cost of a pairing that must never be chosen. Assign may still hand
// such a pair back when no other column is free; callers discard those.
const forbidden = 1e9

// Assign solves the rectangular assignment problem (Hungarian / Kuhn-Munkres with
// potentials, O(n^2 m)): cost[i][j] is the price of giving column j to row i. It
// returns, for every row, the column assigned to it, or -1 when there are more rows
// than columns. The total cost of the assignment is minimal.
func Assign(cost [][]float64) []int {
	n := len(cost)
	if n == 0 {
		return nil
	}
	m := len(cost[0])
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	if m == 0 {
		return out
	}
	if n > m { // solve the transpose, then invert
		t := make([][]float64, m)
		for j := range t {
			t[j] = make([]float64, n)
			for i := 0; i < n; i++ {
				t[j][i] = cost[i][j]
			}
		}
		for j, i := range Assign(t) {
			if i >= 0 {
				out[i] = j
			}
		}
		return out
	}

	// n <= m. Arrays are 1-based; index 0 is a sentinel.
	inf := math.Inf(1)
	u := make([]float64, n+1)
	v := make([]float64, m+1)
	p := make([]int, m+1)   // p[j] = row matched to column j
	way := make([]int, m+1) // previous column on the augmenting path
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		minv := make([]float64, m+1)
		used := make([]bool, m+1)
		for j := range minv {
			minv[j] = inf
		}
		for {
			used[j0] = true
			i0, delta, j1 := p[j0], inf, 0
			for j := 1; j <= m; j++ {
				if used[j] {
					continue
				}
				cur := cost[i0-1][j-1] - u[i0] - v[j]
				if cur < minv[j] {
					minv[j], way[j] = cur, j0
				}
				if minv[j] < delta {
					delta, j1 = minv[j], j
				}
			}
			for j := 0; j <= m; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
		}
	}
	for j := 1; j <= m; j++ {
		if p[j] != 0 {
			out[p[j]-1] = j - 1
		}
	}
	return out
}
