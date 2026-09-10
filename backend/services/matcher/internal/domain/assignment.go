package domain

import "math"

// Assign solves the assignment problem: each row gets at most one column, no
// column is used twice, and the total cost is as small as it can be. It returns
// the column given to each row, or -1 for a row left without one.
//
// A cost of +Inf marks a pair that must never be chosen — a driver who was not
// a candidate for that rider, or one Valhalla found no road to. The solver
// first maximises how many rows are assigned at all, then minimises the cost
// of doing so: a rider left unmatched to save another rider thirty seconds is
// not a trade anyone would make.
//
// The Hungarian algorithm with potentials, O(rows² × cols). A batch is bounded
// to a few dozen requests and drivers, where that is well under a millisecond.
func Assign(cost [][]float64) []int {
	rows := len(cost)
	if rows == 0 {
		return nil
	}
	cols := len(cost[0])
	result := make([]int, rows)
	for i := range result {
		result[i] = -1
	}
	if cols == 0 {
		return result
	}

	// The algorithm below needs at least as many columns as rows.
	if rows > cols {
		transposed := make([][]float64, cols)
		for j := range transposed {
			transposed[j] = make([]float64, rows)
			for i := range cost {
				transposed[j][i] = cost[i][j]
			}
		}
		for j, i := range Assign(transposed) {
			if i >= 0 {
				result[i] = j
			}
		}
		return result
	}

	// +Inf would poison the potentials, so a forbidden pair is given a cost
	// larger than every finite assignment put together, and dropped afterwards.
	// That also makes "assign as many as possible" win over "assign cheaply".
	largest := 1.0
	for _, row := range cost {
		for _, c := range row {
			if forbidden(c) {
				continue
			}
			largest = math.Max(largest, math.Abs(c))
		}
	}
	penalty := largest*float64(rows+1) + 1
	at := func(i, j int) float64 {
		if c := cost[i-1][j-1]; !forbidden(c) {
			return c
		}
		return penalty
	}

	// 1-indexed, with row and column 0 as the algorithm's sentinels.
	u := make([]float64, rows+1)
	v := make([]float64, cols+1)
	owner := make([]int, cols+1)
	way := make([]int, cols+1)
	for i := 1; i <= rows; i++ {
		owner[0] = i
		column := 0
		minv := make([]float64, cols+1)
		for j := range minv {
			minv[j] = math.Inf(1)
		}
		used := make([]bool, cols+1)
		for {
			used[column] = true
			row, delta, next := owner[column], math.Inf(1), 0
			for j := 1; j <= cols; j++ {
				if used[j] {
					continue
				}
				if reduced := at(row, j) - u[row] - v[j]; reduced < minv[j] {
					minv[j], way[j] = reduced, column
				}
				if minv[j] < delta {
					delta, next = minv[j], j
				}
			}
			for j := 0; j <= cols; j++ {
				if used[j] {
					u[owner[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			column = next
			if owner[column] == 0 {
				break
			}
		}
		for column != 0 {
			previous := way[column]
			owner[column] = owner[previous]
			column = previous
		}
	}

	for j := 1; j <= cols; j++ {
		if i := owner[j]; i > 0 && !forbidden(cost[i-1][j-1]) {
			result[i-1] = j - 1
		}
	}
	return result
}

func forbidden(c float64) bool { return math.IsInf(c, 0) || math.IsNaN(c) }
