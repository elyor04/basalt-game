package engine

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

// Params configures one search. A search stops at whichever budget runs out
// first; with Time == 0 and Workers == 1 it is fully deterministic for a
// given Seed.
type Params struct {
	Iterations int           // per worker; 0 means "until Time runs out"
	Time       time.Duration // wall-clock budget; 0 means "no time limit"
	Workers    int           // independent trees merged at the root
	Seed       uint64
	// Tactical enables exact win/block knowledge in the tree and the
	// rollouts. Without it the AI plays on raw random playouts.
	Tactical bool
	// Exploration is the UCT constant (default √2 · 0.7).
	Exploration float64
	// Temperature > 0 picks the final move with probability proportional to
	// visits^(1/T) instead of the most visited one — the easy AI's fallibility.
	Temperature float64
	// Blunder is the chance that the easy AI skips its root safety checks
	// (taking a win in one, avoiding a loss in one) for this move.
	Blunder float64
}

// Candidate is one root move with its statistics.
type Candidate struct {
	Move    int     `json:"move"`
	Visits  int     `json:"visits"`
	WinRate float64 `json:"winRate"`
}

// Result is the outcome of a search.
type Result struct {
	Move       int         `json:"move"`
	WinRate    float64     `json:"winRate"` // for the player to move
	Iterations int         `json:"iterations"`
	Elapsed    float64     `json:"elapsedMs"`
	Top        []Candidate `json:"top"`
	Forced     bool        `json:"forced"` // a win/block shortcut decided the move
}

type node struct {
	move       int16
	parent     int32
	firstChild int32
	nChildren  int16
	expanded   bool
	terminal   bool
	player     int8 // who made move
	visits     float64
	wins       float64 // from the perspective of the player who made move
}

type tree struct {
	nodes []node
	rng   *rand.Rand
	p     Params
	moves []int
	safe  []int
}

// Search runs MCTS with UCT from s for the player to move. It honours
// ctx cancellation and returns ctx.Err() if cancelled before any result.
func Search(ctx context.Context, s State, p Params) (Result, error) {
	start := time.Now()
	if p.Workers < 1 {
		p.Workers = 1
	}
	if p.Exploration == 0 {
		p.Exploration = 1.0
	}
	if p.Iterations == 0 && p.Time == 0 {
		p.Iterations = 1000
	}
	if s.Winner != NoWinner {
		return Result{Move: -1}, ErrGameOver
	}
	rootRng := rand.New(rand.NewPCG(p.Seed, p.Seed^0x9e3779b97f4a7c15))

	// Root shortcuts: take a win and never ignore a loss in one — unless
	// this search is allowed to blunder (the easy level) and does.
	careful := p.Blunder <= 0 || rootRng.Float64() >= p.Blunder
	if wins := s.WinningMoves(nil); len(wins) > 0 && careful {
		return Result{Move: wins[0], WinRate: 1, Forced: true, Elapsed: msSince(start)}, nil
	}
	rootMoves := s.LegalMoves(nil)
	if careful {
		rootMoves = s.SafeMoves(nil)
	}
	if len(rootMoves) == 1 {
		return Result{Move: rootMoves[0], WinRate: 0.5, Forced: true, Elapsed: msSince(start)}, nil
	}

	var deadline time.Time
	if p.Time > 0 {
		deadline = start.Add(p.Time)
	}
	trees := make([]*tree, p.Workers)
	var wg sync.WaitGroup
	for w := 0; w < p.Workers; w++ {
		t := &tree{p: p, rng: rand.New(rand.NewPCG(p.Seed+uint64(w)*7919, uint64(w)+1))}
		trees[w] = t
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.run(ctx, s, rootMoves, deadline)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{Move: -1}, err
	}

	// Merge root statistics.
	visits := map[int]float64{}
	wins := map[int]float64{}
	iters := 0
	for _, t := range trees {
		r := &t.nodes[0]
		iters += int(r.visits)
		for i := int32(0); i < int32(r.nChildren); i++ {
			c := &t.nodes[r.firstChild+i]
			visits[int(c.move)] += c.visits
			wins[int(c.move)] += c.wins
		}
	}
	cands := make([]Candidate, 0, len(visits))
	for m, v := range visits {
		wr := 0.0
		if v > 0 {
			wr = wins[m] / v
		}
		cands = append(cands, Candidate{Move: m, Visits: int(v), WinRate: wr})
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].Visits != cands[j].Visits {
			return cands[i].Visits > cands[j].Visits
		}
		return cands[i].Move < cands[j].Move
	})
	if len(cands) == 0 {
		return Result{Move: rootMoves[0], Elapsed: msSince(start)}, nil
	}
	best := cands[0]
	if p.Temperature > 0 {
		best = pickTemperature(cands, p.Temperature, rootRng)
	}
	top := cands
	if len(top) > 5 {
		top = top[:5]
	}
	return Result{
		Move: best.Move, WinRate: best.WinRate, Iterations: iters,
		Elapsed: msSince(start), Top: append([]Candidate(nil), top...),
	}, nil
}

func msSince(t time.Time) float64 { return float64(time.Since(t).Microseconds()) / 1000 }

func pickTemperature(c []Candidate, temp float64, rng *rand.Rand) Candidate {
	weights := make([]float64, len(c))
	total := 0.0
	for i, x := range c {
		w := math.Pow(float64(x.Visits)+1, 1/temp)
		weights[i] = w
		total += w
	}
	r := rng.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return c[i]
		}
	}
	return c[len(c)-1]
}

func (t *tree) run(ctx context.Context, root State, rootMoves []int, deadline time.Time) {
	t.nodes = make([]node, 1, 1<<14)
	t.nodes[0] = node{move: -1, parent: -1}
	// Each tree shuffles its own copy: workers must not share the slice.
	t.expandWith(0, append([]int(nil), rootMoves...), root.ToMove)
	t.moves = make([]int, 0, NumCells+1)
	t.safe = make([]int, 0, NumCells+1)
	for it := 0; t.p.Iterations == 0 || it < t.p.Iterations; it++ {
		if it&63 == 0 {
			if ctx.Err() != nil {
				return
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return
			}
		}
		t.iterate(root)
	}
}

func (t *tree) expandWith(n int32, moves []int, mover int8) {
	first := int32(len(t.nodes))
	// Random child order so ties between unvisited children are fair.
	t.rng.Shuffle(len(moves), func(i, j int) { moves[i], moves[j] = moves[j], moves[i] })
	for _, m := range moves {
		t.nodes = append(t.nodes, node{move: int16(m), parent: n, player: mover})
	}
	t.nodes[n].firstChild = first
	t.nodes[n].nChildren = int16(len(moves))
	t.nodes[n].expanded = true
}

func (t *tree) iterate(root State) {
	s := root
	n := int32(0)
	// Selection.
	for t.nodes[n].expanded && !t.nodes[n].terminal && t.nodes[n].nChildren > 0 {
		n = t.selectChild(n)
		playInto(&s, int(t.nodes[n].move))
	}
	// Expansion.
	nd := &t.nodes[n]
	if s.Winner != NoWinner {
		nd.terminal = true
	} else if !nd.expanded && nd.visits >= 1 {
		var moves []int
		if t.p.Tactical {
			if w := s.WinningMoves(t.moves); len(w) > 0 {
				moves = append([]int(nil), w[0])
			} else {
				moves = append([]int(nil), s.SafeMoves(t.safe)...)
			}
		} else {
			moves = append([]int(nil), s.LegalMoves(t.moves)...)
		}
		t.expandWith(n, moves, s.ToMove)
		n = t.selectChild(n)
		playInto(&s, int(t.nodes[n].move))
	}
	// Simulation.
	winner := s.Winner
	if winner == NoWinner {
		winner = t.rollout(s)
	}
	// Backpropagation: each node stores wins for the player who moved into
	// it, i.e. the player to move at its parent.
	for n >= 0 {
		nd := &t.nodes[n]
		nd.visits++
		if nd.parent >= 0 {
			switch winner {
			case nd.player:
				nd.wins++
			case Draw:
				nd.wins += 0.5
			}
		}
		n = nd.parent
	}
}

func playInto(s *State, m int) { s.apply(m, nil) }

func (t *tree) selectChild(n int32) int32 {
	nd := &t.nodes[n]
	logN := math.Log(nd.visits + 1)
	best := int32(-1)
	bestScore := math.Inf(-1)
	c := t.p.Exploration
	for i := int32(0); i < int32(nd.nChildren); i++ {
		ci := nd.firstChild + i
		ch := &t.nodes[ci]
		if ch.visits == 0 {
			return ci
		}
		score := ch.wins/ch.visits + c*math.Sqrt(logN/ch.visits)
		if score > bestScore {
			bestScore = score
			best = ci
		}
	}
	return best
}

// rollout plays the game out with a light policy and returns the winner.
func (t *tree) rollout(s State) int8 {
	rng := t.rng
	var cnt [NumCells]int8
	var empties [NumCells]int
	var near [NumCells]int
	for guard := 0; guard < 200 && s.Winner == NoWinner; guard++ {
		p := s.ToMove
		foe := Other(p)
		move := -1
		if t.p.Tactical {
			// 1. Win now (a line, or enough captures).
			// 2. Block the opponent's open line.
			block := -1
			for li := range lines {
				l := lines[li]
				own, opp, empty := 0, 0, -1
				for _, c := range l {
					switch s.Cells[c] {
					case p:
						own++
					case foe:
						opp++
					case Empty:
						empty = c
					}
				}
				if empty < 0 {
					continue
				}
				if own == LineLength-1 {
					move = empty
					break
				}
				if opp == LineLength-1 {
					block = empty
				}
			}
			if move < 0 {
				s.captureCounts(p, &cnt)
				need := int8(CaptureGoal) - s.Captured[p]
				nCap := 0
				capMove := -1
				for c := 0; c < NumCells; c++ {
					if cnt[c] >= need {
						move = c
						break
					}
					if cnt[c] > 0 {
						nCap++
						if rng.IntN(nCap) == 0 {
							capMove = c
						}
					}
				}
				if move < 0 && block >= 0 {
					move = block
				}
				if move < 0 && capMove >= 0 && rng.IntN(2) == 0 {
					move = capMove
				}
			}
		}
		if move < 0 {
			ne, nn := 0, 0
			for c := 0; c < NumCells; c++ {
				if s.Cells[c] != Empty {
					continue
				}
				empties[ne] = c
				ne++
				for d := 0; d < 6; d++ {
					x := neighbor[c][d]
					if x >= 0 && (s.Cells[x] == Lava || s.Cells[x] == Ash) {
						near[nn] = c
						nn++
						break
					}
				}
			}
			if ne == 0 {
				break
			}
			if nn > 0 && rng.IntN(3) != 0 {
				move = near[rng.IntN(nn)]
			} else {
				move = empties[rng.IntN(ne)]
			}
		}
		s.apply(move, nil)
	}
	if s.Winner == NoWinner {
		return Draw
	}
	return s.Winner
}
