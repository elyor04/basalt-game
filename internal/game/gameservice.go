package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"basalt/internal/engine"
)

// GameError is returned to the frontend. Wails serialises the error value
// into the JS error's `cause`, so the UI reads Code and shows its own
// translated message; Message is the English fallback for logs and tools.
type GameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error ends with "[code]": the Android transport (Wails 3.1) sends only this
// text and drops `cause`, so the frontend recovers the code from it.
func (e *GameError) Error() string { return e.Message + " [" + e.Code + "]" }

var (
	ErrNoGame      = &GameError{"no_game", "there is no game in progress"}
	ErrStaleGame   = &GameError{"stale_game", "that game has already been replaced"}
	ErrNotYourTurn = &GameError{"not_your_turn", "it is not your turn"}
	ErrIllegal     = &GameError{"illegal_move", "that hex is not free"}
	ErrOver        = &GameError{"game_over", "the game is already over"}
	ErrCannotUndo  = &GameError{"cannot_undo", "there is nothing to take back"}
	ErrCancelled   = &GameError{"cancelled", "the computer stopped thinking"}
	ErrBadOption   = &GameError{"bad_option", "unknown game option"}
	ErrNoPuzzle    = &GameError{"no_puzzle", "no puzzle could be built for today"}
)

func storageError(err error) error {
	if err == nil {
		return nil
	}
	var ge *GameError
	if errors.As(err, &ge) {
		return ge
	}
	return &GameError{Code: "storage", Message: err.Error()}
}

// Level describes how one AI difficulty searches.
type Level struct {
	Params engine.Params
	// MinDelay keeps very fast replies from feeling instantaneous.
	MinDelay time.Duration
}

func levelFor(d string) Level {
	switch d {
	case "ember":
		// A small search with a soft final choice and a 30% chance per move
		// of skipping the win/loss-in-one checks: it plays sensible shapes
		// but makes human-like mistakes.
		return Level{Params: engine.Params{Iterations: 500, Temperature: 0.6, Blunder: 0.3, Tactical: true}, MinDelay: 450 * time.Millisecond}
	case "magma":
		return Level{Params: engine.Params{Iterations: 12000, Time: 600 * time.Millisecond, Tactical: true}, MinDelay: 350 * time.Millisecond}
	default: // caldera
		return Level{Params: engine.Params{Time: 1500 * time.Millisecond, Workers: min(runtime.NumCPU(), 4), Tactical: true}}
	}
}

func hintParams() engine.Params {
	return engine.Params{Time: 700 * time.Millisecond, Workers: min(runtime.NumCPU(), 2), Tactical: true}
}

// NewGameOptions starts a game.
type NewGameOptions struct {
	Mode       string `json:"mode"`       // "ai", "hotseat" or "puzzle"
	Difficulty string `json:"difficulty"` // AI mode: "ember", "magma" or "caldera"
	Color      string `json:"color"`      // AI mode: "lava", "ash" or "random"
}

// MoveResult is returned after any placement.
type MoveResult struct {
	Game   GameView       `json:"game"`
	Move   int            `json:"move"`
	Player int8           `json:"player"`
	Events []engine.Event `json:"events"`
}

// Hint is the AI's suggestion for the player to move.
type Hint struct {
	Cell    int     `json:"cell"`
	WinRate float64 `json:"winRate"`
}

// DailyPuzzle describes today's puzzle.
type DailyPuzzle struct {
	Key    string `json:"key"`
	Solved bool   `json:"solved"`
	Streak int    `json:"streak"` // consecutive days solved, ending today or yesterday
	Total  int    `json:"total"`
}

// GameService is bound to the frontend. Every exported method becomes a
// typed TypeScript function in frontend/bindings.
type GameService struct {
	store *Store
	now   func() time.Time

	mu       sync.Mutex
	sess     *session
	nextID   int64
	restored bool
	cancel   context.CancelFunc // the running AI search or hint, if any
	puzzles  map[string]engine.Puzzle
	// levels is replaceable in tests to keep AI turns fast.
	levels func(string) Level
}

// NewGameService wires a service to the platform's save directory.
func NewGameService() *GameService {
	return newService(NewStore(saveDir))
}

func newService(store *Store) *GameService {
	return &GameService{store: store, now: time.Now, puzzles: map[string]engine.Puzzle{}, levels: levelFor}
}

// saveDir prefers the native app storage path on mobile and falls back to the
// per-user config directory on desktop.
func saveDir() (string, error) {
	if p := application.Mobile.StoragePath(); p != "" {
		return p, nil
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "Basalt"), nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".basalt"), nil
	}
	return "", errors.New("no writable directory available for save data")
}

// ---------------------------------------------------------------- profile

// GetProfile returns the saved profile.
func (g *GameService) GetProfile() (Profile, error) {
	p, err := g.store.View()
	return p, storageError(err)
}

// SaveSettings persists player preferences.
func (g *GameService) SaveSettings(s Settings) (Profile, error) {
	p, err := g.store.Update(func(p *Profile) error {
		p.Settings = sanitizeSettings(s)
		return nil
	})
	return p, storageError(err)
}

// ResetProfile wipes statistics and achievements but keeps settings.
func (g *GameService) ResetProfile() (Profile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopSearchLocked()
	p, err := g.store.Update(func(p *Profile) error {
		settings := p.Settings
		*p = newProfile()
		p.Settings = settings
		return nil
	})
	if err == nil {
		g.sess = nil
	}
	return p, storageError(err)
}

// ---------------------------------------------------------------- games

// NewGame starts a game, replacing the current one. Abandoning an unfinished
// AI game in which you have already moved counts as a resignation, so a
// losing position cannot be escaped to protect a streak.
func (g *GameService) NewGame(opts NewGameOptions) (GameView, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.restoreLocked()
	g.stopSearchLocked()

	s := &session{mode: opts.Mode, seed: uint64(g.now().UnixNano()) & (1<<53 - 1)}
	switch opts.Mode {
	case ModeAI:
		if !contains(difficulties, opts.Difficulty) {
			return GameView{}, ErrBadOption
		}
		s.difficulty = opts.Difficulty
		switch opts.Color {
		case "lava":
			s.human = engine.Lava
		case "ash":
			s.human = engine.Ash
		case "random":
			s.human = engine.Lava + int8(s.seed%2)
		default:
			return GameView{}, ErrBadOption
		}
		s.start = engine.NewState()
	case ModeHotseat:
		s.start = engine.NewState()
	case ModePuzzle:
		pz, err := g.puzzleLocked(g.today())
		if err != nil {
			return GameView{}, err
		}
		s.puzzleKey = pz.Key
		s.start = pz.State
		s.human = pz.State.ToMove
	default:
		return GameView{}, ErrBadOption
	}
	s.states = []engine.State{s.start}

	// Forfeit the game being replaced if that is what fairness requires.
	if old := g.sess; old != nil && old.forfeitOnReplace() {
		old.resigned = old.human
		if _, err := g.recordLocked(old); err != nil {
			return GameView{}, err
		}
	}

	g.nextID++
	s.id = g.nextID
	if _, err := g.store.Update(func(p *Profile) error {
		p.Saved = s.saved()
		if opts.Mode == ModeAI {
			p.Settings.Difficulty = opts.Difficulty
			p.Settings.Color = opts.Color
		}
		return nil
	}); err != nil {
		return GameView{}, storageError(err)
	}
	g.sess = s
	return s.view(), nil
}

// CurrentGame returns the game in progress (restored from disk after a
// restart), or ErrNoGame.
func (g *GameService) CurrentGame() (GameView, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.restoreLocked()
	if g.sess == nil {
		return GameView{}, ErrNoGame
	}
	return g.sess.view(), nil
}

// Play places a stone for the human (or the player to move in hot-seat).
func (g *GameService) Play(id int64, cell int) (MoveResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, err := g.liveLocked(id)
	if err != nil {
		return MoveResult{}, err
	}
	if s.aiTurn() {
		return MoveResult{}, ErrNotYourTurn
	}
	if s.puzzleFailed() {
		return MoveResult{}, ErrOver
	}
	g.stopSearchLocked() // a pending hint is now stale
	return g.applyLocked(s, cell)
}

// AIMove computes and plays the computer's move. It blocks for the AI's
// thinking time; Undo, NewGame, Resign, CancelAI or cancelling the call
// from JavaScript stop it early with ErrCancelled.
func (g *GameService) AIMove(ctx context.Context, id int64) (MoveResult, error) {
	g.mu.Lock()
	s, err := g.liveLocked(id)
	if err != nil {
		g.mu.Unlock()
		return MoveResult{}, err
	}
	if !s.aiTurn() {
		g.mu.Unlock()
		return MoveResult{}, ErrNotYourTurn
	}
	if s.mode == ModePuzzle {
		defer g.mu.Unlock()
		return g.applyLocked(s, engine.Defend(s.state()))
	}
	st := s.state()
	ply := len(s.moves)
	lvl := g.levels(s.difficulty)
	lvl.Params.Seed = s.seed + uint64(ply)*0x9e37
	sctx, cancel := g.beginSearchLocked(ctx)
	g.mu.Unlock()
	defer cancel()

	started := time.Now()
	res, serr := engine.Search(sctx, st, lvl.Params)
	if serr == nil && lvl.MinDelay > 0 {
		if wait := lvl.MinDelay - time.Since(started); wait > 0 {
			select {
			case <-time.After(wait):
			case <-sctx.Done():
			}
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	// The world may have moved on while we were thinking.
	if sctx.Err() != nil || g.sess != s || len(s.moves) != ply {
		return MoveResult{}, ErrCancelled
	}
	g.cancel = nil
	if serr != nil {
		return MoveResult{}, ErrCancelled
	}
	return g.applyLocked(s, res.Move)
}

// Hint asks the AI what the player to move should play.
func (g *GameService) Hint(ctx context.Context, id int64) (Hint, error) {
	g.mu.Lock()
	s, err := g.liveLocked(id)
	if err != nil {
		g.mu.Unlock()
		return Hint{}, err
	}
	if s.over() || s.aiTurn() || s.puzzleFailed() {
		g.mu.Unlock()
		return Hint{}, ErrNotYourTurn
	}
	st := s.state()
	ply := len(s.moves)
	if s.mode == ModePuzzle {
		// The puzzle hint shows the solution: the first move, then the win.
		cell := -1
		if ply == 0 {
			cell = g.puzzles[s.puzzleKey].Solution
		} else if w := st.WinningMoves(nil); len(w) > 0 {
			cell = w[0]
		}
		if cell >= 0 {
			s.hints++
			g.persistLocked(s)
			g.mu.Unlock()
			return Hint{Cell: cell, WinRate: 1}, nil
		}
	}
	p := hintParams()
	p.Seed = s.seed ^ uint64(ply)<<20
	sctx, cancel := g.beginSearchLocked(ctx)
	g.mu.Unlock()
	defer cancel()

	res, serr := engine.Search(sctx, st, p)

	g.mu.Lock()
	defer g.mu.Unlock()
	if serr != nil || sctx.Err() != nil || g.sess != s || len(s.moves) != ply {
		return Hint{}, ErrCancelled
	}
	g.cancel = nil
	s.hints++
	g.persistLocked(s)
	return Hint{Cell: res.Move, WinRate: res.WinRate}, nil
}

// CancelAI stops a running AI move or hint.
func (g *GameService) CancelAI() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopSearchLocked()
}

// Undo takes back the human's last move (and the AI's reply), or the last
// placement in hot-seat.
func (g *GameService) Undo(id int64) (GameView, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, err := g.liveLocked(id)
	if err != nil {
		return GameView{}, err
	}
	if !s.canUndo() {
		return GameView{}, ErrCannotUndo
	}
	g.stopSearchLocked()
	switch s.mode {
	case ModeAI:
		s.undoPlies(len(s.moves) - s.lastHumanIndex())
	default:
		s.undoPlies(1)
	}
	s.undos++
	g.persistLocked(s)
	return s.view(), nil
}

// Resign ends the current game as a loss for the player to move (AI mode:
// the human). In hot-seat the player to move resigns.
func (g *GameService) Resign(id int64) (GameView, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, err := g.liveLocked(id)
	if err != nil {
		return GameView{}, err
	}
	if s.over() || s.puzzleFailed() {
		return GameView{}, ErrOver
	}
	g.stopSearchLocked()
	switch s.mode {
	case ModeAI, ModePuzzle:
		s.resigned = s.human
	default:
		s.resigned = s.state().ToMove
	}
	if _, err := g.recordLocked(s); err != nil {
		return GameView{}, err
	}
	return s.view(), nil
}

// GetDailyPuzzle reports today's puzzle and the solving streak.
func (g *GameService) GetDailyPuzzle() (DailyPuzzle, error) {
	g.mu.Lock()
	key := g.today()
	if _, err := g.puzzleLocked(key); err != nil {
		g.mu.Unlock()
		return DailyPuzzle{}, err
	}
	g.mu.Unlock()
	p, err := g.store.View()
	if err != nil {
		return DailyPuzzle{}, storageError(err)
	}
	return dailyInfo(p, key, g.now()), nil
}

func dailyInfo(p Profile, key string, now time.Time) DailyPuzzle {
	d := DailyPuzzle{Key: key, Solved: contains(p.Puzzles, key), Total: len(p.Puzzles)}
	day := now.UTC()
	if !d.Solved {
		day = day.AddDate(0, 0, -1)
	}
	for contains(p.Puzzles, day.Format("2006-01-02")) {
		d.Streak++
		day = day.AddDate(0, 0, -1)
	}
	return d
}

// Haptic triggers native haptics on mobile; a no-op on desktop.
// kind is one of light, medium, heavy, success, warning, error, selection.
func (g *GameService) Haptic(kind string) {
	switch kind {
	case "light", "medium", "heavy":
		application.Mobile.Haptic("impact-" + kind)
	case "success", "warning", "error", "selection":
		application.Mobile.Haptic(kind)
	}
}

// ---------------------------------------------------------------- internals

func (g *GameService) today() string { return g.now().UTC().Format("2006-01-02") }

func (g *GameService) puzzleLocked(key string) (engine.Puzzle, error) {
	if pz, ok := g.puzzles[key]; ok {
		return pz, nil
	}
	pz, ok := engine.GeneratePuzzle(key)
	if !ok {
		return engine.Puzzle{}, ErrNoPuzzle
	}
	g.puzzles[key] = pz
	return pz, nil
}

func (g *GameService) liveLocked(id int64) (*session, error) {
	g.restoreLocked()
	if g.sess == nil {
		return nil, ErrNoGame
	}
	if g.sess.id != id {
		return nil, ErrStaleGame
	}
	return g.sess, nil
}

func (g *GameService) beginSearchLocked(parent context.Context) (context.Context, context.CancelFunc) {
	g.stopSearchLocked()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	g.cancel = cancel
	return ctx, cancel
}

func (g *GameService) stopSearchLocked() {
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
}

// applyLocked plays move m in s, persists and records the result.
func (g *GameService) applyLocked(s *session, m int) (MoveResult, error) {
	if s.over() || s.puzzleFailed() {
		return MoveResult{}, ErrOver
	}
	player := s.state().ToMove
	ev, err := s.play(m)
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrGameOver):
			return MoveResult{}, ErrOver
		default:
			return MoveResult{}, ErrIllegal
		}
	}
	if s.over() || s.puzzleFailed() {
		// The move stands either way; a failed write is flagged on the view
		// so the UI can say the result was not saved.
		if _, err := g.recordLocked(s); err != nil {
			s.saveFailed = true
		}
	} else {
		g.persistLocked(s)
	}
	return MoveResult{Game: s.view(), Move: m, Player: player, Events: ev}, nil
}

// persistLocked saves the unfinished game. Failures are not fatal for play:
// the game continues in memory and the next write retries.
func (g *GameService) persistLocked(s *session) {
	_, _ = g.store.Update(func(p *Profile) error {
		p.Saved = s.saved()
		return nil
	})
}

// recordLocked writes a finished game into the profile exactly once.
func (g *GameService) recordLocked(s *session) (*Outcome, error) {
	if s.recorded {
		return s.outcome, nil
	}
	winner, _ := s.winner()
	st := s.state()
	out := &Outcome{NewAchievements: []string{}}
	_, err := g.store.Update(func(p *Profile) error {
		if g.sess == s || g.sess == nil {
			p.Saved = nil
		}
		switch s.mode {
		case ModeAI:
			ds := p.Stats.forDifficulty(s.difficulty)
			p.Stats.StonesPushed += int(st.Captured[s.human])
			switch winner {
			case s.human:
				out.Result = "win"
				ds.Wins++
				ds.Streak++
				ds.BestStreak = max(ds.BestStreak, ds.Streak)
				p.Stats.Streak++
				p.Stats.BestStreak = max(p.Stats.BestStreak, p.Stats.Streak)
				g.grantFor(p, out, "first_win")
				if s.difficulty == "magma" {
					g.grantFor(p, out, "magma")
				}
				if s.difficulty == "caldera" {
					g.grantFor(p, out, "caldera")
					if s.undos == 0 && s.hints == 0 {
						g.grantFor(p, out, "pure_caldera")
					}
				}
				if p.Stats.Streak >= 3 {
					g.grantFor(p, out, "streak3")
				}
				if st.Captured[engine.Other(s.human)] == 0 {
					g.grantFor(p, out, "unbroken")
				}
			case engine.Draw:
				out.Result = "draw"
				ds.Draws++
				ds.Streak = 0
				p.Stats.Streak = 0
			default:
				out.Result = "loss"
				ds.Losses++
				ds.Streak = 0
				p.Stats.Streak = 0
			}
			if s.maxCapture >= 2 {
				g.grantFor(p, out, "avalanche")
			}
			out.Streak, out.BestStreak = p.Stats.Streak, p.Stats.BestStreak
		case ModeHotseat:
			out.Result = "draw"
			if winner == engine.Lava || winner == engine.Ash {
				out.Result = "win"
			}
			p.Stats.HotseatGames++
			g.grantFor(p, out, "hotseat")
		case ModePuzzle:
			if winner == s.human {
				out.Result = "win"
				if !contains(p.Puzzles, s.puzzleKey) {
					p.Puzzles = append(p.Puzzles, s.puzzleKey)
				}
				g.grantFor(p, out, "puzzle")
			} else {
				out.Result = "loss"
			}
		}
		return nil
	})
	if err != nil {
		return nil, storageError(err)
	}
	s.recorded = true
	s.saveFailed = false
	s.outcome = out
	return out, nil
}

func (g *GameService) grantFor(p *Profile, out *Outcome, id string) {
	if grant(p, id) {
		out.NewAchievements = append(out.NewAchievements, id)
	}
}

// restoreLocked rebuilds the saved game once, after launch. Moves are
// replayed through the rules; anything illegal discards the save.
func (g *GameService) restoreLocked() {
	if g.restored {
		return
	}
	g.restored = true
	p, err := g.store.View()
	if err != nil || p.Saved == nil {
		return
	}
	sv := p.Saved
	s := &session{mode: sv.Mode, difficulty: sv.Difficulty, human: sv.Human, seed: sv.Seed, undos: sv.Undos, hints: sv.Hints, humanMoved: sv.HumanMoved}
	switch sv.Mode {
	case ModeAI, ModeHotseat:
		s.start = engine.NewState()
	case ModePuzzle:
		if sv.PuzzleKey != g.today() {
			return // yesterday's puzzle: start fresh
		}
		pz, err := g.puzzleLocked(sv.PuzzleKey)
		if err != nil {
			return
		}
		s.puzzleKey = pz.Key
		s.start = pz.State
		s.human = pz.State.ToMove
	default:
		return
	}
	s.states = []engine.State{s.start}
	for _, m := range sv.Moves {
		if _, err := s.play(m); err != nil {
			return
		}
	}
	if s.over() || s.puzzleFailed() {
		return
	}
	g.nextID++
	s.id = g.nextID
	g.sess = s
}
