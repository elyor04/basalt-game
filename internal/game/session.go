package game

import (
	"basalt/internal/engine"
)

// Game modes.
const (
	ModeAI      = "ai"
	ModeHotseat = "hotseat"
	ModePuzzle  = "puzzle"
)

// session is the live game. It is only touched under GameService.mu.
type session struct {
	id         int64
	mode       string
	difficulty string
	human      int8 // AI and puzzle modes: the human's colour
	seed       uint64
	puzzleKey  string
	start      engine.State
	states     []engine.State // states[0] == start, one per ply played
	moves      []int
	records    []MoveRecord
	undos      int
	hints      int
	resigned   int8 // colour that resigned, 0 if none
	recorded   bool
	saveFailed bool // the finished game could not be written to the profile
	outcome    *Outcome
	maxCapture int  // most stones the human pushed off with one placement
	humanMoved bool // the human has placed a stone at some point (survives undo)
}

func (s *session) state() engine.State { return s.states[len(s.states)-1] }

// winner returns the winner, counting a resignation.
func (s *session) winner() (int8, int8) {
	if s.resigned != 0 {
		return engine.Other(s.resigned), engine.ReasonResign
	}
	st := s.state()
	return st.Winner, st.Reason
}

func (s *session) over() bool {
	w, _ := s.winner()
	return w != engine.NoWinner
}

// aiTurn reports whether the computer (or the puzzle's defender) moves next.
func (s *session) aiTurn() bool {
	if s.over() || s.puzzleFailed() {
		return false
	}
	switch s.mode {
	case ModeAI, ModePuzzle:
		return s.state().ToMove != s.human
	}
	return false
}

// humanMovesInPuzzle counts the puzzle solver's placements so far.
func (s *session) humanMoves() int {
	n := 0
	for _, r := range s.records {
		if r.Player == s.human {
			n++
		}
	}
	return n
}

// puzzleFailed: the solver has used both moves without winning.
func (s *session) puzzleFailed() bool {
	if s.mode != ModePuzzle {
		return false
	}
	w, _ := s.winner()
	return w != s.human && (w != engine.NoWinner || s.humanMoves() >= 2)
}

// play applies a move and records it. The caller has validated turn order.
func (s *session) play(m int) ([]engine.Event, error) {
	st := s.state()
	next, ev, err := st.Play(m)
	if err != nil {
		return nil, err
	}
	rec := MoveRecord{Ply: int(st.Ply) + 1, Player: st.ToMove, Cell: m}
	for _, e := range ev {
		switch e.Kind {
		case engine.EvPush:
			rec.Pushed++
		case engine.EvFall:
			rec.Fell++
		}
	}
	if (s.mode == ModeAI || s.mode == ModePuzzle) && st.ToMove == s.human {
		s.humanMoved = true
		if rec.Fell > s.maxCapture {
			s.maxCapture = rec.Fell
		}
	}
	s.states = append(s.states, next)
	s.moves = append(s.moves, m)
	s.records = append(s.records, rec)
	return ev, nil
}

// undoPlies removes the last n plies.
func (s *session) undoPlies(n int) {
	keep := len(s.moves) - n
	s.states = s.states[:keep+1]
	s.moves = s.moves[:keep]
	s.records = s.records[:keep]
	// Recompute the biggest capture over the remaining human moves so an
	// undone avalanche does not count.
	s.maxCapture = 0
	for _, r := range s.records {
		if r.Player == s.human && r.Fell > s.maxCapture {
			s.maxCapture = r.Fell
		}
	}
}

func (s *session) saved() *SavedGame {
	if s.over() || s.puzzleFailed() {
		return nil
	}
	return &SavedGame{
		Mode: s.mode, Difficulty: s.difficulty, Human: s.human,
		Moves: append([]int{}, s.moves...), Undos: s.undos, Hints: s.hints,
		PuzzleKey: s.puzzleKey, Seed: s.seed, HumanMoved: s.humanMoved,
	}
}

// MoveRecord is one line of the move history.
type MoveRecord struct {
	Ply    int  `json:"ply"`
	Player int8 `json:"player"`
	Cell   int  `json:"cell"`
	Pushed int  `json:"pushed"`
	Fell   int  `json:"fell"`
}

// Outcome is attached to the view once an AI, hot-seat or puzzle game has
// been recorded in the profile.
type Outcome struct {
	Result          string   `json:"result"` // "win", "loss", "draw" (from the human's side in AI/puzzle)
	NewAchievements []string `json:"newAchievements"`
	Streak          int      `json:"streak"`
	BestStreak      int      `json:"bestStreak"`
}

// GameView is the complete, JSON-friendly picture of the current game.
type GameView struct {
	ID          int64        `json:"id"`
	Mode        string       `json:"mode"`
	Difficulty  string       `json:"difficulty"`
	Human       int8         `json:"human"`
	Cells       []int8       `json:"cells"`
	ToMove      int8         `json:"toMove"`
	Captured    []int8       `json:"captured"` // [lava, ash]
	Ply         int          `json:"ply"`
	Winner      int8         `json:"winner"`
	Reason      string       `json:"reason"`
	WinLine     []int        `json:"winLine"`
	CanUndo     bool         `json:"canUndo"`
	AITurn      bool         `json:"aiTurn"`
	History     []MoveRecord `json:"history"`
	CaptureGoal int          `json:"captureGoal"`
	LineLength  int          `json:"lineLength"`
	PuzzleKey   string       `json:"puzzleKey"`
	PuzzleMoves int          `json:"puzzleMoves"` // solver moves used (puzzle mode)
	Failed      bool         `json:"failed"`      // puzzle: out of moves
	Outcome     *Outcome     `json:"outcome"`
	Undos       int          `json:"undos"`
	Hints       int          `json:"hints"`
	SaveFailed  bool         `json:"saveFailed"`
	// Forfeit: replacing this unfinished game counts as a resignation.
	Forfeit bool `json:"forfeit"`
}

var reasonNames = map[int8]string{
	engine.ReasonNone:    "",
	engine.ReasonLine:    "line",
	engine.ReasonCapture: "capture",
	engine.ReasonFull:    "full",
	engine.ReasonResign:  "resign",
}

func (s *session) view() GameView {
	st := s.state()
	w, reason := s.winner()
	v := GameView{
		ID: s.id, Mode: s.mode, Difficulty: s.difficulty, Human: s.human,
		Cells:       append([]int8{}, st.Cells[:]...),
		ToMove:      st.ToMove,
		Captured:    []int8{st.Captured[engine.Lava], st.Captured[engine.Ash]},
		Ply:         int(st.Ply),
		Winner:      w,
		Reason:      reasonNames[reason],
		WinLine:     []int{},
		CanUndo:     s.canUndo(),
		AITurn:      s.aiTurn(),
		History:     append([]MoveRecord{}, s.records...),
		CaptureGoal: engine.CaptureGoal,
		LineLength:  engine.LineLength,
		PuzzleKey:   s.puzzleKey,
		Failed:      s.puzzleFailed(),
		Outcome:     s.outcome,
		Undos:       s.undos,
		Hints:       s.hints,
		SaveFailed:  s.saveFailed,
		Forfeit:     s.forfeitOnReplace(),
	}
	if s.mode == ModePuzzle {
		v.PuzzleMoves = s.humanMoves()
	}
	if reason == engine.ReasonLine && w != engine.Draw {
		if l := st.WinningLine(w); l != nil {
			v.WinLine = l
		}
	}
	return v
}

// canUndo: AI games can take back the human's last move (and the AI reply);
// hot-seat games can take back one placement. Finished games are final.
func (s *session) canUndo() bool {
	if s.over() || s.puzzleFailed() {
		return false
	}
	switch s.mode {
	case ModeAI:
		return s.lastHumanIndex() >= 0
	case ModeHotseat:
		return len(s.moves) > 0
	}
	return false
}

// forfeitOnReplace: abandoning an AI game you have played in is a loss,
// even if you took every move back first.
func (s *session) forfeitOnReplace() bool {
	return s.mode == ModeAI && !s.over() && s.humanMoved
}

func (s *session) lastHumanIndex() int {
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].Player == s.human {
			return i
		}
	}
	return -1
}
