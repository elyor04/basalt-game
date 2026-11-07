package game

import (
	"context"
	"os"
	"testing"
	"time"

	"basalt/internal/engine"
)

// match plays n games between two parameter sets, alternating colours, and
// returns a's wins, b's wins and draws plus the average game length and the
// slowest single move of each side.
func match(t *testing.T, a, b engine.Params, n int, seed uint64) (aw, bw, dr int, avgPly float64, slowA, slowB time.Duration) {
	t.Helper()
	plies := 0
	for g := 0; g < n; g++ {
		s := engine.NewState()
		aColor := engine.Lava
		if g%2 == 1 {
			aColor = engine.Ash
		}
		for s.Winner == engine.NoWinner {
			p := b
			if s.ToMove == aColor {
				p = a
			}
			p.Seed = seed + uint64(g)*1000 + uint64(s.Ply)
			start := time.Now()
			r, err := engine.Search(context.Background(), s, p)
			el := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			if s.ToMove == aColor {
				slowA = max(slowA, el)
			} else {
				slowB = max(slowB, el)
			}
			s, _, _ = s.Play(r.Move)
		}
		plies += int(s.Ply)
		switch s.Winner {
		case aColor:
			aw++
		case engine.Draw:
			dr++
		default:
			bw++
		}
	}
	return aw, bw, dr, float64(plies) / float64(n), slowA, slowB
}

// TestStrengthLadder checks that each level beats the one below it, using
// iteration budgets (deterministic, CPU-independent) that mirror the levels.
func TestStrengthLadder(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: plays AI-vs-AI games")
	}
	ember := levelFor("ember").Params
	magma := engine.Params{Iterations: 4000, Tactical: true}
	caldera := engine.Params{Iterations: 16000, Tactical: true}
	w, l, d, ply, _, _ := match(t, magma, ember, 10, 1)
	t.Logf("magma-lite vs ember: %d-%d-%d, %.1f plies", w, l, d, ply)
	if w < 8 {
		t.Errorf("magma should beat ember in at least 8 of 10, got %d", w)
	}
	w, l, d, ply, _, _ = match(t, caldera, magma, 10, 2)
	t.Logf("caldera-lite vs magma-lite: %d-%d-%d, %.1f plies", w, l, d, ply)
	if w <= l {
		t.Errorf("caldera should beat magma, got %d-%d", w, l)
	}
}

// TestStrengthReal measures the shipped, time-budgeted levels. It takes
// several minutes, so it only runs with BASALT_STRENGTH=1.
func TestStrengthReal(t *testing.T) {
	if os.Getenv("BASALT_STRENGTH") == "" {
		t.Skip("set BASALT_STRENGTH=1 to measure the real levels")
	}
	n := 20
	e, m, c := levelFor("ember").Params, levelFor("magma").Params, levelFor("caldera").Params
	w, l, d, ply, sa, sb := match(t, m, e, n, 10)
	t.Logf("magma vs ember:   %d-%d-%d  avg %.1f plies  slowest move magma %v ember %v", w, l, d, ply, sa, sb)
	w, l, d, ply, sa, sb = match(t, c, m, n, 20)
	t.Logf("caldera vs magma: %d-%d-%d  avg %.1f plies  slowest move caldera %v magma %v", w, l, d, ply, sa, sb)
}
