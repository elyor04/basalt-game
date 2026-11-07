package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const profileVersion = 1

// Difficulties, easiest first.
var difficulties = []string{"ember", "magma", "caldera"}

// Settings are player preferences persisted with the profile.
type Settings struct {
	Language     string  `json:"language"`     // "", "en", "ru" or "uz"; "" follows the device
	Volume       float64 `json:"volume"`       // 0..1
	Haptics      bool    `json:"haptics"`      // native vibration on mobile
	ConfirmTouch bool    `json:"confirmTouch"` // touch: tap to preview, tap again to place
	ShowPreview  bool    `json:"showPreview"`  // preview pushes before placing
	FaceToFace   bool    `json:"faceToFace"`   // hot-seat: flip the far player's panel
	Difficulty   string  `json:"difficulty"`   // last chosen AI level
	Color        string  `json:"color"`        // last chosen side: "lava", "ash" or "random"
}

// DiffStats are the results against one AI level.
type DiffStats struct {
	Wins       int `json:"wins"`
	Losses     int `json:"losses"`
	Draws      int `json:"draws"`
	Streak     int `json:"streak"`     // current win streak at this level
	BestStreak int `json:"bestStreak"` // longest win streak at this level
}

// Stats holds every counter shown on the profile screen.
type Stats struct {
	Ember        DiffStats `json:"ember"`
	Magma        DiffStats `json:"magma"`
	Caldera      DiffStats `json:"caldera"`
	Streak       int       `json:"streak"`     // current win streak against any AI
	BestStreak   int       `json:"bestStreak"` // longest win streak against any AI
	HotseatGames int       `json:"hotseatGames"`
	StonesPushed int       `json:"stonesPushed"` // enemy stones you pushed off vs the AI
}

// SavedGame lets an unfinished game survive the app being closed. Only the
// moves are stored; the position is rebuilt by replaying them through the
// rules, so a tampered file cannot produce an illegal position.
type SavedGame struct {
	Mode       string `json:"mode"`
	Difficulty string `json:"difficulty"`
	Human      int8   `json:"human"`
	Moves      []int  `json:"moves"`
	Undos      int    `json:"undos"`
	Hints      int    `json:"hints"`
	PuzzleKey  string `json:"puzzleKey"`
	Seed       uint64 `json:"seed"`
	HumanMoved bool   `json:"humanMoved"`
}

// Profile is everything persisted between sessions.
type Profile struct {
	Version      int        `json:"version"`
	Stats        Stats      `json:"stats"`
	Achievements []string   `json:"achievements"`
	Puzzles      []string   `json:"puzzles"` // UTC dates of solved daily puzzles
	Settings     Settings   `json:"settings"`
	Saved        *SavedGame `json:"saved"`
}

func defaultSettings() Settings {
	return Settings{
		Volume:       0.7,
		Haptics:      true,
		ConfirmTouch: true,
		ShowPreview:  true,
		Difficulty:   "ember",
		Color:        "lava",
	}
}

func newProfile() Profile {
	return Profile{
		Version:      profileVersion,
		Achievements: []string{},
		Puzzles:      []string{},
		Settings:     defaultSettings(),
	}
}

// DiffStats returns a pointer to the stats of a difficulty, or nil.
func (st *Stats) forDifficulty(d string) *DiffStats {
	switch d {
	case "ember":
		return &st.Ember
	case "magma":
		return &st.Magma
	case "caldera":
		return &st.Caldera
	}
	return nil
}

// normalize repairs a profile loaded from disk so the rest of the code can
// rely on its invariants.
func (p *Profile) normalize() {
	if p.Achievements == nil {
		p.Achievements = []string{}
	}
	if p.Puzzles == nil {
		p.Puzzles = []string{}
	}
	// Drop unknown achievement ids and duplicates.
	seen := map[string]bool{}
	kept := p.Achievements[:0]
	for _, id := range p.Achievements {
		if _, ok := findAchievement(id); ok && !seen[id] {
			seen[id] = true
			kept = append(kept, id)
		}
	}
	p.Achievements = kept
	// Keep the most recent 400 solved puzzle dates, unique and sorted.
	seen = map[string]bool{}
	dates := []string{}
	for _, d := range p.Puzzles {
		if _, err := time.Parse("2006-01-02", d); err == nil && !seen[d] {
			seen[d] = true
			dates = append(dates, d)
		}
	}
	sort.Strings(dates)
	if len(dates) > 400 {
		dates = dates[len(dates)-400:]
	}
	p.Puzzles = dates
	for _, d := range difficulties {
		ds := p.Stats.forDifficulty(d)
		ds.Wins = max(ds.Wins, 0)
		ds.Losses = max(ds.Losses, 0)
		ds.Draws = max(ds.Draws, 0)
		ds.Streak = clampInt(ds.Streak, 0, ds.Wins)
		ds.BestStreak = clampInt(ds.BestStreak, ds.Streak, max(ds.Wins, ds.Streak))
	}
	p.Stats.Streak = max(p.Stats.Streak, 0)
	p.Stats.BestStreak = max(p.Stats.BestStreak, p.Stats.Streak)
	p.Stats.HotseatGames = max(p.Stats.HotseatGames, 0)
	p.Stats.StonesPushed = max(p.Stats.StonesPushed, 0)
	if p.Version == 0 {
		p.Settings = defaultSettings()
	}
	p.Settings = sanitizeSettings(p.Settings)
	if p.Saved != nil && !validSaved(p.Saved) {
		p.Saved = nil
	}
	p.Version = profileVersion
}

func validSaved(s *SavedGame) bool {
	switch s.Mode {
	case ModeAI:
		if s.Human != 1 && s.Human != 2 || !contains(difficulties, s.Difficulty) {
			return false
		}
	case ModeHotseat, ModePuzzle:
	default:
		return false
	}
	return len(s.Moves) <= 400 && s.Undos >= 0 && s.Hints >= 0
}

func sanitizeSettings(s Settings) Settings {
	s.Volume = clampFloat(s.Volume, 0, 1)
	switch s.Language {
	case "", "en", "ru", "uz":
	default:
		s.Language = ""
	}
	if !contains(difficulties, s.Difficulty) {
		s.Difficulty = "ember"
	}
	switch s.Color {
	case "lava", "ash", "random":
	default:
		s.Color = "lava"
	}
	return s
}

// clone returns a deep copy so callers can never mutate the stored profile.
func (p Profile) clone() Profile {
	c := p
	c.Achievements = append([]string{}, p.Achievements...)
	c.Puzzles = append([]string{}, p.Puzzles...)
	if p.Saved != nil {
		sg := *p.Saved
		sg.Moves = append([]int{}, p.Saved.Moves...)
		c.Saved = &sg
	}
	return c
}

// Store persists a single profile as JSON. All access goes through the mutex.
type Store struct {
	mu      sync.Mutex
	dirFunc func() (string, error)
	now     func() time.Time
	path    string
	profile *Profile
}

// NewStore creates a store that resolves its directory lazily with dirFunc.
// Resolution is deferred because on mobile the native storage path is only
// available once the platform bridge is up.
func NewStore(dirFunc func() (string, error)) *Store {
	return &Store{dirFunc: dirFunc, now: time.Now}
}

func (s *Store) ensureLoaded() error {
	if s.profile != nil {
		return nil
	}
	dir, err := s.dirFunc()
	if err != nil {
		return fmt.Errorf("resolve save directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create save directory: %w", err)
	}
	s.path = filepath.Join(dir, "profile.json")
	p := newProfile()
	fresh := false
	data, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if jerr := json.Unmarshal(data, &p); jerr != nil {
			// Keep the unreadable file for inspection instead of silently
			// overwriting the player's progress.
			stamp := s.now().UTC().Format("20060102T150405")
			if rerr := os.Rename(s.path, strings.TrimSuffix(s.path, ".json")+"."+stamp+".corrupt"); rerr != nil {
				// Never overwrite progress we could not set aside.
				return fmt.Errorf("keep corrupt profile: %w", rerr)
			}
			p = newProfile()
			fresh = true
		}
	case errors.Is(err, os.ErrNotExist):
		fresh = true
	default:
		return fmt.Errorf("read profile: %w", err)
	}
	p.normalize()
	if fresh {
		// Write the fresh profile now so an unwritable save directory is
		// reported at launch rather than after a game.
		if werr := s.write(p); werr != nil {
			return werr
		}
	}
	s.profile = &p
	return nil
}

// View returns a copy of the current profile.
func (s *Store) View() (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Profile{}, err
	}
	return s.profile.clone(), nil
}

// Update applies fn to a copy of the profile and persists it atomically. If
// fn or the write fails, nothing changes (in memory or on disk).
func (s *Store) Update(fn func(p *Profile) error) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoaded(); err != nil {
		return Profile{}, err
	}
	next := s.profile.clone()
	if err := fn(&next); err != nil {
		return s.profile.clone(), err
	}
	next.normalize()
	if err := s.write(next); err != nil {
		return s.profile.clone(), err
	}
	s.profile = &next
	return next.clone(), nil
}

func (s *Store) write(p Profile) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile: %w", err)
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write profile: %w", err)
	}
	// Flush to disk before the rename so a crash or a mobile OS kill can
	// never leave an empty profile.json behind.
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync profile: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close profile: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit profile: %w", err)
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v != v { // NaN
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
