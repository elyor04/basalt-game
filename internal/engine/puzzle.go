package engine

import (
	"hash/fnv"
	"math/rand/v2"
)

// Puzzle is a "win in two" position: the side to move can force a win with
// its second move whatever the opponent replies, and has exactly one first
// move that does it.
type Puzzle struct {
	Key      string `json:"key"`
	State    State  `json:"-"`
	Solution int    `json:"-"`
	Attempts int    `json:"-"`
}

// GeneratePuzzle derives a puzzle deterministically from key (the UTC date).
// It returns false only if no puzzle was found within the attempt budget,
// which the tests show does not happen in practice.
func GeneratePuzzle(key string) (Puzzle, bool) {
	h := fnv.New64a()
	_, _ = h.Write([]byte("basalt-puzzle:" + key))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, seed^0xdeadbeefcafef00d))
	moves := make([]int, 0, NumCells+1)
	for attempt := 1; attempt <= 4000; attempt++ {
		s := NewState()
		target := 14 + rng.IntN(14)
		ok := true
		for s.Ply < int16(target) {
			moves = s.SafeMoves(moves)
			// Keep the game going.
			cand := moves[:0:0]
			for _, m := range moves {
				n := s
				n.apply(m, nil)
				if n.Winner == NoWinner {
					cand = append(cand, m)
				}
			}
			if len(cand) == 0 {
				ok = false
				break
			}
			// Mostly play near existing stones so positions look natural.
			m := pickNear(&s, cand, rng)
			s.apply(m, nil)
		}
		if !ok || s.Winner != NoWinner {
			continue
		}
		if sol, found := uniqueWinInTwo(s); found {
			return Puzzle{Key: key, State: s, Solution: sol, Attempts: attempt}, true
		}
	}
	return Puzzle{Key: key}, false
}

func pickNear(s *State, cand []int, rng *rand.Rand) int {
	if rng.IntN(4) == 0 || s.Ply == 0 {
		return cand[rng.IntN(len(cand))]
	}
	near := make([]int, 0, len(cand))
	for _, c := range cand {
		for d := 0; d < 6; d++ {
			x := neighbor[c][d]
			if x >= 0 && (s.Cells[x] == Lava || s.Cells[x] == Ash) {
				near = append(near, c)
				break
			}
		}
	}
	if len(near) == 0 {
		return cand[rng.IntN(len(cand))]
	}
	return near[rng.IntN(len(near))]
}

// uniqueWinInTwo reports the single first move that forces a win next turn,
// if there is exactly one and there is no win in one.
func uniqueWinInTwo(s State) (int, bool) {
	if s.hasWinningMove() {
		return -1, false
	}
	sol := -1
	for m := 0; m < NumCells; m++ {
		if s.Cells[m] != Empty {
			continue
		}
		if ForcesWin(s, m) {
			if sol >= 0 {
				return -1, false // more than one answer: not a clean puzzle
			}
			sol = m
		}
	}
	return sol, sol >= 0
}

// ForcesWin reports whether first move m by the side to move wins by force
// on its next move, whatever the opponent replies.
func ForcesWin(s State, m int) bool {
	p := s.ToMove
	s1 := s
	s1.apply(m, nil)
	if s1.Winner == p {
		return true
	}
	if s1.Winner != NoWinner {
		return false
	}
	for r := 0; r < NumCells; r++ {
		if s1.Cells[r] != Empty {
			continue
		}
		s2 := s1
		s2.apply(r, nil)
		if s2.Winner == p {
			continue
		}
		if s2.Winner != NoWinner || !s2.hasWinningMove() {
			return false
		}
	}
	return true
}

// Defend picks the opponent's most stubborn reply in a puzzle: a win if it
// has one, otherwise the reply that leaves the fewest immediate wins.
func Defend(s State) int {
	best, bestScore := -1, 1<<30
	var buf []int
	for r := 0; r < NumCells; r++ {
		if s.Cells[r] != Empty {
			continue
		}
		n := s
		n.apply(r, nil)
		if n.Winner == s.ToMove {
			return r
		}
		score := 0
		if n.Winner != NoWinner {
			score = 1000 // handing the win over is the worst reply
		} else {
			buf = n.WinningMoves(buf)
			score = len(buf)
		}
		if score < bestScore {
			best, bestScore = r, score
		}
	}
	return best
}
