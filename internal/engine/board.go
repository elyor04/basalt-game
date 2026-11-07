// Package engine holds the rules of Basalt and its AI. It is the single
// source of truth for the game: the UI only ever sends a cell index and
// renders the state and events that come back.
package engine

// Radius is the board radius in hexes; the board has NumCells cells.
const Radius = 4

// NumCells is the number of hexes on a radius-4 board (3R²+3R+1).
const NumCells = 3*Radius*Radius + 3*Radius + 1

// Axial direction vectors, clockwise starting east. Opposite(d) = (d+3)%6.
var dirQ = [6]int{1, 1, 0, -1, -1, 0}
var dirR = [6]int{0, -1, -1, 0, 1, 1}

// Coord is an axial hex coordinate.
type Coord struct {
	Q int `json:"q"`
	R int `json:"r"`
}

var (
	coords  [NumCells]Coord
	indexOf = map[Coord]int{}
	// neighbor[c][d] is the cell one step from c in direction d, or -1 when
	// that step leaves the board.
	neighbor [NumCells][6]int
	// lines lists every run of LineLength cells along one of the three axes.
	lines [][]int
	// linesAt[c] lists the indices into lines of every run through c.
	linesAt [NumCells][]int
	// rim[c] reports whether c has at least one off-board neighbour.
	rim [NumCells]bool
)

func onBoard(q, r int) bool {
	s := -q - r
	return abs(q) <= Radius && abs(r) <= Radius && abs(s) <= Radius
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func init() {
	// Cells are numbered row by row (r from -R to R, q ascending), which is
	// also the order the frontend uses.
	i := 0
	for r := -Radius; r <= Radius; r++ {
		for q := -Radius; q <= Radius; q++ {
			if onBoard(q, r) {
				coords[i] = Coord{q, r}
				indexOf[Coord{q, r}] = i
				i++
			}
		}
	}
	for c := 0; c < NumCells; c++ {
		for d := 0; d < 6; d++ {
			q, r := coords[c].Q+dirQ[d], coords[c].R+dirR[d]
			if onBoard(q, r) {
				neighbor[c][d] = indexOf[Coord{q, r}]
			} else {
				neighbor[c][d] = -1
				rim[c] = true
			}
		}
	}
	buildLines(LineLength)
}

// CoordOf returns the axial coordinate of a cell index.
func CoordOf(c int) Coord { return coords[c] }

// IndexOf returns the cell index of an axial coordinate, or -1.
func IndexOf(q, r int) int {
	if i, ok := indexOf[Coord{q, r}]; ok {
		return i
	}
	return -1
}

// Neighbor returns the cell next to c in direction d (0..5), or -1.
func Neighbor(c, d int) int { return neighbor[c][d] }

// IsRim reports whether c lies on the outer ring.
func IsRim(c int) bool { return rim[c] }

// Lines returns every winning-length straight run (for tests and tools).
func Lines() [][]int { return lines }

// LineLength is how many stones in a straight row win. Tuned by self-play.
var LineLength = 5

// buildLines precomputes every straight run of n cells. Three axes: east
// (0), north-west (2) and south-west (4) are pairwise non-opposite, so every
// run is found exactly once.
func buildLines(n int) {
	LineLength = n
	lines = nil
	linesAt = [NumCells][]int{}
	for _, d := range []int{0, 2, 4} {
		for c := 0; c < NumCells; c++ {
			run := make([]int, n)
			run[0] = c
			ok := true
			for k := 1; k < n; k++ {
				x := neighbor[run[k-1]][d]
				if x < 0 {
					ok = false
					break
				}
				run[k] = x
			}
			if ok {
				idx := len(lines)
				lines = append(lines, run)
				for _, x := range run {
					linesAt[x] = append(linesAt[x], idx)
				}
			}
		}
	}
}
