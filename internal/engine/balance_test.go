package engine

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestBalance plays AI-vs-AI games and prints statistics used to tune the
// rules. It only runs with BASALT_BALANCE=1 because it takes minutes.
func TestBalance(t *testing.T) {
	if os.Getenv("BASALT_BALANCE") == "" {
		t.Skip("set BASALT_BALANCE=1 to run the self-play balance study")
	}
	games := 40
	iters := 3000
	if v := os.Getenv("BASALT_ITERS"); v != "" {
		fmt.Sscan(v, &iters)
	}
	if v := os.Getenv("BASALT_GAMES"); v != "" {
		fmt.Sscan(v, &games)
	}
	seedOff := 0
	if v := os.Getenv("BASALT_SEED"); v != "" {
		fmt.Sscan(v, &seedOff)
	}
	type variant struct {
		name     string
		line     int
		goal     int
		collapse bool
	}
	variants := []variant{
		{"line4 goal5 collapse", 4, 5, true},
		{"line5 goal4 collapse", 5, 4, true},
		{"line5 goal5 collapse", 5, 5, true}, // the shipped rules
		{"line5 goal5 nocollapse", 5, 5, false},
		{"line5 goal6 collapse", 5, 6, true},
	}
	if v := os.Getenv("BASALT_VARIANT"); v != "" {
		var keep []variant
		for _, x := range variants {
			if x.name == v {
				keep = append(keep, x)
			}
		}
		variants = keep
	}
	for _, v := range variants {
		CaptureGoal, collapseOnRim = v.goal, v.collapse
		buildLines(v.line)
		var first, second, draws, plies, lineW, capW, fullW int
		start := time.Now()
		for g := 0; g < games; g++ {
			s := NewState()
			for s.Winner == NoWinner {
				r, err := Search(context.Background(), s, Params{Iterations: iters, Seed: uint64(seedOff + g*1000 + int(s.Ply)), Tactical: true})
				if err != nil {
					t.Fatal(err)
				}
				s, _, err = s.Play(r.Move)
				if err != nil {
					t.Fatal(err)
				}
			}
			plies += int(s.Ply)
			firstColor := Lava
			switch s.Winner {
			case Draw:
				draws++
			case firstColor:
				first++
			default:
				second++
			}
			switch s.Reason {
			case ReasonLine:
				lineW++
			case ReasonCapture:
				capW++
			case ReasonFull:
				fullW++
			}
		}
		t.Logf("%-24s games=%d first=%d second=%d draws=%d avgPly=%.1f line=%d capture=%d full=%d (%.0fs)",
			v.name, games, first, second, draws, float64(plies)/float64(games), lineW, capW, fullW, time.Since(start).Seconds())
	}
	CaptureGoal, collapseOnRim = 5, true
	buildLines(5)
}
