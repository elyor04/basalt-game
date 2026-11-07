package engine

import "errors"

// Cell contents.
const (
	Empty int8 = 0
	Lava  int8 = 1 // moves first
	Ash  int8 = 2
	Gap  int8 = 3 // a collapsed column: a hole stones fall into
)

// Game results stored in State.Winner.
const (
	NoWinner int8 = 0
	// Winner values 1 and 2 are the players; Draw ends a game level.
	Draw int8 = 3
)

// Win reasons.
const (
	ReasonNone    int8 = 0
	ReasonLine    int8 = 1 // four in a row
	ReasonCapture int8 = 2 // captured CaptureGoal stones
	ReasonFull    int8 = 3 // no empty column left; most stones on the board
	ReasonResign  int8 = 4 // set by the session, never by the rules
)

// CaptureGoal is how many enemy stones you must push off to win. Tuned by
// self-play (see balance_test.go and the README).
var CaptureGoal = 5

// collapseOnRim: when a stone is pushed off the outer edge, the column it
// stood on crumbles into a gap. Kept as a variable so the balance
// experiments can switch it off.
var collapseOnRim = true

// State is a complete, immutable-by-convention game position. It is a small
// value type so search can copy it freely.
type State struct {
	Cells    [NumCells]int8
	ToMove   int8    // Lava or Ash
	Captured [3]int8 // Captured[p]: enemy stones player p has pushed off
	Ply      int16
	Winner   int8
	Reason   int8
}

// NewState returns the empty starting position with Lava to move.
func NewState() State {
	return State{ToMove: Lava}
}

// Other returns the opponent of player p.
func Other(p int8) int8 { return 3 - p }

// Event kinds for animation, in the order they happen within one move.
const (
	EvPlace    = "place"
	EvPush     = "push"     // a stone slid one hex; From → To
	EvFall     = "fall"     // a stone was pushed off the board or into a gap
	EvBlocked  = "blocked"  // a stone would be pushed but the hex behind is taken
	EvCollapse = "collapse" // a column crumbled into a gap
)

// Event describes one visible consequence of a move. To is -1 for a fall
// off the board; for a fall into a gap it is the gap's cell.
type Event struct {
	Kind  string `json:"kind"`
	From  int    `json:"from"`
	To    int    `json:"to"`
	Color int8   `json:"color"`
	Dir   int    `json:"dir"`
}

// Rule violations.
var (
	ErrGameOver = errors.New("the game is over")
	ErrBadCell  = errors.New("no such hex")
	ErrOccupied = errors.New("that hex is not empty")
)

// Legal reports whether move m is legal in s.
func (s *State) Legal(m int) bool {
	if s.Winner != NoWinner {
		return false
	}
	return m >= 0 && m < NumCells && s.Cells[m] == Empty
}

// LegalMoves appends every legal move to dst and returns it.
func (s *State) LegalMoves(dst []int) []int {
	dst = dst[:0]
	if s.Winner != NoWinner {
		return dst
	}
	for c := 0; c < NumCells; c++ {
		if s.Cells[c] == Empty {
			dst = append(dst, c)
		}
	}
	return dst
}

// Play applies move m and returns the new state and its events.
func (s State) Play(m int) (State, []Event, error) {
	if s.Winner != NoWinner {
		return s, nil, ErrGameOver
	}
	if m < 0 || m >= NumCells {
		return s, nil, ErrBadCell
	}
	if s.Cells[m] != Empty {
		return s, nil, ErrOccupied
	}
	ev := make([]Event, 0, 8)
	s.apply(m, &ev)
	return s, ev, nil
}

// apply plays a known-legal placement in place. ev may be nil (search).
func (s *State) apply(m int, ev *[]Event) {
	me := s.ToMove
	foe := Other(me)
	s.Cells[m] = me
	if ev != nil {
		*ev = append(*ev, Event{Kind: EvPlace, From: m, To: m, Color: me, Dir: -1})
	}
	var moved [6]int
	nMoved := 0
	for d := 0; d < 6; d++ {
		n := neighbor[m][d]
		if n < 0 || s.Cells[n] != foe {
			continue
		}
		dst := neighbor[n][d]
		switch {
		case dst < 0:
			// Off the edge: the stone falls into the sea.
			s.Cells[n] = Empty
			s.Captured[me]++
			if ev != nil {
				*ev = append(*ev, Event{Kind: EvFall, From: n, To: -1, Color: foe, Dir: d})
			}
			if collapseOnRim {
				s.Cells[n] = Gap
				if ev != nil {
					*ev = append(*ev, Event{Kind: EvCollapse, From: n, To: n, Color: 0, Dir: d})
				}
			}
		case s.Cells[dst] == Gap:
			s.Cells[n] = Empty
			s.Captured[me]++
			if ev != nil {
				*ev = append(*ev, Event{Kind: EvFall, From: n, To: dst, Color: foe, Dir: d})
			}
		case s.Cells[dst] == Empty:
			s.Cells[n] = Empty
			s.Cells[dst] = foe
			moved[nMoved] = dst
			nMoved++
			if ev != nil {
				*ev = append(*ev, Event{Kind: EvPush, From: n, To: dst, Color: foe, Dir: d})
			}
		default:
			if ev != nil {
				*ev = append(*ev, Event{Kind: EvBlocked, From: n, To: dst, Color: foe, Dir: d})
			}
		}
	}
	s.Ply++
	s.ToMove = foe

	// The mover wins first: by captures or by a line through the new stone
	// (the mover's other stones never move on their own turn).
	switch {
	case int(s.Captured[me]) >= CaptureGoal:
		s.Winner, s.Reason = me, ReasonCapture
		return
	case s.lineThrough(m, me):
		s.Winner, s.Reason = me, ReasonLine
		return
	}
	// A push can complete the opponent's line; then they win.
	for i := 0; i < nMoved; i++ {
		if s.lineThrough(moved[i], foe) {
			s.Winner, s.Reason = foe, ReasonLine
			return
		}
	}
	// No empty column left: whoever has more stones standing wins.
	full := true
	for c := 0; c < NumCells; c++ {
		if s.Cells[c] == Empty {
			full = false
			break
		}
	}
	if full {
		a, b := s.Count(Lava), s.Count(Ash)
		s.Reason = ReasonFull
		switch {
		case a > b:
			s.Winner = Lava
		case b > a:
			s.Winner = Ash
		default:
			s.Winner = Draw
		}
	}
}

// lineThrough reports whether player p owns a full line through cell c.
func (s *State) lineThrough(c int, p int8) bool {
	for _, li := range linesAt[c] {
		if s.owns(lines[li], p) {
			return true
		}
	}
	return false
}

// WinningLine returns the cells of a completed line for player p, if any.
func (s *State) WinningLine(p int8) []int {
	for _, l := range lines {
		if s.owns(l, p) {
			return append([]int(nil), l...)
		}
	}
	return nil
}

func (s *State) owns(l []int, p int8) bool {
	for _, c := range l {
		if s.Cells[c] != p {
			return false
		}
	}
	return true
}

// Count returns how many stones of colour p stand on the board.
func (s *State) Count(p int8) int {
	n := 0
	for _, v := range s.Cells {
		if v == p {
			n++
		}
	}
	return n
}
