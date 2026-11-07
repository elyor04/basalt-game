// TypeScript port of internal/engine/rules.go. The Go engine is the source
// of truth; this copy powers the hover preview (which stones a placement
// would push) and the browser-only preview backend. Both implementations
// are checked against the same vectors (scripts/check-vectors.ts).

export const RADIUS = 4;
export const NUM_CELLS = 3 * RADIUS * RADIUS + 3 * RADIUS + 1;
export const CAPTURE_GOAL = 5;
export const LINE_LENGTH = 5;

export const EMPTY = 0;
export const LAVA = 1;
export const ASH = 2;
export const GAP = 3;

export const NO_WINNER = 0;
export const DRAW = 3;

export const REASON_NONE = 0;
export const REASON_LINE = 1;
export const REASON_CAPTURE = 2;
export const REASON_FULL = 3;

export type Player = 1 | 2;

// Axial directions, clockwise from east; opposite(d) = (d + 3) % 6.
export const DIR_Q = [1, 1, 0, -1, -1, 0];
export const DIR_R = [0, -1, -1, 0, 1, 1];

export interface Coord {
  q: number;
  r: number;
}

export const coords: Coord[] = [];
const indexMap = new Map<string, number>();
for (let r = -RADIUS; r <= RADIUS; r++) {
  for (let q = -RADIUS; q <= RADIUS; q++) {
    if (Math.abs(q) <= RADIUS && Math.abs(r) <= RADIUS && Math.abs(-q - r) <= RADIUS) {
      indexMap.set(`${q},${r}`, coords.length);
      coords.push({ q, r });
    }
  }
}

export function indexOf(q: number, r: number): number {
  return indexMap.get(`${q},${r}`) ?? -1;
}

/** neighbor[c][d] is the cell one step from c in direction d, or -1. */
export const neighbor: number[][] = coords.map(({ q, r }) => DIR_Q.map((dq, d) => indexOf(q + dq, r + DIR_R[d])));

export const lines: number[][] = [];
const linesAt: number[][] = coords.map(() => []);
for (const d of [0, 2, 4]) {
  for (let c = 0; c < NUM_CELLS; c++) {
    const run = [c];
    for (let k = 1; k < LINE_LENGTH; k++) {
      const x = neighbor[run[k - 1]][d];
      if (x < 0) break;
      run.push(x);
    }
    if (run.length === LINE_LENGTH) {
      const idx = lines.length;
      lines.push(run);
      for (const x of run) linesAt[x].push(idx);
    }
  }
}

export interface State {
  cells: number[];
  toMove: Player;
  /** [lava, ash]: enemy stones each side has pushed into the sea. */
  captured: [number, number];
  ply: number;
  winner: number;
  reason: number;
}

export type EventKind = "place" | "push" | "fall" | "blocked" | "collapse";

export interface GameEvent {
  kind: EventKind;
  from: number;
  to: number;
  color: number;
  dir: number;
}

export function newState(): State {
  return { cells: new Array(NUM_CELLS).fill(EMPTY), toMove: LAVA, captured: [0, 0], ply: 0, winner: NO_WINNER, reason: REASON_NONE };
}

export const other = (p: Player): Player => (p === LAVA ? ASH : LAVA);

export function legal(s: State, m: number): boolean {
  return s.winner === NO_WINNER && m >= 0 && m < NUM_CELLS && s.cells[m] === EMPTY;
}

function lineThrough(cells: number[], c: number, p: number): boolean {
  return linesAt[c].some((li) => lines[li].every((x) => cells[x] === p));
}

export function winningLine(s: State, p: number): number[] | null {
  return lines.find((l) => l.every((x) => s.cells[x] === p)) ?? null;
}

/** Play a legal move; returns the new state and the events, like Go's Play. */
export function play(s: State, m: number): { state: State; events: GameEvent[] } {
  if (!legal(s, m)) throw new Error("illegal move");
  const cells = s.cells.slice();
  const captured: [number, number] = [s.captured[0], s.captured[1]];
  const me = s.toMove;
  const foe = other(me);
  const events: GameEvent[] = [{ kind: "place", from: m, to: m, color: me, dir: -1 }];
  cells[m] = me;
  const moved: number[] = [];
  for (let d = 0; d < 6; d++) {
    const n = neighbor[m][d];
    if (n < 0 || cells[n] !== foe) continue;
    const dst = neighbor[n][d];
    if (dst < 0) {
      cells[n] = GAP; // falls off the rim; the column crumbles
      captured[me - 1]++;
      events.push({ kind: "fall", from: n, to: -1, color: foe, dir: d });
      events.push({ kind: "collapse", from: n, to: n, color: 0, dir: d });
    } else if (cells[dst] === GAP) {
      cells[n] = EMPTY;
      captured[me - 1]++;
      events.push({ kind: "fall", from: n, to: dst, color: foe, dir: d });
    } else if (cells[dst] === EMPTY) {
      cells[n] = EMPTY;
      cells[dst] = foe;
      moved.push(dst);
      events.push({ kind: "push", from: n, to: dst, color: foe, dir: d });
    } else {
      events.push({ kind: "blocked", from: n, to: dst, color: foe, dir: d });
    }
  }
  const next: State = { cells, toMove: foe, captured, ply: s.ply + 1, winner: NO_WINNER, reason: REASON_NONE };
  if (captured[me - 1] >= CAPTURE_GOAL) {
    next.winner = me;
    next.reason = REASON_CAPTURE;
  } else if (lineThrough(cells, m, me)) {
    next.winner = me;
    next.reason = REASON_LINE;
  } else if (moved.some((c) => lineThrough(cells, c, foe))) {
    next.winner = foe;
    next.reason = REASON_LINE;
  } else if (!cells.includes(EMPTY)) {
    const a = cells.filter((x) => x === LAVA).length;
    const b = cells.filter((x) => x === ASH).length;
    next.reason = REASON_FULL;
    next.winner = a > b ? LAVA : b > a ? ASH : DRAW;
  }
  return { state: next, events };
}

/** Cells where the player to move would win at once (lines, then captures). */
export function winningMoves(s: State): number[] {
  if (s.winner !== NO_WINNER) return [];
  const p = s.toMove;
  const out: number[] = [];
  for (const l of lines) {
    let own = 0;
    let empty = -1;
    for (const c of l) {
      if (s.cells[c] === p) own++;
      else if (s.cells[c] === EMPTY) empty = c;
    }
    if (own === LINE_LENGTH - 1 && empty >= 0 && !out.includes(empty)) out.push(empty);
  }
  const need = CAPTURE_GOAL - s.captured[p - 1];
  const cnt = captureCounts(s, p);
  for (let c = 0; c < NUM_CELLS; c++) if (cnt[c] >= need && !out.includes(c)) out.push(c);
  return out;
}

/** How many enemy stones a placement by p at each empty cell would sink. */
export function captureCounts(s: State, p: Player): number[] {
  const cnt = new Array(NUM_CELLS).fill(0);
  const foe = other(p);
  for (let c = 0; c < NUM_CELLS; c++) {
    if (s.cells[c] !== foe) continue;
    for (let d = 0; d < 6; d++) {
      const beyond = neighbor[c][d];
      if (beyond >= 0 && s.cells[beyond] !== GAP) continue;
      const from = neighbor[c][(d + 3) % 6];
      if (from >= 0 && s.cells[from] === EMPTY) cnt[from]++;
    }
  }
  return cnt;
}

/** Human-readable coordinate: column letter a–i (q) and row 1–9 (r). */
export function cellName(c: number): string {
  const { q, r } = coords[c];
  return `${String.fromCharCode(97 + q + RADIUS)}${r + RADIUS + 1}`;
}
