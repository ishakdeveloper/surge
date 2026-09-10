package domain_test

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ishakdeveloper/surge/services/matcher/internal/domain"
)

var inf = math.Inf(1)

// The case batching exists for. Rider A is close to both cars, rider B only to
// the first. Greedy, in arrival order, gives A the first car and sends the
// second one across town to B: 1 + 100. Solved together it is 2 + 1.
func TestAssignAvoidsTheGreedyTrap(t *testing.T) {
	got := domain.Assign([][]float64{
		{1, 2},
		{1, 100},
	})
	if !slices.Equal(got, []int{1, 0}) {
		t.Errorf("Assign = %v, want [1 0]", got)
	}
}

func TestAssignShapes(t *testing.T) {
	cases := []struct {
		name string
		cost [][]float64
		want []int
	}{
		{"more drivers than riders", [][]float64{{5, 1, 9}}, []int{1}},
		{"more riders than drivers", [][]float64{{1}, {2}}, []int{0, -1}},
		{"a forbidden pair is never chosen", [][]float64{{inf, 3}, {inf, inf}}, []int{1, -1}},
		{"a rider with no options is left unassigned", [][]float64{{inf}}, []int{-1}},
		{"no drivers", [][]float64{{}}, []int{-1}},
		// Two assigned at a higher total beats one assigned cheaply.
		{"matching more riders wins over cost", [][]float64{{1, 50}, {2, inf}}, []int{1, 0}},
	}
	for _, c := range cases {
		if got := domain.Assign(c.cost); !slices.Equal(got, c.want) {
			t.Errorf("%s: Assign = %v, want %v", c.name, got, c.want)
		}
	}
}

// Against brute force on small random matrices, with forbidden pairs: the same
// number of riders matched, at the same total cost.
func TestAssignMatchesBruteForce(t *testing.T) {
	random := rand.New(rand.NewPCG(7, 11))
	for trial := range 500 {
		rows, cols := 1+random.IntN(5), 1+random.IntN(5)
		cost := make([][]float64, rows)
		for i := range cost {
			cost[i] = make([]float64, cols)
			for j := range cost[i] {
				if random.Float64() < 0.2 {
					cost[i][j] = inf
				} else {
					cost[i][j] = float64(random.IntN(100))
				}
			}
		}

		got := domain.Assign(cost)
		gotCount, gotCost := score(cost, got)
		wantCount, wantCost := best(cost)
		if gotCount != wantCount || gotCost != wantCost {
			t.Fatalf("trial %d: %v → %v matched %d for %v, brute force matches %d for %v",
				trial, cost, got, gotCount, gotCost, wantCount, wantCost)
		}
		used := map[int]bool{}
		for _, j := range got {
			if j >= 0 && used[j] {
				t.Fatalf("trial %d: column %d given twice in %v", trial, j, got)
			}
			used[j] = true
		}
	}
}

func score(cost [][]float64, assigned []int) (int, float64) {
	count, total := 0, 0.0
	for i, j := range assigned {
		if j >= 0 {
			count++
			total += cost[i][j]
		}
	}
	return count, total
}

// best tries every assignment: most rows matched, then least cost.
func best(cost [][]float64) (int, float64) {
	bestCount, bestCost := -1, 0.0
	used := make([]bool, len(cost[0]))
	var walk func(row, count int, total float64)
	walk = func(row, count int, total float64) {
		if row == len(cost) {
			if count > bestCount || (count == bestCount && total < bestCost) {
				bestCount, bestCost = count, total
			}
			return
		}
		walk(row+1, count, total)
		for j, c := range cost[row] {
			if used[j] || math.IsInf(c, 1) {
				continue
			}
			used[j] = true
			walk(row+1, count+1, total+c)
			used[j] = false
		}
	}
	walk(0, 0, 0)
	return bestCount, bestCost
}
