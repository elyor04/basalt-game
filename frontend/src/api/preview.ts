// Browser-only stand-in for the Go GameService, used when the page runs
// without the Wails runtime (vite dev, devtools QA). It uses the TypeScript
// rules port, a simple greedy opponent and localStorage. The Go service is
// the real implementation; this mirrors it closely enough to exercise every
// screen, including results, achievements and the daily puzzle.

import type { Backend, DailyPuzzle, GameView, Hint, MoveResult, NewGameOptions, Pending, Profile, Settings } from "./backend";
import { BackendError } from "./backend";
import {
  ASH, CAPTURE_GOAL, DRAW, EMPTY, LAVA, LINE_LENGTH, NO_WINNER, NUM_CELLS, Player, REASON_CAPTURE, REASON_FULL, REASON_LINE,
  State, captureCounts, coords, newState, neighbor, other, play, winningLine, winningMoves,
} from "../game/rules";

const KEY = "basalt.preview.profile";
const LEVELS = ["ember", "magma", "caldera"] as const;

function fresh(): Profile {
  const ds = () => ({ wins: 0, losses: 0, draws: 0, streak: 0, bestStreak: 0 });
  return {
    stats: { ember: ds(), magma: ds(), caldera: ds(), streak: 0, bestStreak: 0, hotseatGames: 0, stonesPushed: 0, lan: { wins: 0, losses: 0, draws: 0 } },
    achievements: [], puzzles: [], saved: null,
    settings: { language: "", volume: 0.7, haptics: true, confirmTouch: true, showPreview: true, faceToFace: false, difficulty: "ember", color: "lava" },
  };
}

function load(): Profile {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) {
      const p = JSON.parse(raw) as Profile;
      return { ...fresh(), ...p, settings: { ...fresh().settings, ...p.settings }, stats: { ...fresh().stats, ...p.stats } };
    }
  } catch {
    /* storage blocked or corrupt: start fresh */
  }
  return fresh();
}

function store(p: Profile) {
  try {
    localStorage.setItem(KEY, JSON.stringify(p));
  } catch {
    /* ignore */
  }
}

const err = (code: string) => new BackendError(code, code);
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---------------------------------------------------------------- puzzles

function mulberry(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function hash(s: string) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

function forcesWin(s: State, m: number): boolean {
  const p = s.toMove;
  const s1 = play(s, m).state;
  if (s1.winner === p) return true;
  if (s1.winner !== NO_WINNER) return false;
  for (let r = 0; r < NUM_CELLS; r++) {
    if (s1.cells[r] !== EMPTY) continue;
    const s2 = play(s1, r).state;
    if (s2.winner === p) continue;
    if (s2.winner !== NO_WINNER || winningMoves(s2).length === 0) return false;
  }
  return true;
}

const puzzleCache = new Map<string, { state: State; solution: number }>();

function puzzleFor(key: string): { state: State; solution: number } {
  const hit = puzzleCache.get(key);
  if (hit) return hit;
  const rnd = mulberry(hash(`basalt-preview:${key}`));
  for (let attempt = 0; attempt < 3000; attempt++) {
    let s = newState();
    const target = 14 + Math.floor(rnd() * 14);
    let ok = true;
    while (s.ply < target) {
      const cand: number[] = [];
      for (let m = 0; m < NUM_CELLS; m++) {
        if (s.cells[m] !== EMPTY) continue;
        const n = play(s, m).state;
        if (n.winner === NO_WINNER && winningMoves(n).length === 0) cand.push(m);
      }
      if (!cand.length) {
        ok = false;
        break;
      }
      s = play(s, cand[Math.floor(rnd() * cand.length)]).state;
    }
    if (!ok || winningMoves(s).length) continue;
    const sols: number[] = [];
    for (let m = 0; m < NUM_CELLS && sols.length < 2; m++) if (s.cells[m] === EMPTY && forcesWin(s, m)) sols.push(m);
    if (sols.length === 1) {
      const pz = { state: s, solution: sols[0] };
      puzzleCache.set(key, pz);
      return pz;
    }
  }
  throw err("no_puzzle");
}

function defend(s: State): number {
  let best = -1;
  let bestScore = Infinity;
  for (let r = 0; r < NUM_CELLS; r++) {
    if (s.cells[r] !== EMPTY) continue;
    const n = play(s, r).state;
    if (n.winner === s.toMove) return r;
    const score = n.winner !== NO_WINNER ? 1000 : winningMoves(n).length;
    if (score < bestScore) {
      bestScore = score;
      best = r;
    }
  }
  return best;
}

// ---------------------------------------------------------------- greedy AI

/** One-ply greedy opponent: win, block, sink, then crowd the centre. */
function greedy(s: State, level: string, rnd: () => number): number {
  const wins = winningMoves(s);
  if (wins.length && (level !== "ember" || rnd() > 0.3)) return wins[0];
  const me = s.toMove;
  const foe = other(me);
  let best = -1;
  let bestScore = -Infinity;
  for (let m = 0; m < NUM_CELLS; m++) {
    if (s.cells[m] !== EMPTY) continue;
    const n = play(s, m).state;
    let score = 0;
    if (n.winner === foe) score -= 1000;
    const theirWins = winningMoves(n);
    if (theirWins.length && (level !== "ember" || rnd() > 0.4)) score -= 400 * theirWins.length;
    score += (n.captured[me - 1] - s.captured[me - 1]) * 40;
    const cnt = captureCounts(n, foe);
    score -= Math.max(...cnt) * 25;
    for (const d of neighbor[m]) if (d >= 0 && n.cells[d] === me) score += 4;
    const { q, r } = coords[m];
    score -= (Math.abs(q) + Math.abs(r) + Math.abs(q + r)) * 0.4;
    score += rnd() * (level === "ember" ? 60 : level === "magma" ? 12 : 3);
    if (score > bestScore) {
      bestScore = score;
      best = m;
    }
  }
  return best;
}

// ---------------------------------------------------------------- sessions

interface Session {
  id: number;
  mode: "ai" | "hotseat" | "puzzle";
  difficulty: string;
  human: number;
  puzzleKey: string;
  states: State[];
  history: GameView["history"];
  undos: number;
  hints: number;
  resigned: number;
  outcome: GameView["outcome"];
  maxCapture: number;
  humanMoved: boolean;
}

export function previewBackend(): Backend {
  let profile = load();
  let sess: Session | null = null;
  let nextId = 1;
  let token = 0;
  const rnd = Math.random;

  const today = () => new Date().toISOString().slice(0, 10);
  const cur = (s: Session) => s.states[s.states.length - 1];
  const winner = (s: Session): [number, string] => {
    if (s.resigned) return [s.resigned === LAVA ? ASH : LAVA, "resign"];
    const st = cur(s);
    const reason = st.reason === REASON_LINE ? "line" : st.reason === REASON_CAPTURE ? "capture" : st.reason === REASON_FULL ? "full" : "";
    return [st.winner, reason];
  };
  const over = (s: Session) => winner(s)[0] !== NO_WINNER;
  const humanMoves = (s: Session) => s.history.filter((h) => h.player === s.human).length;
  const failed = (s: Session) => s.mode === "puzzle" && winner(s)[0] !== s.human && (over(s) || humanMoves(s) >= 2);
  const aiTurn = (s: Session) => !over(s) && !failed(s) && s.mode !== "hotseat" && cur(s).toMove !== s.human;
  const forfeit = (s: Session) => s.mode === "ai" && !over(s) && s.humanMoved;
  const lastHuman = (s: Session) => {
    for (let i = s.history.length - 1; i >= 0; i--) if (s.history[i].player === s.human) return i;
    return -1;
  };
  const canUndo = (s: Session) =>
    !over(s) && !failed(s) && (s.mode === "ai" ? lastHuman(s) >= 0 : s.mode === "hotseat" ? s.history.length > 0 : false);

  const view = (s: Session): GameView => {
    const st = cur(s);
    const [w, reason] = winner(s);
    return {
      id: s.id, mode: s.mode, difficulty: s.difficulty, human: s.human, cells: st.cells.slice(), toMove: st.toMove,
      captured: [st.captured[0], st.captured[1]], ply: s.history.length, winner: w, reason,
      winLine: reason === "line" && w !== DRAW ? (winningLine(st, w) ?? []) : [],
      canUndo: canUndo(s), aiTurn: aiTurn(s), history: s.history.slice(), captureGoal: CAPTURE_GOAL, lineLength: LINE_LENGTH,
      puzzleKey: s.puzzleKey, puzzleMoves: s.mode === "puzzle" ? humanMoves(s) : 0, failed: failed(s), outcome: s.outcome,
      undos: s.undos, hints: s.hints, saveFailed: false, forfeit: forfeit(s), remoteTurn: false,
    };
  };

  const persist = (s: Session | null) => {
    profile.saved = s && !over(s) && !failed(s)
      ? { mode: s.mode, difficulty: s.difficulty, human: s.human, moves: s.history.map((h) => h.cell) }
      : null;
    store(profile);
  };

  const record = (s: Session) => {
    if (s.outcome) return;
    const [w] = winner(s);
    const out: NonNullable<GameView["outcome"]> = { result: "draw", newAchievements: [], streak: 0, bestStreak: 0 };
    const grant = (id: string) => {
      if (!profile.achievements.includes(id)) {
        profile.achievements.push(id);
        out.newAchievements.push(id);
      }
    };
    const st = profile.stats;
    if (s.mode === "ai") {
      const ds = st[s.difficulty as (typeof LEVELS)[number]];
      st.stonesPushed += cur(s).captured[s.human - 1];
      if (w === s.human) {
        out.result = "win";
        ds.wins++;
        ds.streak++;
        ds.bestStreak = Math.max(ds.bestStreak, ds.streak);
        st.streak++;
        st.bestStreak = Math.max(st.bestStreak, st.streak);
        grant("first_win");
        if (s.difficulty === "magma") grant("magma");
        if (s.difficulty === "caldera") {
          grant("caldera");
          if (!s.undos && !s.hints) grant("pure_caldera");
        }
        if (st.streak >= 3) grant("streak3");
        if (cur(s).captured[(s.human === LAVA ? ASH : LAVA) - 1] === 0) grant("unbroken");
      } else if (w === DRAW) {
        ds.draws++;
        ds.streak = 0;
        st.streak = 0;
      } else {
        out.result = "loss";
        ds.losses++;
        ds.streak = 0;
        st.streak = 0;
      }
      if (s.maxCapture >= 2) grant("avalanche");
      out.streak = st.streak;
      out.bestStreak = st.bestStreak;
    } else if (s.mode === "hotseat") {
      out.result = w === LAVA || w === ASH ? "win" : "draw";
      st.hotseatGames++;
      grant("hotseat");
    } else {
      out.result = w === s.human ? "win" : "loss";
      if (w === s.human) {
        if (!profile.puzzles.includes(s.puzzleKey)) profile.puzzles.push(s.puzzleKey);
        grant("puzzle");
      }
    }
    s.outcome = out;
    if (sess === s) profile.saved = null;
    store(profile);
  };

  const apply = (s: Session, m: number): MoveResult => {
    const st = cur(s);
    if (st.cells[m] !== EMPTY || m < 0 || m >= NUM_CELLS) throw err("illegal_move");
    const { state, events } = play(st, m);
    const fell = events.filter((e) => e.kind === "fall").length;
    const rec = { ply: st.ply + 1, player: st.toMove, cell: m, pushed: events.filter((e) => e.kind === "push").length, fell };
    if (s.mode !== "hotseat" && st.toMove === s.human) {
      s.maxCapture = Math.max(s.maxCapture, fell);
      s.humanMoved = true;
    }
    s.states.push(state);
    s.history.push(rec);
    if (over(s) || failed(s)) record(s);
    else persist(s);
    return { game: view(s), move: m, player: st.toMove, events };
  };

  const live = (id: number) => {
    if (!sess) throw err("no_game");
    if (sess.id !== id) throw err("stale_game");
    return sess;
  };

  const pending = <T,>(fn: (my: number) => Promise<T>): Pending<T> => {
    const my = ++token;
    return { promise: fn(my), cancel: () => { if (token === my) token++; } };
  };

  return {
    native: false,
    lan: null, // the browser preview has no network play
    getProfile: async () => structuredClone(profile),
    saveSettings: async (s: Settings) => {
      profile.settings = { ...s };
      store(profile);
      return structuredClone(profile);
    },
    resetProfile: async () => {
      token++;
      profile = { ...fresh(), settings: profile.settings };
      sess = null;
      store(profile);
      return structuredClone(profile);
    },
    newGame: async (o: NewGameOptions) => {
      token++;
      let human = 0;
      let start = newState();
      let puzzleKey = "";
      if (o.mode === "ai") {
        if (!LEVELS.includes(o.difficulty as never)) throw err("bad_option");
        human = o.color === "lava" ? LAVA : o.color === "ash" ? ASH : o.color === "random" ? (rnd() < 0.5 ? LAVA : ASH) : 0;
        if (!human) throw err("bad_option");
      } else if (o.mode === "puzzle") {
        puzzleKey = today();
        const pz = puzzleFor(puzzleKey);
        start = { ...pz.state, cells: pz.state.cells.slice(), captured: [...pz.state.captured] as [number, number] };
        human = start.toMove;
      } else if (o.mode !== "hotseat") throw err("bad_option");
      if (sess && forfeit(sess)) {
        sess.resigned = sess.human;
        record(sess);
      }
      sess = {
        id: nextId++, mode: o.mode as Session["mode"], difficulty: o.mode === "ai" ? o.difficulty : "", human, puzzleKey,
        states: [start], history: [], undos: 0, hints: 0, resigned: 0, outcome: null, maxCapture: 0, humanMoved: false,
      };
      if (o.mode === "ai") profile.settings = { ...profile.settings, difficulty: o.difficulty, color: o.color };
      persist(sess);
      return view(sess);
    },
    currentGame: async () => {
      if (!sess && profile.saved) {
        // Rebuild the saved game by replaying its moves.
        const sv = profile.saved;
        let start = newState();
        let puzzleKey = "";
        if (sv.mode === "puzzle") {
          puzzleKey = today();
          try {
            start = puzzleFor(puzzleKey).state;
          } catch {
            return null;
          }
        }
        const s: Session = {
          id: nextId++, mode: sv.mode as Session["mode"], difficulty: sv.difficulty, human: sv.mode === "puzzle" ? start.toMove : sv.human,
          puzzleKey, states: [start], history: [], undos: 0, hints: 0, resigned: 0, outcome: null, maxCapture: 0, humanMoved: false,
        };
        try {
          for (const m of sv.moves) apply(s, m);
          sess = s;
        } catch {
          profile.saved = null;
          store(profile);
        }
      }
      return sess ? view(sess) : null;
    },
    play: async (id, cell) => {
      const s = live(id);
      if (aiTurn(s)) throw err("not_your_turn");
      if (over(s) || failed(s)) throw err("game_over");
      token++;
      return apply(s, cell);
    },
    aiMove: (id) =>
      pending(async (my) => {
        const s = live(id);
        if (!aiTurn(s)) throw err("not_your_turn");
        const ply = s.history.length;
        await sleep(s.mode === "puzzle" ? 300 : s.difficulty === "caldera" ? 900 : 550);
        if (token !== my || sess !== s || s.history.length !== ply) throw err("cancelled");
        if (over(s) || failed(s)) throw err("game_over");
        const st = cur(s);
        const m = s.mode === "puzzle" ? defend(st) : greedy(st, s.difficulty, rnd);
        return apply(s, m);
      }),
    hint: (id) =>
      pending(async (my): Promise<Hint> => {
        const s = live(id);
        if (aiTurn(s) || over(s) || failed(s)) throw err("not_your_turn");
        const ply = s.history.length;
        await sleep(350);
        if (token !== my || sess !== s || s.history.length !== ply) throw err("cancelled");
        s.hints++;
        const st = cur(s);
        if (s.mode === "puzzle" && humanMoves(s) === 0) return { cell: puzzleFor(s.puzzleKey).solution, winRate: 1 };
        return { cell: greedy(st, "caldera", () => 0.5), winRate: 0.5 };
      }),
    cancelAI: () => {
      token++;
    },
    undo: async (id) => {
      const s = live(id);
      if (!canUndo(s)) throw err("cannot_undo");
      token++;
      const n = s.mode === "ai" ? s.history.length - lastHuman(s) : 1;
      s.states.length -= n;
      s.history.length -= n;
      s.undos++;
      s.maxCapture = Math.max(0, ...s.history.filter((h) => h.player === s.human).map((h) => h.fell));
      persist(s);
      return view(s);
    },
    resign: async (id) => {
      const s = live(id);
      if (over(s) || failed(s)) throw err("game_over");
      token++;
      s.resigned = s.mode === "hotseat" ? (cur(s).toMove as Player) : s.human;
      record(s);
      return view(s);
    },
    dailyPuzzle: async (): Promise<DailyPuzzle> => {
      const key = today();
      puzzleFor(key);
      const solved = profile.puzzles.includes(key);
      let streak = 0;
      const d = new Date(`${key}T00:00:00Z`);
      if (!solved) d.setUTCDate(d.getUTCDate() - 1);
      while (profile.puzzles.includes(d.toISOString().slice(0, 10))) {
        streak++;
        d.setUTCDate(d.getUTCDate() - 1);
      }
      return { key, solved, streak, total: profile.puzzles.length };
    },
    haptic: () => {},
  };
}
