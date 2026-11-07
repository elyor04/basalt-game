package game

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"basalt/internal/engine"
)

func testService(t *testing.T) (*GameService, string) {
	t.Helper()
	dir := t.TempDir()
	g := newService(NewStore(func() (string, error) { return dir, nil }))
	g.now = func() time.Time { return time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC) }
	g.levels = func(d string) Level {
		return Level{Params: engine.Params{Iterations: 200, Tactical: d != "ember"}}
	}
	return g, dir
}

func code(err error) string {
	var ge *GameError
	if errors.As(err, &ge) {
		return ge.Code
	}
	return ""
}

func mustGame(t *testing.T, g *GameService, o NewGameOptions) GameView {
	t.Helper()
	v, err := g.NewGame(o)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// playOut finishes an AI game with the human playing the first free hex.
func playOut(t *testing.T, g *GameService, v GameView) GameView {
	t.Helper()
	for i := 0; i < 200 && v.Winner == 0; i++ {
		if v.AITurn {
			r, err := g.AIMove(context.Background(), v.ID)
			if err != nil {
				t.Fatal(err)
			}
			v = r.Game
			continue
		}
		cell := -1
		for c, x := range v.Cells {
			if x == 0 {
				cell = c
				break
			}
		}
		r, err := g.Play(v.ID, cell)
		if err != nil {
			t.Fatal(err)
		}
		v = r.Game
	}
	if v.Winner == 0 {
		t.Fatal("game did not end")
	}
	return v
}

func TestNewGameValidatesOptions(t *testing.T) {
	g, _ := testService(t)
	for _, o := range []NewGameOptions{
		{Mode: "chess"},
		{Mode: ModeAI, Difficulty: "volcano", Color: "lava"},
		{Mode: ModeAI, Difficulty: "ember", Color: "green"},
	} {
		if _, err := g.NewGame(o); code(err) != "bad_option" {
			t.Fatalf("%+v: want bad_option, got %v", o, err)
		}
	}
}

func TestPlayValidation(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "ember", Color: "lava"})
	if _, err := g.Play(v.ID+1, 30); code(err) != "stale_game" {
		t.Fatalf("want stale_game, got %v", err)
	}
	if _, err := g.AIMove(context.Background(), v.ID); code(err) != "not_your_turn" {
		t.Fatalf("AI must not move on the human's turn, got %v", err)
	}
	r, err := g.Play(v.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Game.AITurn || r.Events[0].Kind != engine.EvPlace {
		t.Fatalf("unexpected result %+v", r)
	}
	if _, err := g.Play(v.ID, 31); code(err) != "not_your_turn" {
		t.Fatalf("human must not move on the AI's turn, got %v", err)
	}
	if _, err := g.Play(v.ID, 999); code(err) != "not_your_turn" {
		t.Fatalf("got %v", err)
	}
	r, err = g.AIMove(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Play(v.ID, 30); code(err) != "illegal_move" {
		t.Fatalf("occupied hex accepted: %v", err)
	}
	if _, err := g.Play(v.ID, -4); code(err) != "illegal_move" {
		t.Fatalf("bad index accepted: %v", err)
	}
}

func TestAIOpensWhenHumanIsTide(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "magma", Color: "ash"})
	if !v.AITurn || v.Human != engine.Ash {
		t.Fatalf("AI should open: %+v", v)
	}
	if _, err := g.Play(v.ID, 30); code(err) != "not_your_turn" {
		t.Fatalf("got %v", err)
	}
	r, err := g.AIMove(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Player != engine.Lava || r.Game.AITurn {
		t.Fatalf("unexpected %+v", r.Game)
	}
}

func TestUndoTakesBackHumanAndAIMoves(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "ember", Color: "lava"})
	if v.CanUndo {
		t.Fatal("nothing to undo yet")
	}
	if _, err := g.Undo(v.ID); code(err) != "cannot_undo" {
		t.Fatalf("got %v", err)
	}
	r, _ := g.Play(v.ID, 30)
	// Undo while the AI is "thinking" (its turn): only the human move goes.
	u, err := g.Undo(r.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Ply != 0 || u.Undos != 1 {
		t.Fatalf("want ply 0, got %d", u.Ply)
	}
	r, _ = g.Play(v.ID, 30)
	r, _ = g.AIMove(context.Background(), v.ID)
	r, _ = g.Play(v.ID, firstFree(r.Game))
	r, _ = g.AIMove(context.Background(), v.ID)
	if r.Game.Ply != 4 {
		t.Fatalf("want ply 4, got %d", r.Game.Ply)
	}
	u, _ = g.Undo(v.ID)
	if u.Ply != 2 || u.AITurn {
		t.Fatalf("undo should return to the human's turn at ply 2, got %d", u.Ply)
	}
}

func firstFree(v GameView) int {
	for c, x := range v.Cells {
		if x == 0 {
			return c
		}
	}
	return -1
}

func TestUndoCancelsThinkingAI(t *testing.T) {
	g, _ := testService(t)
	g.levels = func(string) Level { return Level{Params: engine.Params{Time: 5 * time.Second, Tactical: true}} }
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "caldera", Color: "lava"})
	r, _ := g.Play(v.ID, 30)
	done := make(chan error, 1)
	go func() {
		_, err := g.AIMove(context.Background(), r.Game.ID)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if _, err := g.Undo(v.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if code(err) != "cancelled" {
			t.Fatalf("want cancelled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AI did not stop after undo")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation too slow")
	}
	cur, _ := g.CurrentGame()
	if cur.Ply != 0 {
		t.Fatalf("cancelled AI must not have moved, ply=%d", cur.Ply)
	}
}

func TestJSCancelStopsAI(t *testing.T) {
	g, _ := testService(t)
	g.levels = func(string) Level { return Level{Params: engine.Params{Time: 5 * time.Second, Tactical: true}} }
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "caldera", Color: "ash"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := g.AIMove(ctx, v.ID); code(err) != "cancelled" {
		t.Fatalf("want cancelled, got %v", err)
	}
}

func TestResultRecordedOnceAndAchievements(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "magma", Color: "lava"})
	v = playOut(t, g, v)
	if v.Outcome == nil {
		t.Fatal("finished game must carry an outcome")
	}
	p, _ := g.GetProfile()
	total := p.Stats.Magma.Wins + p.Stats.Magma.Losses + p.Stats.Magma.Draws
	if total != 1 {
		t.Fatalf("want exactly one recorded game, got %d", total)
	}
	if p.Saved != nil {
		t.Fatal("finished game must not stay saved")
	}
	// No more moves, undo or resign after the end.
	if _, err := g.Play(v.ID, firstFree(v)); code(err) != "game_over" && code(err) != "not_your_turn" {
		t.Fatalf("got %v", err)
	}
	if _, err := g.Undo(v.ID); code(err) != "cannot_undo" {
		t.Fatalf("got %v", err)
	}
	if _, err := g.Resign(v.ID); code(err) != "game_over" {
		t.Fatalf("got %v", err)
	}
	p2, _ := g.GetProfile()
	if p2.Stats.Magma != p.Stats.Magma {
		t.Fatal("stats changed after the game ended")
	}
}

func TestRecordWinGrantsAchievements(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "caldera", Color: "lava"})
	// Force a won position: give the session a state where the human's
	// next placement completes a line.
	g.mu.Lock()
	s := g.sess
	st := engine.NewState()
	for _, c := range []int{26, 27, 28, 29} {
		st.Cells[c] = engine.Lava
	}
	st.Ply = 8
	s.states = []engine.State{st}
	s.start = st
	g.mu.Unlock()
	r, err := g.Play(v.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if r.Game.Winner != engine.Lava || r.Game.Reason != "line" || len(r.Game.WinLine) != engine.LineLength {
		t.Fatalf("want line win, got %+v", r.Game)
	}
	o := r.Game.Outcome
	if o == nil || o.Result != "win" {
		t.Fatalf("outcome %+v", o)
	}
	want := map[string]bool{"first_win": true, "caldera": true, "pure_caldera": true, "unbroken": true}
	for _, a := range o.NewAchievements {
		delete(want, a)
	}
	if len(want) != 0 {
		t.Fatalf("missing achievements %v (got %v)", want, o.NewAchievements)
	}
	p, _ := g.GetProfile()
	if p.Stats.Caldera.Wins != 1 || p.Stats.Streak != 1 || p.Stats.Caldera.BestStreak != 1 {
		t.Fatalf("stats %+v", p.Stats)
	}
}

func TestResignAndAbandonCountAsLosses(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "ember", Color: "lava"})
	// Starting over before moving costs nothing.
	v = mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "ember", Color: "lava"})
	p, _ := g.GetProfile()
	if p.Stats.Ember.Losses != 0 {
		t.Fatal("abandoning an untouched game must not count")
	}
	g.Play(v.ID, 30)
	g.AIMove(context.Background(), v.ID)
	v = mustGame(t, g, NewGameOptions{Mode: ModeHotseat})
	p, _ = g.GetProfile()
	if p.Stats.Ember.Losses != 1 {
		t.Fatalf("abandoning a started game is a resignation; losses=%d", p.Stats.Ember.Losses)
	}
	// Undoing every move first does not escape the forfeit.
	v2 := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "magma", Color: "lava"})
	g.Play(v2.ID, 30)
	g.AIMove(context.Background(), v2.ID)
	if u, err := g.Undo(v2.ID); err != nil || u.Ply != 0 || !u.Forfeit {
		t.Fatalf("undo to start: %+v %v", u, err)
	}
	v = mustGame(t, g, NewGameOptions{Mode: ModeHotseat})
	p, _ = g.GetProfile()
	if p.Stats.Magma.Losses != 1 {
		t.Fatalf("undo-then-abandon must still forfeit; magma losses=%d", p.Stats.Magma.Losses)
	}
	r, err := g.Resign(v.ID)
	if err != nil || r.Winner != engine.Ash || r.Reason != "resign" {
		t.Fatalf("hot-seat resign by Lava: %+v %v", r, err)
	}
	p, _ = g.GetProfile()
	if p.Stats.HotseatGames != 1 || !contains(p.Achievements, "hotseat") {
		t.Fatalf("hot-seat game not counted: %+v", p.Stats)
	}
}

func TestSavedGameRestores(t *testing.T) {
	dir := t.TempDir()
	mk := func() *GameService {
		g := newService(NewStore(func() (string, error) { return dir, nil }))
		g.levels = func(string) Level { return Level{Params: engine.Params{Iterations: 100}} }
		return g
	}
	g := mk()
	v := mustGame(t, g, NewGameOptions{Mode: ModeAI, Difficulty: "magma", Color: "lava"})
	g.Play(v.ID, 30)
	r, _ := g.AIMove(context.Background(), v.ID)
	g.Play(v.ID, firstFree(r.Game))
	before, _ := g.CurrentGame()

	g2 := mk()
	after, err := g2.CurrentGame()
	if err != nil {
		t.Fatal(err)
	}
	if after.Ply != before.Ply || after.Difficulty != "magma" || !after.AITurn {
		t.Fatalf("restored %+v, want ply %d", after, before.Ply)
	}
	for i := range after.Cells {
		if after.Cells[i] != before.Cells[i] {
			t.Fatal("restored board differs")
		}
	}
	// A tampered save with an illegal move is discarded, not trusted.
	g2.store.Update(func(p *Profile) error {
		p.Saved.Moves = append(p.Saved.Moves, p.Saved.Moves[0])
		return nil
	})
	g3 := mk()
	if _, err := g3.CurrentGame(); code(err) != "no_game" {
		t.Fatalf("tampered save must be dropped, got %v", err)
	}
}

func TestPuzzleFlow(t *testing.T) {
	g, _ := testService(t)
	info, err := g.GetDailyPuzzle()
	if err != nil {
		t.Fatal(err)
	}
	if info.Key != "2026-11-07" || info.Solved {
		t.Fatalf("info %+v", info)
	}
	v := mustGame(t, g, NewGameOptions{Mode: ModePuzzle})
	h, err := g.Hint(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.Play(v.ID, h.Cell)
	if err != nil {
		t.Fatal(err)
	}
	r, err = g.AIMove(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	h, _ = g.Hint(context.Background(), v.ID)
	r, err = g.Play(v.ID, h.Cell)
	if err != nil {
		t.Fatal(err)
	}
	if r.Game.Winner != v.Human || r.Game.Outcome == nil || r.Game.Outcome.Result != "win" {
		t.Fatalf("puzzle not solved: %+v", r.Game)
	}
	info, _ = g.GetDailyPuzzle()
	if !info.Solved || info.Streak != 1 {
		t.Fatalf("info %+v", info)
	}
}

func TestPuzzleFailsAfterTwoMoves(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModePuzzle})
	pz := g.puzzles[v.PuzzleKey]
	wrong := -1
	for c, x := range v.Cells {
		if x == 0 && c != pz.Solution {
			wrong = c
			break
		}
	}
	r, err := g.Play(v.ID, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if r.Game.Winner == 0 {
		r, err = g.AIMove(context.Background(), v.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.Game.Winner == 0 {
		r, err = g.Play(v.ID, firstFree(r.Game))
		if err != nil {
			t.Fatal(err)
		}
	}
	if r.Game.Winner == v.Human {
		t.Skip("the arbitrary moves happened to win; nothing to assert")
	}
	if !r.Game.Failed && r.Game.Winner == 0 {
		t.Fatalf("puzzle should be failed: %+v", r.Game)
	}
	if r.Game.AITurn {
		t.Fatal("the defender must not move after the puzzle is failed")
	}
	if _, err := g.AIMove(context.Background(), v.ID); err == nil {
		t.Fatal("AIMove after a failed puzzle must be refused")
	}
	p, _ := g.GetProfile()
	if len(p.Puzzles) != 0 {
		t.Fatal("failed puzzle must not count")
	}
}

func TestConcurrentCallsAreSafe(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeHotseat})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g.Play(v.ID, i)
			g.CurrentGame()
			g.GetProfile()
			g.Hint(context.Background(), v.ID)
		}(i)
	}
	wg.Wait()
	cur, _ := g.CurrentGame()
	stones := 0
	for _, c := range cur.Cells {
		if c == engine.Lava || c == engine.Ash {
			stones++
		}
	}
	if cur.Ply != len(cur.History) {
		t.Fatalf("history out of sync: ply %d, %d records", cur.Ply, len(cur.History))
	}
}

func TestErrorsSerializeWithCode(t *testing.T) {
	b, _ := json.Marshal(ErrNotYourTurn)
	if !strings.Contains(string(b), `"code":"not_your_turn"`) {
		t.Fatalf("serialised %s", b)
	}
}

func TestGameViewJSONHasNoNulls(t *testing.T) {
	g, _ := testService(t)
	v := mustGame(t, g, NewGameOptions{Mode: ModeHotseat})
	b, _ := json.Marshal(v)
	for _, k := range []string{`"cells":null`, `"winLine":null`, `"history":null`, `"captured":null`} {
		if strings.Contains(string(b), k) {
			t.Fatalf("view has %s", k)
		}
	}
}

func TestSettingsSanitised(t *testing.T) {
	g, _ := testService(t)
	p, err := g.SaveSettings(Settings{Language: "fr", Volume: 3, Difficulty: "lava", Color: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := p.Settings
	if s.Language != "" || s.Volume != 1 || s.Difficulty != "ember" || s.Color != "lava" {
		t.Fatalf("not sanitised: %+v", s)
	}
}

func TestResetKeepsSettings(t *testing.T) {
	g, _ := testService(t)
	g.SaveSettings(Settings{Language: "uz", Volume: 0.2, Difficulty: "magma", Color: "ash"})
	v := mustGame(t, g, NewGameOptions{Mode: ModeHotseat})
	g.Resign(v.ID)
	p, err := g.ResetProfile()
	if err != nil {
		t.Fatal(err)
	}
	if p.Stats.HotseatGames != 0 || len(p.Achievements) != 0 || p.Settings.Language != "uz" {
		t.Fatalf("reset wrong: %+v", p)
	}
	if _, err := g.CurrentGame(); code(err) != "no_game" {
		t.Fatal("reset should drop the game in progress")
	}
}

func TestStoreCorruptFileIsKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	st := NewStore(func() (string, error) { return dir, nil })
	st.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	p, err := st.View()
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != profileVersion {
		t.Fatal("fresh profile expected")
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.20260102T030405.corrupt")); err != nil {
		t.Fatalf("corrupt file not kept: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("fresh profile not written")
	}
}

func TestStoreRollbackOnFailedUpdate(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(func() (string, error) { return dir, nil })
	st.Update(func(p *Profile) error { p.Stats.HotseatGames = 3; return nil })
	_, err := st.Update(func(p *Profile) error {
		p.Stats.HotseatGames = 99
		return errors.New("nope")
	})
	if err == nil {
		t.Fatal("want error")
	}
	p, _ := st.View()
	if p.Stats.HotseatGames != 3 {
		t.Fatalf("failed update leaked: %d", p.Stats.HotseatGames)
	}
	// A failed write (directory gone read-only is hard to fake portably, so
	// make the target path a directory) must also leave memory unchanged.
	os.Remove(filepath.Join(dir, "profile.json"))
	os.Mkdir(filepath.Join(dir, "profile.json"), 0o755)
	if _, err := st.Update(func(p *Profile) error { p.Stats.HotseatGames = 7; return nil }); err == nil {
		t.Fatal("write into a directory should fail")
	}
	p, _ = st.View()
	if p.Stats.HotseatGames != 3 {
		t.Fatalf("failed write leaked into memory: %d", p.Stats.HotseatGames)
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestStoreCloneIsDeep(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(func() (string, error) { return dir, nil })
	st.Update(func(p *Profile) error {
		p.Achievements = append(p.Achievements, "hotseat")
		p.Saved = &SavedGame{Mode: ModeHotseat, Moves: []int{1, 2}}
		return nil
	})
	p, _ := st.View()
	p.Achievements[0] = "x"
	p.Saved.Moves[0] = 42
	q, _ := st.View()
	if q.Achievements[0] != "hotseat" || q.Saved.Moves[0] != 1 {
		t.Fatal("View leaked internal state")
	}
}

func TestStoreNormalizesTamperedProfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "profile.json"), []byte(`{"version":1,"achievements":["bogus","hotseat","hotseat"],
		"puzzles":["2026-01-01","nope","2026-01-01"],"stats":{"ember":{"wins":-3,"streak":9}},
		"settings":{"language":"de","volume":-1,"difficulty":"magma","color":"ash"},
		"saved":{"mode":"ai","difficulty":"lava","human":1,"moves":[]}}`), 0o644)
	st := NewStore(func() (string, error) { return dir, nil })
	p, err := st.View()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Achievements) != 1 || len(p.Puzzles) != 1 || p.Stats.Ember.Wins != 0 || p.Stats.Ember.Streak != 0 {
		t.Fatalf("not normalised: %+v", p)
	}
	if p.Settings.Language != "" || p.Settings.Volume != 0 || p.Saved != nil {
		t.Fatalf("not normalised: %+v", p.Settings)
	}
}

func TestDailyStreak(t *testing.T) {
	now := time.Date(2026, 11, 7, 1, 0, 0, 0, time.UTC)
	p := newProfile()
	p.Puzzles = []string{"2026-11-04", "2026-11-05", "2026-11-06"}
	d := dailyInfo(p, "2026-11-07", now)
	if d.Solved || d.Streak != 3 {
		t.Fatalf("%+v", d)
	}
	p.Puzzles = append(p.Puzzles, "2026-11-07")
	d = dailyInfo(p, "2026-11-07", now)
	if !d.Solved || d.Streak != 4 || d.Total != 4 {
		t.Fatalf("%+v", d)
	}
}

// The Android transport flattens a bound method's error to its Error() string
// and drops the structured cause, so the code must survive in the text.
func TestGameErrorTextCarriesCode(t *testing.T) {
	for _, e := range []*GameError{ErrNoGame, ErrStaleGame, ErrCancelled, storageError(errors.New("disk full")).(*GameError)} {
		if !strings.HasSuffix(e.Error(), "["+e.Code+"]") {
			t.Errorf("%q does not end with [%s]", e.Error(), e.Code)
		}
	}
	b, _ := json.Marshal(ErrNoGame)
	if string(b) != `{"code":"no_game","message":"there is no game in progress"}` {
		t.Errorf("JSON cause changed: %s", b)
	}
}
