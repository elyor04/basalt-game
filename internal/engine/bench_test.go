package engine

import (
	"context"
	"testing"
	"time"
)

func midgame() State {
	s := NewState()
	for _, m := range []int{30, 31, 22, 39, 21, 29, 40, 23, 38} {
		s, _, _ = s.Play(m)
	}
	return s
}

func BenchmarkIteration(b *testing.B) {
	s := midgame()
	b.ResetTimer()
	r, _ := Search(context.Background(), s, Params{Iterations: b.N, Seed: 1, Tactical: true})
	_ = r
}

func TestThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	for _, tac := range []bool{false, true} {
		r, _ := Search(context.Background(), midgame(), Params{Time: 500 * time.Millisecond, Seed: 1, Tactical: tac})
		t.Logf("tactical=%v iterations/s=%.0f", tac, float64(r.Iterations)/(r.Elapsed/1000))
	}
}
