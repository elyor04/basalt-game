package game

// Achievement is identified by ID only; the UI owns every visible string.
type Achievement struct {
	ID string `json:"id"`
}

// achievements in display order.
var achievements = []Achievement{
	{"first_win"},    // beat any AI level
	{"magma"},        // beat Magma
	{"caldera"},      // beat Caldera
	{"pure_caldera"}, // beat Caldera without undo or hints
	{"streak3"},      // win three AI games in a row
	{"avalanche"},    // push two or more stones off with one placement
	{"unbroken"},     // beat an AI without losing a single stone
	{"hotseat"},      // finish a two-player game
	{"puzzle"},       // solve a daily puzzle
	{"lan"},          // finish a game against another device
}

func findAchievement(id string) (Achievement, bool) {
	for _, a := range achievements {
		if a.ID == id {
			return a, true
		}
	}
	return Achievement{}, false
}

// grant unlocks id and reports whether it was new.
func grant(p *Profile, id string) bool {
	if contains(p.Achievements, id) {
		return false
	}
	if _, ok := findAchievement(id); !ok {
		return false
	}
	p.Achievements = append(p.Achievements, id)
	return true
}
