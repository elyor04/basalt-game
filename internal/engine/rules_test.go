package engine

import (
	"context"
	"testing"
)

// at returns the cell index of (q, r) and fails the test if it is off-board.
func at(t testing.TB, q, r int) int {
	t.Helper()
	i := IndexOf(q, r)
	if i < 0 {
		t.Fatalf("(%d,%d) is off the board", q, r)
	}
	return i
}

type stone struct {
	q, r  int
	color int8
}

func position(t testing.TB, toMove int8, stones ...stone) State {
	t.Helper()
	s := NewState()
	s.ToMove = toMove
	s.Ply = 10
	for _, x := range stones {
		s.Cells[at(t, x.q, x.r)] = x.color
	}
	return s
}

func TestGeometry(t *testing.T) {
	if NumCells != 61 {
		t.Fatalf("NumCells = %d, want 61", NumCells)
	}
	if c := IndexOf(0, 0); c != 30 {
		t.Fatalf("centre index = %d, want 30", c)
	}
	rimCount := 0
	for c := 0; c < NumCells; c++ {
		if IsRim(c) {
			rimCount++
		}
		for d := 0; d < 6; d++ {
			n := Neighbor(c, d)
			if n >= 0 && Neighbor(n, (d+3)%6) != c {
				t.Fatalf("neighbour relation not symmetric at %d dir %d", c, d)
			}
		}
	}
	if rimCount != 24 {
		t.Fatalf("rim cells = %d, want 24", rimCount)
	}
	// Runs of five: 3 axes × (1+2+3+4+5+4+3+2+1).
	if LineLength != 5 || len(Lines()) != 75 {
		t.Fatalf("lines = %d (length %d), want 75 runs of 5", len(Lines()), LineLength)
	}
}

func TestPushMovesEnemyAway(t *testing.T) {
	s := position(t, Lava, stone{1, 0, Ash}, stone{-1, 0, Lava})
	n, ev, err := s.Play(at(t, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if n.Cells[at(t, 1, 0)] != Empty || n.Cells[at(t, 2, 0)] != Ash {
		t.Fatalf("Ash stone was not pushed east")
	}
	if n.Cells[at(t, -1, 0)] != Lava {
		t.Fatalf("own stone must not move")
	}
	if len(ev) != 2 || ev[0].Kind != EvPlace || ev[1].Kind != EvPush || ev[1].From != at(t, 1, 0) || ev[1].To != at(t, 2, 0) {
		t.Fatalf("unexpected events %+v", ev)
	}
	if n.ToMove != Ash || n.Ply != s.Ply+1 {
		t.Fatalf("turn did not pass")
	}
}

func TestPushInAllDirections(t *testing.T) {
	var stones []stone
	for d := 0; d < 6; d++ {
		stones = append(stones, stone{dirQ[d], dirR[d], Ash})
	}
	s := position(t, Lava, stones...)
	n, ev, _ := s.Play(at(t, 0, 0))
	for d := 0; d < 6; d++ {
		if n.Cells[at(t, 2*dirQ[d], 2*dirR[d])] != Ash || n.Cells[at(t, dirQ[d], dirR[d])] != Empty {
			t.Fatalf("direction %d not pushed", d)
		}
	}
	if len(ev) != 7 {
		t.Fatalf("want 7 events, got %d", len(ev))
	}
}

func TestBlockedPushStays(t *testing.T) {
	s := position(t, Lava, stone{1, 0, Ash}, stone{2, 0, Lava})
	n, ev, _ := s.Play(at(t, 0, 0))
	if n.Cells[at(t, 1, 0)] != Ash || n.Cells[at(t, 2, 0)] != Lava {
		t.Fatalf("blocked stone moved")
	}
	if ev[1].Kind != EvBlocked {
		t.Fatalf("want blocked event, got %+v", ev[1])
	}
	// Blocked by an enemy stone too: pushes never chain.
	s = position(t, Lava, stone{1, 0, Ash}, stone{2, 0, Ash})
	n, _, _ = s.Play(at(t, 0, 0))
	if n.Cells[at(t, 1, 0)] != Ash || n.Cells[at(t, 2, 0)] != Ash {
		t.Fatalf("pushes must not chain")
	}
}

func TestFallOffEdgeCapturesAndCollapses(t *testing.T) {
	s := position(t, Ash, stone{4, 0, Lava})
	n, ev, _ := s.Play(at(t, 3, 0))
	if n.Captured[Ash] != 1 {
		t.Fatalf("capture not counted")
	}
	if n.Cells[at(t, 4, 0)] != Gap {
		t.Fatalf("rim column should collapse into a gap, got %d", n.Cells[at(t, 4, 0)])
	}
	if ev[1].Kind != EvFall || ev[1].To != -1 || ev[2].Kind != EvCollapse {
		t.Fatalf("unexpected events %+v", ev)
	}
	// Nobody may build on a gap.
	if n.Legal(at(t, 4, 0)) {
		t.Fatalf("gap must not be playable")
	}
	if _, _, err := n.Play(at(t, 4, 0)); err != ErrOccupied {
		t.Fatalf("want ErrOccupied, got %v", err)
	}
}

func TestFallIntoGap(t *testing.T) {
	s := position(t, Lava, stone{1, 0, Ash})
	s.Cells[at(t, 2, 0)] = Gap
	n, ev, _ := s.Play(at(t, 0, 0))
	if n.Captured[Lava] != 1 || n.Cells[at(t, 1, 0)] != Empty || n.Cells[at(t, 2, 0)] != Gap {
		t.Fatalf("stone should drop into the gap and its column stay intact")
	}
	if ev[1].Kind != EvFall || ev[1].To != at(t, 2, 0) {
		t.Fatalf("unexpected events %+v", ev)
	}
}

func TestLineOfFiveWins(t *testing.T) {
	s := position(t, Lava, stone{-3, 0, Lava}, stone{-2, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava})
	n, _, _ := s.Play(at(t, 1, 0))
	if n.Winner != Lava || n.Reason != ReasonLine {
		t.Fatalf("want Lava line win, got winner=%d reason=%d", n.Winner, n.Reason)
	}
	if got := n.WinningLine(Lava); len(got) != 5 {
		t.Fatalf("winning line not reported")
	}
	if _, _, err := n.Play(at(t, 3, 3-3)); err != ErrGameOver {
		t.Fatalf("moves after the end must fail, got %v", err)
	}
	// Four is not enough; a hole in the row is not a line.
	s = position(t, Lava, stone{-2, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava})
	n, _, _ = s.Play(at(t, 1, 0))
	if n.Winner != NoWinner {
		t.Fatalf("four in a row must not win")
	}
	s = position(t, Lava, stone{-3, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava})
	n, _, _ = s.Play(at(t, 1, 0))
	if n.Winner != NoWinner {
		t.Fatalf("broken line must not win")
	}
}

func TestLineOnEveryAxis(t *testing.T) {
	for _, d := range []int{0, 2, 4} {
		var st []stone
		for k := -2; k < 2; k++ {
			st = append(st, stone{k * dirQ[d], k * dirR[d], Ash})
		}
		s := position(t, Ash, st...)
		n, _, _ := s.Play(at(t, 2*dirQ[d], 2*dirR[d]))
		if n.Winner != Ash {
			t.Fatalf("axis %d line not detected", d)
		}
	}
}

func TestCaptureGoalWins(t *testing.T) {
	s := position(t, Ash, stone{4, 0, Lava})
	s.Captured[Ash] = int8(CaptureGoal - 1)
	n, _, _ := s.Play(at(t, 3, 0))
	if n.Winner != Ash || n.Reason != ReasonCapture {
		t.Fatalf("want capture win, got %d/%d", n.Winner, n.Reason)
	}
}

func TestDoubleCapture(t *testing.T) {
	// Placing at (3,0) pushes (4,0) east off the board and (4,-1) north-east off it.
	s := position(t, Ash, stone{4, 0, Lava}, stone{4, -1, Lava})
	n, _, _ := s.Play(at(t, 3, 0))
	if n.Captured[Ash] != 2 {
		t.Fatalf("want 2 captures, got %d", n.Captured[Ash])
	}
}

func TestPushCompletingEnemyLineHandsThemTheWin(t *testing.T) {
	// Ash has (2,0),(3,0)... wait: Ash stones on (0,1),(0,2),(0,3) are
	// missing (0,0); a Ash stone at (1,-1)... we push a Ash stone from
	// (-1,0) into (0,0)? Pushes go away from the placed stone, so place
	// Lava at (-2,0) to push Ash (-1,0) to (0,0), completing (0,0)-(0,3).
	s := position(t, Lava, stone{-1, 0, Ash}, stone{0, 1, Ash}, stone{0, 2, Ash}, stone{0, 3, Ash}, stone{0, 4, Ash})
	n, _, _ := s.Play(at(t, -2, 0))
	if n.Winner != Ash || n.Reason != ReasonLine {
		t.Fatalf("push into a Ash line should make Ash win, got %d/%d", n.Winner, n.Reason)
	}
	// But the mover's own line takes precedence.
	s = position(t, Lava, stone{-1, 0, Ash}, stone{0, 1, Ash}, stone{0, 2, Ash}, stone{0, 3, Ash}, stone{0, 4, Ash},
		stone{-2, -2, Lava}, stone{-2, -1, Lava}, stone{-2, 1, Lava}, stone{-2, 2, Lava})
	n, _, _ = s.Play(at(t, -2, 0))
	if n.Winner != Lava {
		t.Fatalf("mover's line must win first, got %d", n.Winner)
	}
}

func TestFullBoardMostStonesWins(t *testing.T) {
	s := NewState()
	s.Ply = 60
	// Fill with a pattern that has no line of four: alternate colours along
	// rows in pairs is fiddly, so use gaps for most cells and leave a tiny
	// field.
	for c := range s.Cells {
		s.Cells[c] = Gap
	}
	s.Cells[at(t, 0, 0)] = Empty
	s.Cells[at(t, 1, 0)] = Ash
	s.Cells[at(t, -1, 0)] = Lava
	s.Cells[at(t, 0, 1)] = Lava
	s.ToMove = Lava
	n, _, _ := s.Play(at(t, 0, 0))
	// The new Lava stone pushes Ash (1,0) into the gap at (2,0): the hex
	// it leaves is empty again, so the board is not full yet.
	if n.Winner != NoWinner || n.Captured[Lava] != 1 {
		t.Fatalf("want no winner and one capture, got %d/%d", n.Winner, n.Captured[Lava])
	}
	// A clean full-board count with no pushes possible.
	s = NewState()
	for c := range s.Cells {
		s.Cells[c] = Gap
	}
	s.Cells[at(t, 0, 0)] = Empty
	s.Cells[at(t, 3, -3)] = Ash
	s.Cells[at(t, -3, 3)] = Ash
	s.ToMove = Lava
	n, _, _ = s.Play(at(t, 0, 0))
	if n.Winner != Ash || n.Reason != ReasonFull {
		t.Fatalf("Ash has more stones; got %d/%d", n.Winner, n.Reason)
	}
	s.Cells[at(t, -3, 3)] = Lava
	n, _, _ = s.Play(at(t, 0, 0))
	if n.Winner != Lava {
		t.Fatalf("Lava 2 vs Ash 1; got %d", n.Winner)
	}
	s.Cells[at(t, -3, 3)] = Gap
	n, _, _ = s.Play(at(t, 0, 0))
	if n.Winner != Draw {
		t.Fatalf("1 vs 1 should draw; got %d", n.Winner)
	}
}

func TestIllegalMoves(t *testing.T) {
	s := NewState()
	for _, m := range []int{-1, NumCells + 1, 999} {
		if _, _, err := s.Play(m); err == nil {
			t.Fatalf("move %d accepted", m)
		}
	}
	s, _, _ = s.Play(30)
	if _, _, err := s.Play(30); err != ErrOccupied {
		t.Fatalf("want ErrOccupied, got %v", err)
	}
}

func TestWinningAndSafeMoves(t *testing.T) {
	s := position(t, Lava, stone{-2, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava}, stone{1, 0, Lava})
	w := s.WinningMoves(nil)
	if len(w) != 2 {
		t.Fatalf("want 2 winning cells (both ends), got %v", w)
	}
	// Ash to move must block one of... both ends are open, so every move
	// loses: SafeMoves returns all moves.
	s.ToMove = Ash
	safe := s.SafeMoves(nil)
	if len(safe) == 0 {
		t.Fatal("SafeMoves must never be empty")
	}
	// One open end: Ash stone at (1,0) blocks the east end.
	s = position(t, Ash, stone{-3, 0, Lava}, stone{-2, 0, Lava}, stone{-1, 0, Lava}, stone{0, 0, Lava}, stone{1, 0, Ash})
	safe = s.SafeMoves(nil)
	west := at(t, -4, 0)
	found := false
	for _, m := range safe {
		if m == west {
			found = true
		}
	}
	if !found {
		t.Fatalf("blocking the west end must be safe: %v", safe)
	}
	for _, m := range safe {
		n, _, _ := s.Play(m)
		if n.hasWinningMove() {
			t.Fatalf("safe move %d leaves a win for Lava", m)
		}
	}
}

func TestAIFindsWinInOne(t *testing.T) {
	s := position(t, Ash, stone{1, -2, Ash}, stone{1, -1, Ash}, stone{1, 0, Ash}, stone{1, 1, Ash},
		stone{1, 2, Lava}, stone{-2, 2, Lava}, stone{-3, 2, Lava})
	for _, tac := range []bool{true, false} {
		r, err := Search(context.Background(), s, Params{Iterations: 400, Seed: 7, Tactical: tac})
		if err != nil {
			t.Fatal(err)
		}
		if r.Move != at(t, 1, -3) {
			t.Fatalf("tactical=%v: want winning move %d, got %d", tac, at(t, 1, -3), r.Move)
		}
	}
}

func TestAIFindsCaptureWin(t *testing.T) {
	s := position(t, Lava, stone{-4, 2, Ash}, stone{0, 0, Lava})
	s.Captured[Lava] = int8(CaptureGoal - 1)
	r, _ := Search(context.Background(), s, Params{Iterations: 300, Seed: 3, Tactical: true})
	n, _, _ := s.Play(r.Move)
	if n.Winner != Lava {
		t.Fatalf("AI missed the capture win, played %d", r.Move)
	}
}

func TestAIBlocksLossInOne(t *testing.T) {
	// Lava threatens (-1,0)..(2,0) + (3,0) with (-2,0) blocked by Ash.
	s := position(t, Ash, stone{-1, 0, Lava}, stone{0, 0, Lava}, stone{1, 0, Lava}, stone{2, 0, Lava}, stone{-2, 0, Ash},
		stone{0, -3, Ash})
	r, err := Search(context.Background(), s, Params{Iterations: 2000, Seed: 11, Tactical: true})
	if err != nil {
		t.Fatal(err)
	}
	n, _, _ := s.Play(r.Move)
	if n.hasWinningMove() {
		t.Fatalf("AI move %d leaves Lava a win in one", r.Move)
	}
}

func TestSearchDeterministic(t *testing.T) {
	s := midgame()
	a, _ := Search(context.Background(), s, Params{Iterations: 1500, Seed: 42, Tactical: true})
	b, _ := Search(context.Background(), s, Params{Iterations: 1500, Seed: 42, Tactical: true})
	if a.Move != b.Move || a.Iterations != b.Iterations {
		t.Fatalf("same seed gave %d/%d vs %d/%d", a.Move, a.Iterations, b.Move, b.Iterations)
	}
}

func TestSearchCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Search(ctx, midgame(), Params{Iterations: 0, Time: 0, Seed: 1, Tactical: true})
	if err == nil {
		t.Fatal("cancelled search should return an error")
	}
}

func TestSearchNeverIllegal(t *testing.T) {
	s := NewState()
	for i := 0; s.Winner == NoWinner && i < 120; i++ {
		r, err := Search(context.Background(), s, Params{Iterations: 120, Seed: uint64(i), Tactical: i%2 == 0})
		if err != nil {
			t.Fatal(err)
		}
		if !s.Legal(r.Move) {
			t.Fatalf("illegal move %d at ply %d", r.Move, s.Ply)
		}
		s, _, _ = s.Play(r.Move)
	}
	if s.Winner == NoWinner {
		t.Fatal("game did not finish")
	}
}
