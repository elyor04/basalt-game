package engine

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Shared rule vectors: the same file is checked by the TypeScript port used
// in the browser preview (frontend/scripts/check-vectors.ts), so the two
// implementations cannot drift apart. Regenerate with
// BASALT_UPDATE_VECTORS=1 go test ./internal/engine -run TestVectors.

type vecState struct {
	Cells    []int8 `json:"cells"`
	ToMove   int8   `json:"toMove"`
	Captured []int8 `json:"captured"` // [lava, ash]
	Winner   int8   `json:"winner"`
	Reason   int8   `json:"reason"`
}

type vector struct {
	Name   string   `json:"name"`
	Before vecState `json:"before"`
	Move   int      `json:"move"`
	Events []Event  `json:"events"`
	After  vecState `json:"after"`
}

func toVec(s State) vecState {
	return vecState{
		Cells: append([]int8{}, s.Cells[:]...), ToMove: s.ToMove,
		Captured: []int8{s.Captured[Lava], s.Captured[Ash]}, Winner: s.Winner, Reason: s.Reason,
	}
}

func buildVectors(t *testing.T) []vector {
	var out []vector
	add := func(name string, s State, m int) {
		n, ev, err := s.Play(m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, vector{Name: name, Before: toVec(s), Move: m, Events: ev, After: toVec(n)})
	}
	add("push east", position(t, Lava, stone{1, 0, Ash}), at(t, 0, 0))
	add("push all six", position(t, Ash, stone{1, 0, Lava}, stone{1, -1, Lava}, stone{0, -1, Lava}, stone{-1, 0, Lava}, stone{-1, 1, Lava}, stone{0, 1, Lava}), at(t, 0, 0))
	add("blocked by own", position(t, Lava, stone{1, 0, Ash}, stone{2, 0, Lava}), at(t, 0, 0))
	add("blocked by enemy", position(t, Lava, stone{1, 0, Ash}, stone{2, 0, Ash}), at(t, 0, 0))
	add("fall off edge crumbles rim", position(t, Ash, stone{4, 0, Lava}), at(t, 3, 0))
	add("double capture", position(t, Ash, stone{4, 0, Lava}, stone{4, -1, Lava}), at(t, 3, 0))
	g := position(t, Lava, stone{1, 0, Ash})
	g.Cells[at(t, 2, 0)] = Gap
	add("fall into gap", g, at(t, 0, 0))
	add("line of five", position(t, Lava, stone{-3, 0, Lava}, stone{-2, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava}), at(t, 1, 0))
	add("push completes enemy line", position(t, Lava, stone{-1, 0, Ash}, stone{0, 1, Ash}, stone{0, 2, Ash}, stone{0, 3, Ash}, stone{0, 4, Ash}), at(t, -2, 0))
	cw := position(t, Ash, stone{4, 0, Lava})
	cw.Captured[Ash] = int8(CaptureGoal - 1)
	add("capture goal", cw, at(t, 3, 0))
	full := NewState()
	for c := range full.Cells {
		full.Cells[c] = Gap
	}
	full.Cells[at(t, 0, 0)] = Empty
	full.Cells[at(t, 3, -3)] = Ash
	add("full board", full, at(t, 0, 0))

	// Random positions from seeded play cover everything else.
	rng := rand.New(rand.NewPCG(2026, 11))
	for i := 0; i < 40; i++ {
		s := NewState()
		plies := 4 + rng.IntN(40)
		var moves []int
		for k := 0; k < plies && s.Winner == NoWinner; k++ {
			moves = s.LegalMoves(moves)
			s, _, _ = s.Play(moves[rng.IntN(len(moves))])
		}
		if s.Winner != NoWinner {
			continue
		}
		moves = s.LegalMoves(moves)
		add("random", s, moves[rng.IntN(len(moves))])
	}
	return out
}

func TestVectors(t *testing.T) {
	path := filepath.Join("testdata", "vectors.json")
	want := buildVectors(t)
	if os.Getenv("BASALT_UPDATE_VECTORS") != "" {
		b, _ := json.MarshalIndent(map[string]any{"captureGoal": CaptureGoal, "lineLength": LineLength, "vectors": want}, "", " ")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vectors (run with BASALT_UPDATE_VECTORS=1 to create): %v", err)
	}
	var file struct {
		CaptureGoal int      `json:"captureGoal"`
		LineLength  int      `json:"lineLength"`
		Vectors     []vector `json:"vectors"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if file.CaptureGoal != CaptureGoal || file.LineLength != LineLength {
		t.Fatal("vector file was generated with different rules")
	}
	for i, v := range file.Vectors {
		var s State
		copy(s.Cells[:], v.Before.Cells)
		s.ToMove = v.Before.ToMove
		s.Captured[Lava], s.Captured[Ash] = v.Before.Captured[0], v.Before.Captured[1]
		n, ev, err := s.Play(v.Move)
		if err != nil {
			t.Fatalf("vector %d (%s): %v", i, v.Name, err)
		}
		got := toVec(n)
		if !reflect.DeepEqual(got, v.After) || !reflect.DeepEqual(ev, v.Events) {
			t.Fatalf("vector %d (%s) no longer matches the rules", i, v.Name)
		}
	}
}
