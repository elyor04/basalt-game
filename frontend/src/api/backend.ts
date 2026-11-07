// Thin adapter over the generated Wails bindings. Inside the Wails app every
// call goes to Go, which owns the rules, the AI and the profile. Only when the
// page runs without the Wails runtime at all (plain browser: vite dev or a
// devtools session) does a localStorage preview stand in. A failing Go call in
// the native app is an error to show, never a reason to switch to the preview.

import * as Go from "../../bindings/basalt/internal/game/gameservice";
import type * as M from "../../bindings/basalt/internal/game/models";
import type { GameEvent } from "../game/rules";

export type Settings = M.Settings;
export type DiffStats = M.DiffStats;
export type Stats = M.Stats;
export type MoveRecord = M.MoveRecord;
export type Hint = M.Hint;
export type DailyPuzzle = M.DailyPuzzle;
export type NewGameOptions = M.NewGameOptions;

export type Mode = "ai" | "hotseat" | "puzzle" | "lan";
export type Difficulty = "ember" | "magma" | "caldera";

export interface Outcome {
  result: "win" | "loss" | "draw";
  newAchievements: string[];
  streak: number;
  bestStreak: number;
}

export interface SavedGame {
  mode: string;
  difficulty: string;
  human: number;
  moves: number[];
}

export interface Profile {
  stats: Stats;
  achievements: string[];
  puzzles: string[];
  settings: Settings;
  saved: SavedGame | null;
}

export interface GameView {
  id: number;
  mode: Mode;
  difficulty: string;
  human: number;
  cells: number[];
  toMove: number;
  captured: [number, number];
  ply: number;
  winner: number;
  reason: string;
  winLine: number[];
  canUndo: boolean;
  aiTurn: boolean;
  history: MoveRecord[];
  captureGoal: number;
  lineLength: number;
  puzzleKey: string;
  puzzleMoves: number;
  failed: boolean;
  outcome: Outcome | null;
  undos: number;
  hints: number;
  saveFailed: boolean;
  /** Replacing this unfinished AI game would count as a resignation. */
  forfeit: boolean;
  /** A network game waiting for the rival's move. */
  remoteTurn: boolean;
}

export interface MoveResult {
  game: GameView;
  move: number;
  player: number;
  events: GameEvent[];
}

export type NetPhase = "off" | "hosting" | "joining" | "playing" | "lost" | "closed";

/** Everything about a local-network match (Go: game.NetStatus). */
export interface NetStatus {
  version: number;
  phase: NetPhase;
  role: "host" | "guest" | "";
  /** Why a closed match ended: "left", "timeout", "error", "busy" or "version". */
  reason: string;
  code: string;
  altCodes: string[];
  discoverable: boolean;
  wantRematch: boolean;
  peerRematch: boolean;
  game: GameView | null;
  /** The rival's latest move in this game, for animation. */
  remote: MoveResult | null;
}

export interface LANGame {
  code: string;
  name: string;
}

/** Local-network play. Only the native app has it. */
export interface LanApi {
  host(): Promise<NetStatus>;
  /** Games seen on the network in the last few seconds (starts looking). */
  browse(): Promise<LANGame[]>;
  stopBrowse(): void;
  join(code: string): Promise<NetStatus>;
  status(): Promise<NetStatus>;
  /** Resolves when the state is newer than `version` (or after ~20 s). */
  wait(version: number): Pending<NetStatus>;
  rematch(want: boolean): Promise<NetStatus>;
  leave(): Promise<NetStatus>;
}

/** A request that can be abandoned; the Go side stops thinking too. */
export interface Pending<T> {
  promise: Promise<T>;
  cancel: () => void;
}

export interface Backend {
  native: boolean;
  getProfile(): Promise<Profile>;
  saveSettings(s: Settings): Promise<Profile>;
  resetProfile(): Promise<Profile>;
  newGame(o: NewGameOptions): Promise<GameView>;
  /** The game in progress, or null if there is none. */
  currentGame(): Promise<GameView | null>;
  play(id: number, cell: number): Promise<MoveResult>;
  aiMove(id: number): Pending<MoveResult>;
  hint(id: number): Pending<Hint>;
  cancelAI(): void;
  undo(id: number): Promise<GameView>;
  resign(id: number): Promise<GameView>;
  dailyPuzzle(): Promise<DailyPuzzle>;
  haptic(kind: "light" | "medium" | "heavy" | "success" | "warning" | "error" | "selection"): void;
  lan: LanApi | null;
}

/** An error from the game service. `code` matches Go's GameError.Code. */
export class BackendError extends Error {
  code: string;
  constructor(message: string, code = "generic") {
    super(message);
    this.code = code;
  }
}

function safeParse(s: string): unknown {
  try {
    return JSON.parse(s);
  } catch {
    return undefined;
  }
}

/**
 * Wails puts the marshalled Go error in `cause`; keep its code. The Android
 * transport drops `cause` and sends only the text, so fall back to the
 * "[code]" suffix that Go's GameError.Error() appends.
 */
function toBackendError(e: unknown): BackendError {
  if (e instanceof BackendError) return e;
  const cause = (e as { cause?: unknown })?.cause;
  const c = typeof cause === "string" ? safeParse(cause) : cause;
  const msg = e instanceof Error ? e.message : typeof e === "string" ? e : "game service error";
  if ((e as { name?: string })?.name === "CancelError") return new BackendError(msg, "cancelled");
  const code = (c as { code?: unknown } | undefined)?.code;
  const fromText = /\[([a-z_]+)\]\s*$/.exec(msg)?.[1];
  return new BackendError(msg, typeof code === "string" ? code : fromText ?? "generic");
}

const wrap = async <T,>(p: PromiseLike<T>): Promise<T> => {
  try {
    return await p;
  } catch (e) {
    throw toBackendError(e);
  }
};

export function fixProfile(p: M.Profile): Profile {
  return {
    stats: p.stats,
    achievements: p.achievements ?? [],
    puzzles: p.puzzles ?? [],
    settings: p.settings,
    saved: p.saved ? { mode: p.saved.mode, difficulty: p.saved.difficulty, human: p.saved.human, moves: p.saved.moves ?? [] } : null,
  };
}

export function fixGame(v: M.GameView): GameView {
  return {
    ...v,
    mode: v.mode as Mode,
    cells: v.cells ?? [],
    captured: [v.captured?.[0] ?? 0, v.captured?.[1] ?? 0],
    winLine: v.winLine ?? [],
    history: v.history ?? [],
    outcome: v.outcome
      ? { ...v.outcome, result: v.outcome.result as Outcome["result"], newAchievements: v.outcome.newAchievements ?? [] }
      : null,
  };
}

function fixMove(r: M.MoveResult): MoveResult {
  return { game: fixGame(r.game), move: r.move, player: r.player, events: (r.events ?? []) as GameEvent[] };
}

export function fixNet(n: M.NetStatus): NetStatus {
  return {
    ...n,
    phase: (n.phase || "off") as NetPhase,
    role: (n.role || "") as NetStatus["role"],
    altCodes: n.altCodes ?? [],
    game: n.game ? fixGame(n.game) : null,
    remote: n.remote ? fixMove(n.remote) : null,
  };
}

const nativeLan: LanApi = {
  host: async () => fixNet(await wrap(Go.HostLAN())),
  browse: async () => ((await wrap(Go.BrowseLAN())) ?? []).map((g) => ({ code: g.code, name: g.name })),
  stopBrowse: () => void Go.StopBrowseLAN().catch(() => {}),
  join: async (code) => fixNet(await wrap(Go.JoinLAN(code))),
  status: async () => fixNet(await wrap(Go.LANStatus())),
  wait: (version) => {
    const call = Go.NetWait(version);
    return { promise: wrap(call).then(fixNet), cancel: () => call.cancel() };
  },
  rematch: async (want) => fixNet(await wrap(Go.RematchLAN(want))),
  leave: async () => fixNet(await wrap(Go.LeaveLAN())),
};

const native: Backend = {
  native: true,
  getProfile: async () => fixProfile(await wrap(Go.GetProfile())),
  saveSettings: async (s) => fixProfile(await wrap(Go.SaveSettings(s))),
  resetProfile: async () => fixProfile(await wrap(Go.ResetProfile())),
  newGame: async (o) => fixGame(await wrap(Go.NewGame(o))),
  currentGame: async () => {
    try {
      return fixGame(await wrap(Go.CurrentGame()));
    } catch (e) {
      if (e instanceof BackendError && e.code === "no_game") return null;
      throw e;
    }
  },
  play: async (id, cell) => fixMove(await wrap(Go.Play(id, cell))),
  aiMove: (id) => {
    const call = Go.AIMove(id);
    return { promise: wrap(call).then(fixMove), cancel: () => call.cancel() };
  },
  hint: (id) => {
    const call = Go.Hint(id);
    return { promise: wrap(call), cancel: () => call.cancel() };
  },
  cancelAI: () => void Go.CancelAI().catch(() => {}),
  undo: async (id) => fixGame(await wrap(Go.Undo(id))),
  resign: async (id) => fixGame(await wrap(Go.Resign(id))),
  dailyPuzzle: () => wrap(Go.GetDailyPuzzle()),
  haptic: (k) => void Go.Haptic(k).catch(() => {}),
  lan: nativeLan,
};

/**
 * True when the page runs inside a Wails webview: WebView2 on Windows,
 * WKWebView/WebKitGTK on macOS, iOS and Linux, or the Android bridge.
 */
export function hasWailsRuntime(): boolean {
  const w = window as unknown as {
    chrome?: { webview?: { postMessage?: unknown } };
    webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } };
    wails?: { invoke?: unknown };
  };
  return Boolean(w.chrome?.webview?.postMessage || w.webkit?.messageHandlers?.external?.postMessage || w.wails?.invoke);
}

let chosen: Backend | null = null;

/** Pick the backend once, by whether the Wails runtime is present. */
export async function connect(): Promise<Backend> {
  if (chosen) return chosen;
  if (hasWailsRuntime()) {
    chosen = native;
  } else {
    const { previewBackend } = await import("./preview");
    chosen = previewBackend();
  }
  return chosen;
}
