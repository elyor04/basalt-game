package engine

import (
	"testing"
	"time"
)

func TestDailyPuzzles(t *testing.T) {
	days := 60
	if testing.Short() {
		days = 10
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var worst time.Duration
	totalAttempts := 0
	for i := 0; i < days; i++ {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		t0 := time.Now()
		pz, ok := GeneratePuzzle(key)
		el := time.Since(t0)
		if el > worst {
			worst = el
		}
		if !ok {
			t.Fatalf("%s: no puzzle", key)
		}
		totalAttempts += pz.Attempts
		s := pz.State
		if s.Winner != NoWinner || s.hasWinningMove() {
			t.Fatalf("%s: position is already decided", key)
		}
		if !ForcesWin(s, pz.Solution) {
			t.Fatalf("%s: solution %d does not force a win", key, pz.Solution)
		}
		for m := 0; m < NumCells; m++ {
			if m != pz.Solution && s.Cells[m] == Empty && ForcesWin(s, m) {
				t.Fatalf("%s: second solution %d", key, m)
			}
		}
		// Following the defender's best replies, the solver still wins.
		s1, _, _ := s.Play(pz.Solution)
		r := Defend(s1)
		s2, _, _ := s1.Play(r)
		if s2.Winner != s.ToMove {
			w := s2.WinningMoves(nil)
			if len(w) == 0 {
				t.Fatalf("%s: no finishing move after defence", key)
			}
			s3, _, _ := s2.Play(w[0])
			if s3.Winner != s.ToMove {
				t.Fatalf("%s: finishing move did not win", key)
			}
		}
		again, _ := GeneratePuzzle(key)
		if again.State != pz.State || again.Solution != pz.Solution {
			t.Fatalf("%s: generation is not deterministic", key)
		}
	}
	t.Logf("%d puzzles, avg %.1f attempts, slowest %v", days, float64(totalAttempts)/float64(days), worst)
}
