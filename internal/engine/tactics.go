package engine

// Cheap, exact tactical queries used by the AI, the hint and the puzzle
// generator. "Exact" matters: a line can only be completed by the mover's
// placed stone, and captures only depend on which enemy stones sit next to
// the placement with the edge or a gap behind them.

// captureCounts fills cnt[e] with how many enemy stones a placement by p at
// empty cell e would push off the board or into a gap.
func (s *State) captureCounts(p int8, cnt *[NumCells]int8) {
	*cnt = [NumCells]int8{}
	foe := Other(p)
	for c := 0; c < NumCells; c++ {
		if s.Cells[c] != foe {
			continue
		}
		for d := 0; d < 6; d++ {
			beyond := neighbor[c][d]
			if beyond >= 0 && s.Cells[beyond] != Gap {
				continue
			}
			from := neighbor[c][(d+3)%6]
			if from >= 0 && s.Cells[from] == Empty {
				cnt[from]++
			}
		}
	}
}

// WinningMoves appends to dst every move that wins immediately for the
// player to move (lines first, then captures) and returns it.
func (s *State) WinningMoves(dst []int) []int {
	dst = dst[:0]
	if s.Winner != NoWinner {
		return dst
	}
	p := s.ToMove
	var mark [NumCells]bool
	for li := range lines {
		l := lines[li]
		own, empty := 0, -1
		for _, c := range l {
			switch s.Cells[c] {
			case p:
				own++
			case Empty:
				empty = c
			}
		}
		if own == LineLength-1 && empty >= 0 && !mark[empty] {
			mark[empty] = true
			dst = append(dst, empty)
		}
	}
	if c, ok := s.lastEmpty(); ok && !mark[c] {
		// Filling the last column ends the game on a stone count.
		n := *s
		n.apply(c, nil)
		if n.Winner == p {
			mark[c] = true
			dst = append(dst, c)
		}
	}
	need := int8(CaptureGoal) - s.Captured[p]
	if need <= 6 {
		var cnt [NumCells]int8
		s.captureCounts(p, &cnt)
		for c := 0; c < NumCells; c++ {
			if cnt[c] >= need && !mark[c] {
				mark[c] = true
				dst = append(dst, c)
			}
		}
	}
	return dst
}

// lastEmpty returns the only empty column, if exactly one is left.
func (s *State) lastEmpty() (int, bool) {
	at, n := -1, 0
	for c := 0; c < NumCells; c++ {
		if s.Cells[c] == Empty {
			at = c
			n++
			if n > 1 {
				return -1, false
			}
		}
	}
	return at, n == 1
}

// hasWinningMove is WinningMoves without the allocation.
func (s *State) hasWinningMove() bool {
	if s.Winner != NoWinner {
		return false
	}
	p := s.ToMove
	if c, ok := s.lastEmpty(); ok {
		n := *s
		n.apply(c, nil)
		if n.Winner == p {
			return true
		}
	}
	for li := range lines {
		l := lines[li]
		own, empty := 0, 0
		for _, c := range l {
			switch s.Cells[c] {
			case p:
				own++
			case Empty:
				empty++
			}
		}
		if own == LineLength-1 && empty == 1 {
			return true
		}
	}
	need := int8(CaptureGoal) - s.Captured[p]
	if need <= 6 {
		foe := Other(p)
		var cnt [NumCells]int8
		for c := 0; c < NumCells; c++ {
			if s.Cells[c] != foe {
				continue
			}
			for d := 0; d < 6; d++ {
				beyond := neighbor[c][d]
				if beyond >= 0 && s.Cells[beyond] != Gap {
					continue
				}
				from := neighbor[c][(d+3)%6]
				if from >= 0 && s.Cells[from] == Empty {
					cnt[from]++
					if cnt[from] >= need {
						return true
					}
				}
			}
		}
	}
	return false
}

// SafeMoves returns the legal moves after which the opponent has no
// immediate win (and that do not hand them a line through our own push).
// If every move loses at once it returns all legal moves.
func (s *State) SafeMoves(dst []int) []int {
	all := s.LegalMoves(nil)
	dst = dst[:0]
	for _, m := range all {
		n := *s
		n.apply(m, nil)
		if n.Winner == s.ToMove {
			dst = append(dst, m)
			continue
		}
		if n.Winner != NoWinner && n.Winner != Draw {
			continue // our push completed their line
		}
		if !n.hasWinningMove() {
			dst = append(dst, m)
		}
	}
	if len(dst) == 0 {
		return append(dst, all...)
	}
	return dst
}

// ThreatCells returns the empty cells where player p would complete a line
// (used by the UI to highlight danger, and by the easy AI).
func (s *State) ThreatCells(p int8) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, l := range lines {
		own, empty := 0, -1
		for _, c := range l {
			switch s.Cells[c] {
			case p:
				own++
			case Empty:
				empty = c
			}
		}
		if own == LineLength-1 && empty >= 0 && !seen[empty] {
			seen[empty] = true
			out = append(out, empty)
		}
	}
	return out
}
