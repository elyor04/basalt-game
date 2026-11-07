import type { GameView, NetStatus, Settings } from "../api/backend";
import { Key, useI18n } from "../i18n";
import { ASH, GameEvent, LAVA, NUM_CELLS, State, cellName, legal, play } from "../game/rules";
import { Board, PointerKind } from "./Board";
import { StoneIcon } from "./common";
import { HistoryList, resultTitle } from "./Screens";

export type InputMode = "mouse" | "touch" | "keyboard" | "gamepad";

interface Props {
  game: GameView;
  /** The network match, for local-network games. */
  net: NetStatus | null;
  display: { state: State; events: GameEvent[]; key: number };
  settings: Settings;
  thinking: boolean;
  hintCell: number | null;
  previewCell: number | null;
  cursor: number | null;
  inputMode: InputMode;
  reducedMotion: boolean;
  resultHidden: boolean;
  onHover: (c: number | null) => void;
  onActivate: (c: number, kind: PointerKind) => void;
  onPlace: () => void;
  onUndo: () => void;
  onHint: () => void;
  onMoves: () => void;
  onMenu: () => void;
  onAgain: () => void;
  onShowResult: () => void;
  /** Leave a network game that can no longer go on. */
  onLeave: () => void;
}

/** What a network game's status line says about the connection, if anything. */
export function netNotice(t: ReturnType<typeof useI18n>["t"], net: NetStatus | null, over = false): string | null {
  if (!net) return null;
  if (net.phase === "lost") return t("game.reconnecting");
  if (net.phase === "closed") {
    // A finished game stands; only say why the match ended if it matters.
    if (over && net.reason !== "left") return null;
    const r = ["left", "timeout", "busy", "version"].includes(net.reason) ? net.reason : "error";
    return t(`game.net.${r}` as Key);
  }
  return null;
}

function PlayerPanel({ game, color, flipped }: { game: GameView; color: number; flipped?: boolean }) {
  const { t, num } = useI18n();
  const name =
    game.mode === "hotseat"
      ? t(color === LAVA ? "player.lava" : "player.ash")
      : color === game.human
        ? t("player.you")
        : game.mode === "ai"
          ? t(`level.${game.difficulty}` as Key)
          : game.mode === "lan"
            ? t("player.rival")
            : t(color === LAVA ? "player.lava" : "player.ash");
  const sunk = game.captured[color - 1];
  const active = !game.winner && !game.failed && game.toMove === color;
  const side = color === LAVA ? "lava" : "ash";
  const foe = color === LAVA ? "ash" : "lava";
  const label = t(color === LAVA ? "player.lava" : "player.ash");
  return (
    <div className={`player ${side}${active ? " active" : ""}${flipped ? " flipped" : ""}`}>
      <span className="player-id">
        <StoneIcon color={side} size={22} />
        <span className="player-name">{name}</span>
        {name !== label && <span className="player-side">{label}</span>}
      </span>
      <span className="tray" role="img" aria-label={t("game.sunkLabel", { player: label, n: sunk, goal: game.captureGoal })}>
        {Array.from({ length: game.captureGoal }, (_, i) => (
          <span key={i} className={`slot${i < sunk ? " filled" : ""}`}>
            {i < sunk && <StoneIcon color={foe} size={14} />}
          </span>
        ))}
        <span className="tray-count" aria-hidden="true">
          {t("game.sunk", { n: num(sunk), goal: num(game.captureGoal) })}
        </span>
      </span>
    </div>
  );
}

export function GameScreen(p: Props) {
  const i18n = useI18n();
  const { t } = i18n;
  const g = p.game;
  const over = g.winner !== 0 || g.failed;
  const lan = g.mode === "lan";
  const connected = !lan || p.net?.phase === "playing";
  const humanTurn = !over && !g.aiTurn && !g.remoteTurn && connected;
  const notice = lan ? netNotice(t, p.net, over) : null;
  const topColor = g.mode === "hotseat" ? ASH : g.human === LAVA ? ASH : LAVA;
  const bottomColor = topColor === LAVA ? ASH : LAVA;
  const faceToFace = g.mode === "hotseat" && p.settings.faceToFace;

  let status: string;
  if (over) status = resultTitle(i18n, g);
  else if (lan) status = notice ?? (g.remoteTurn ? t("game.rivalMove") : t("game.yourMove"));
  else if (p.thinking || g.aiTurn) status = g.mode === "puzzle" ? t("game.toMove", { player: t(g.toMove === LAVA ? "player.lava" : "player.ash") }) : t("game.thinking", { level: t(`level.${g.difficulty}` as Key) });
  else if (g.mode === "hotseat") status = t("game.toMove", { player: t(g.toMove === LAVA ? "player.lava" : "player.ash") });
  else status = t("game.yourMove");

  const sub =
    over && notice
      ? notice
      : over && lan && p.net?.peerRematch && !p.net.wantRematch
        ? t("result.rivalWants")
        : g.mode === "puzzle" && !over ? `${t("game.puzzleGoal")}. ${t("game.puzzleMove", { n: Math.min(g.puzzleMoves + 1, 2) })}` : p.hintCell !== null ? t("game.hintFor", { cell: cellName(p.hintCell) }) : "";

  const focusCell = p.previewCell ?? p.cursor;
  const cellDesc =
    focusCell !== null && focusCell >= 0 && focusCell < NUM_CELLS
      ? (() => {
          const v = p.display.state.cells[focusCell];
          const name = cellName(focusCell);
          if (v === LAVA) return t("game.cellLava", { cell: name });
          if (v === ASH) return t("game.cellAsh", { cell: name });
          if (v === 3) return t("game.cellGap", { cell: name });
          if (focusCell === p.previewCell && legal(p.display.state, focusCell)) {
            const ev = play(p.display.state, focusCell).events;
            return t("game.preview", { cell: name, pushed: ev.filter((e) => e.kind === "push").length, sunk: ev.filter((e) => e.kind === "fall").length });
          }
          return t("game.cellEmpty", { cell: name });
        })()
      : "";

  const showPlace = p.inputMode === "touch" && p.settings.confirmTouch && p.previewCell !== null && humanTurn;

  return (
    <main className={`screen game-screen mode-${g.mode}${faceToFace ? " face" : ""}${lan && !connected ? " offline" : ""}`}>
      <header className="hud hud-top">
        <PlayerPanel game={g} color={topColor} flipped={faceToFace} />
        <button type="button" className="icon-btn menu-btn" onClick={p.onMenu} aria-label={t("game.menu")}>
          <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true">
            <path d="M4 7h16M4 12h16M4 17h16" />
          </svg>
        </button>
      </header>

      <div className={`board-wrap${humanTurn ? " your-turn" : ""}`}>
        <Board
          state={p.display.state}
          events={p.display.events}
          moveKey={p.display.key}
          interactive={humanTurn}
          previewCell={humanTurn ? p.previewCell : null}
          showPushes={p.settings.showPreview}
          cursor={humanTurn && (p.inputMode === "keyboard" || p.inputMode === "gamepad") ? p.cursor : null}
          hintCell={p.hintCell}
          winLine={g.winLine}
          reducedMotion={p.reducedMotion}
          label={t("game.boardLabel", { n: NUM_CELLS })}
          onHover={p.onHover}
          onActivate={p.onActivate}
        />
      </div>

      <aside className="side-panel">
        <h2 className="side-title">{t("history.title")}</h2>
        <div className="history-scroll">
          <HistoryList history={g.history} />
        </div>
      </aside>

      <footer className="hud hud-bottom">
        <PlayerPanel game={g} color={bottomColor} />
        <div className="status-block">
          <p className="status" aria-live="polite">
            {status}
          </p>
          <p className="status-sub" aria-live="polite">
            {sub}
          </p>
          <p className="visually-hidden" aria-live="polite">
            {cellDesc}
          </p>
        </div>
        <div className="actions">
          {lan && p.net?.phase === "closed" ? (
            <>
              <button type="button" className="btn primary" onClick={p.onLeave}>
                {t("pause.menu")}
              </button>
              {over && p.resultHidden && (
                <button type="button" className="btn" onClick={p.onShowResult}>
                  {t("game.over")}
                </button>
              )}
            </>
          ) : over ? (
            <>
              <button type="button" className="btn primary" disabled={lan && (p.net?.wantRematch || !connected)} onClick={p.onAgain}>
                {lan && p.net?.wantRematch ? t("result.waitingRival") : g.mode === "puzzle" && g.winner !== g.human ? t("result.retry") : t("result.again")}
              </button>
              {p.resultHidden && (
                <button type="button" className="btn" onClick={p.onShowResult}>
                  {t("game.over")}
                </button>
              )}
            </>
          ) : (
            <>
              {g.mode !== "puzzle" && !lan && (
                <button type="button" className="btn" disabled={!g.canUndo} onClick={p.onUndo}>
                  {t("game.undo")}
                </button>
              )}
              {!lan && (
                <button type="button" className="btn" disabled={!humanTurn || p.thinking} onClick={p.onHint}>
                  {t("game.hint")}
                </button>
              )}
              {showPlace && (
                <button type="button" className="btn primary place-btn" onClick={p.onPlace}>
                  {t("game.place")}
                </button>
              )}
            </>
          )}
          <button type="button" className="btn moves-btn" onClick={p.onMoves}>
            {t("game.moves")}
          </button>
        </div>
        {p.inputMode === "keyboard" && !over && (
          <p className="keys" aria-hidden="true">
            <span><kbd>←↑→↓</kbd> {t("howto.aim")}</span>
            <span><kbd>Enter</kbd> {t("howto.placeStone")}</span>
            {!lan && <span><kbd>U</kbd> {t("howto.undoMove")}</span>}
            {!lan && <span><kbd>H</kbd> {t("howto.getHint")}</span>}
            <span><kbd>Esc</kbd> {t("howto.openMenu")}</span>
          </p>
        )}
      </footer>
    </main>
  );
}
