import { useMemo } from "react";
import type { DailyPuzzle, Difficulty, GameView, MoveRecord, Profile, Settings } from "../api/backend";
import { I18n, LANGS, Key, useI18n } from "../i18n";
import { ASH, CAPTURE_GOAL, GAP, LAVA, State, cellName, coords, indexOf, newState } from "../game/rules";
import { Board } from "./Board";
import { Choice, ConfirmButton, Dialog, StoneIcon, Toggle } from "./common";

const LEVELS: Difficulty[] = ["ember", "magma", "caldera"];
export const ACHIEVEMENTS = ["first_win", "magma", "caldera", "pure_caldera", "streak3", "avalanche", "unbroken", "hotseat", "puzzle"];

// ---------------------------------------------------------------- setup

interface SetupProps {
  difficulty: Difficulty;
  color: string;
  forfeitLevel: string | null;
  onChange: (difficulty: Difficulty, color: string) => void;
  onStart: () => void;
  onBack: () => void;
}

export function SetupDialog({ difficulty, color, forfeitLevel, onChange, onStart, onBack }: SetupProps) {
  const { t } = useI18n();
  return (
    <Dialog title={t("setup.title")} className="setup">
      <Choice
        label={t("setup.opponent")}
        value={difficulty}
        className="levels"
        options={LEVELS.map((l) => ({ id: l, label: t(`level.${l}` as Key), sub: t(`level.${l}.desc` as Key) }))}
        onChange={(d) => onChange(d, color)}
      />
      <Choice
        label={t("setup.side")}
        value={color as "lava" | "ash" | "random"}
        className="sides"
        options={[
          { id: "lava", label: t("player.lava"), sub: t("setup.lavaSub") },
          { id: "ash", label: t("player.ash"), sub: t("setup.ashSub") },
          { id: "random", label: t("setup.random"), sub: t("setup.randomSub") },
        ]}
        onChange={(c) => onChange(difficulty, c)}
      />
      {forfeitLevel && <p className="note warn">{t("setup.forfeit", { level: t(`level.${forfeitLevel}` as Key) })}</p>}
      <div className="panel-actions">
        <button type="button" className="btn" onClick={onBack}>
          {t("common.back")}
        </button>
        <button type="button" className="btn primary" onClick={onStart}>
          {t("setup.start")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- pause

interface PauseProps {
  canResign: boolean;
  onResume: () => void;
  onHowTo: () => void;
  onSettings: () => void;
  onResign: () => void;
  onMenu: () => void;
}

export function PauseDialog(p: PauseProps) {
  const { t } = useI18n();
  return (
    <Dialog title={t("pause.title")} className="pause">
      <div className="stack">
        <button type="button" className="btn primary" onClick={p.onResume}>
          {t("pause.resume")}
        </button>
        <button type="button" className="btn" onClick={p.onHowTo}>
          {t("title.howto")}
        </button>
        <button type="button" className="btn" onClick={p.onSettings}>
          {t("title.settings")}
        </button>
        {p.canResign && <ConfirmButton label={t("pause.resign")} confirmLabel={t("pause.resignConfirm")} onConfirm={p.onResign} />}
        <button type="button" className="btn" onClick={p.onMenu}>
          <span>{t("pause.menu")}</span>
          <span className="btn-sub">{t("pause.menuSub")}</span>
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- settings

interface SettingsProps {
  settings: Settings;
  native: boolean;
  touch: boolean;
  onChange: (s: Settings) => void;
  onReset: () => void;
  onFullscreen: () => void;
  onBack: () => void;
}

export function SettingsDialog({ settings, native, touch, onChange, onReset, onFullscreen, onBack }: SettingsProps) {
  const { t, num } = useI18n();
  const set = (patch: Partial<Settings>) => onChange({ ...settings, ...patch });
  const lang = (settings.language || "device") as "device" | "en" | "ru" | "uz";
  return (
    <Dialog title={t("settings.title")} className="settings wide">
      <Choice
        label={t("settings.language")}
        value={lang}
        className="langs"
        options={[{ id: "device" as const, label: t("settings.device") }, ...LANGS.map((l) => ({ id: l.id, label: l.label }))]}
        onChange={(v) => set({ language: v === "device" ? "" : v })}
      />
      <label className="row slider">
        <span className="row-text">
          <span className="row-label">{t("settings.volume")}</span>
        </span>
        <input
          type="range"
          min={0}
          max={100}
          step={10}
          value={Math.round(settings.volume * 100)}
          aria-valuetext={`${num(Math.round(settings.volume * 100))}%`}
          onChange={(e) => set({ volume: Number(e.target.value) / 100 })}
        />
      </label>
      <Toggle label={t("settings.preview")} checked={settings.showPreview} onChange={(v) => set({ showPreview: v })} />
      {touch && (
        <Toggle label={t("settings.confirmTouch")} sub={t("settings.confirmTouchSub")} checked={settings.confirmTouch} onChange={(v) => set({ confirmTouch: v })} />
      )}
      {touch && <Toggle label={t("settings.faceToFace")} sub={t("settings.faceToFaceSub")} checked={settings.faceToFace} onChange={(v) => set({ faceToFace: v })} />}
      {native && touch && <Toggle label={t("settings.haptics")} checked={settings.haptics} onChange={(v) => set({ haptics: v })} />}
      <div className="panel-actions spread">
        {!touch && (
          <button type="button" className="btn" onClick={onFullscreen}>
            {t("settings.fullscreen")}
          </button>
        )}
        <ConfirmButton label={t("settings.reset")} confirmLabel={t("settings.resetConfirm")} onConfirm={onReset} />
      </div>
      <div className="panel-actions">
        <button type="button" className="btn primary" onClick={onBack}>
          {t("common.close")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- profile

export function ProfileDialog({ profile, daily, onBack }: { profile: Profile; daily: DailyPuzzle | null; onBack: () => void }) {
  const { t, num } = useI18n();
  const st = profile.stats;
  return (
    <Dialog title={t("profile.title")} className="profile wide">
      <table className="stats-table">
        <thead>
          <tr>
            <th scope="col">{t("profile.level")}</th>
            <th scope="col">{t("profile.wins")}</th>
            <th scope="col">{t("profile.losses")}</th>
            <th scope="col">{t("profile.draws")}</th>
            <th scope="col">{t("profile.bestStreak")}</th>
          </tr>
        </thead>
        <tbody>
          {LEVELS.map((l) => (
            <tr key={l}>
              <th scope="row">{t(`level.${l}` as Key)}</th>
              <td>{num(st[l].wins)}</td>
              <td>{num(st[l].losses)}</td>
              <td>{num(st[l].draws)}</td>
              <td>{num(st[l].bestStreak)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <dl className="facts">
        <div>
          <dt>{t("profile.streak")}</dt>
          <dd>{num(st.streak)}</dd>
        </div>
        <div>
          <dt>{t("profile.overallBest")}</dt>
          <dd>{num(st.bestStreak)}</dd>
        </div>
        <div>
          <dt>{t("profile.pushed")}</dt>
          <dd>{num(st.stonesPushed)}</dd>
        </div>
        <div>
          <dt>{t("profile.hotseat")}</dt>
          <dd>{num(st.hotseatGames)}</dd>
        </div>
        <div>
          <dt>{t("profile.puzzles")}</dt>
          <dd>{num(profile.puzzles.length)}</dd>
        </div>
        <div>
          <dt>{t("profile.puzzleStreak")}</dt>
          <dd>{num(daily?.streak ?? 0)}</dd>
        </div>
      </dl>
      <h3 className="section-title">{t("profile.achievements")}</h3>
      <ul className="achievements">
        {ACHIEVEMENTS.map((id) => {
          const got = profile.achievements.includes(id);
          return (
            <li key={id} className={got ? "got" : "locked"}>
              <span className="ach-mark" aria-hidden="true" />
              <span className="ach-text">
                <span className="ach-name">{t(`ach.${id}` as Key)}</span>
                <span className="ach-desc">
                  {t(`ach.${id}.desc` as Key)}
                  {!got && <span className="visually-hidden"> ({t("profile.locked")})</span>}
                </span>
              </span>
            </li>
          );
        })}
      </ul>
      <div className="panel-actions">
        <button type="button" className="btn primary" onClick={onBack}>
          {t("common.close")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- how to play

function around(q0: number, r0: number, rad: number): number[] {
  const out: number[] = [];
  coords.forEach(({ q, r }, i) => {
    const dq = q - q0;
    const dr = r - r0;
    if (Math.max(Math.abs(dq), Math.abs(dr), Math.abs(dq + dr)) <= rad) out.push(i);
  });
  return out;
}

function diagram(stones: [number, number, number][], toMove: number, gaps: [number, number][] = []): State {
  const s = newState();
  s.toMove = toMove as 1 | 2;
  for (const [q, r, v] of stones) s.cells[indexOf(q, r)] = v;
  for (const [q, r] of gaps) s.cells[indexOf(q, r)] = GAP;
  return s;
}

export function HowToDialog({ onBack }: { onBack: () => void }) {
  const { t, num } = useI18n();
  const d = useMemo(
    () => ({
      push: {
        state: diagram([[1, 0, ASH], [0, 1, ASH], [-1, 0, ASH], [-2, 0, LAVA], [1, -1, LAVA]], LAVA),
        cell: indexOf(0, 0),
        view: around(0, 0, 2),
      },
      sink: {
        state: diagram([[4, -2, ASH], [3, -1, ASH], [2, -1, LAVA]], LAVA, [[3, 0]]),
        cell: indexOf(3, -2),
        view: around(3, -2, 1).concat(around(3, -1, 1)),
      },
      line: {
        state: diagram([[-2, 0, LAVA], [-1, 0, LAVA], [0, 0, LAVA], [1, 0, LAVA], [0, -1, ASH], [-1, 1, ASH]], LAVA),
        cell: indexOf(2, 0),
        view: around(0, 0, 3).filter((c) => Math.abs(coords[c].r) <= 1),
      },
    }),
    [],
  );
  const fig = (key: "push" | "sink" | "line", caption: Key) => (
    <figure className="diagram">
      <div className="diagram-board">
        <Board state={d[key].state} events={[]} moveKey={0} previewCell={d[key].cell} viewCells={d[key].view} label={t(caption)} />
      </div>
      <figcaption>{t(caption)}</figcaption>
    </figure>
  );
  return (
    <Dialog title={t("howto.title")} className="howto wide">
      <div className="howto-body">
        <section>
          <p>{t("howto.place")}</p>
          <p>{t("howto.push")}</p>
          {fig("push", "howto.diagramPush")}
          <p>{t("howto.sink")}</p>
          {fig("sink", "howto.diagramSink")}
          <p>{t("howto.win", { n: num(CAPTURE_GOAL) })}</p>
          {fig("line", "howto.diagramLine")}
          <p>{t("howto.fine")}</p>
        </section>
        <section>
          <h3 className="section-title">{t("howto.controls")}</h3>
          <div className="controls">
            {(
              [
                ["howto.mouse", [["howto.aim", t("howto.mouseAim")], ["howto.placeStone", t("howto.mousePlace")], ["howto.undoMove", t("howto.button")], ["howto.getHint", t("howto.button")], ["howto.openMenu", t("howto.button")]]],
                ["howto.touch", [["howto.aim", t("howto.touchAim")], ["howto.placeStone", t("howto.touchPlace")], ["howto.undoMove", t("howto.button")], ["howto.getHint", t("howto.button")], ["howto.openMenu", t("howto.button")]]],
                ["howto.keyboard", [["howto.aim", "← ↑ → ↓ / WASD"], ["howto.placeStone", "Enter / Space"], ["howto.undoMove", "U"], ["howto.getHint", "H"], ["howto.openMenu", "Esc"]]],
                ["howto.gamepad", [["howto.aim", "✚ / L"], ["howto.placeStone", "A"], ["howto.undoMove", "X"], ["howto.getHint", "Y"], ["howto.openMenu", "B"]]],
              ] as [Key, [Key, string][]][]
            ).map(([device, rows]) => (
              <div key={device} className="control-group">
                <h4>{t(device)}</h4>
                <dl>
                  {rows.map(([action, how]) => (
                    <div key={action}>
                      <dt>{t(action)}</dt>
                      <dd>{device === "howto.keyboard" || device === "howto.gamepad" ? <kbd>{how}</kbd> : how}</dd>
                    </div>
                  ))}
                </dl>
              </div>
            ))}
          </div>
        </section>
      </div>
      <div className="panel-actions">
        <button type="button" className="btn primary" onClick={onBack}>
          {t("common.close")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- history

export function historyLine(i18n: I18n, m: MoveRecord) {
  const parts = [cellName(m.cell)];
  if (m.pushed) parts.push(i18n.tn("history.pushed", m.pushed));
  if (m.fell) parts.push(i18n.tn("history.fell", m.fell));
  return parts.join(", ");
}

export function HistoryList({ history }: { history: MoveRecord[] }) {
  const i18n = useI18n();
  if (!history.length) return <p className="muted">{i18n.t("game.noMoves")}</p>;
  return (
    <ol className="history">
      {history.map((m) => (
        <li key={m.ply} className={m.player === LAVA ? "lava" : "ash"}>
          <span className="ply">{i18n.num(m.ply)}</span>
          <StoneIcon color={m.player === LAVA ? "lava" : "ash"} size={14} />
          <span className="visually-hidden">{i18n.t(m.player === LAVA ? "player.lava" : "player.ash")}</span>
          <span className="move-text">{historyLine(i18n, m)}</span>
        </li>
      ))}
    </ol>
  );
}

export function HistoryDialog({ history, onBack }: { history: MoveRecord[]; onBack: () => void }) {
  const { t } = useI18n();
  return (
    <Dialog title={t("history.title")} className="history-dialog">
      <div className="history-scroll">
        <HistoryList history={history} />
      </div>
      <div className="panel-actions">
        <button type="button" className="btn primary" onClick={onBack}>
          {t("common.close")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- result

interface ResultProps {
  game: GameView;
  onAgain: () => void;
  onReview: () => void;
  onMenu: () => void;
}

export function resultTitle(i18n: I18n, g: GameView): string {
  const { t } = i18n;
  if (g.mode === "puzzle") return g.winner === g.human ? t("result.solved") : t("result.failed");
  if (g.mode === "hotseat") {
    if (g.winner === LAVA) return t("result.lavaWins");
    if (g.winner === ASH) return t("result.ashWins");
    return t("result.draw");
  }
  if (g.winner === g.human) return t("result.win");
  if (g.winner === LAVA || g.winner === ASH) return t("result.loss");
  return t("result.draw");
}

export function resultReason(i18n: I18n, g: GameView): string {
  const { t } = i18n;
  if (g.mode === "puzzle" && g.winner !== g.human) return t("result.reason.puzzleFailed");
  switch (g.reason) {
    case "line":
      return t("result.reason.line");
    case "capture":
      return t("result.reason.capture");
    case "full":
      return g.winner === LAVA || g.winner === ASH ? t("result.reason.full") : t("result.reason.fullDraw");
    case "resign":
      return g.winner === LAVA ? t("result.reason.resignAsh") : t("result.reason.resignLava");
  }
  return "";
}

export function ResultDialog({ game, onAgain, onReview, onMenu }: ResultProps) {
  const i18n = useI18n();
  const { t, tn } = i18n;
  const o = game.outcome;
  const tone = game.mode === "hotseat" ? "neutral" : o?.result ?? "neutral";
  return (
    <Dialog title={resultTitle(i18n, game)} className={`result tone-${tone}`} initialFocus=".btn.primary">
      <p className="result-reason">{resultReason(i18n, game)}</p>
      {o && game.mode === "ai" && o.result === "win" && o.streak > 1 && <p className="result-streak">{tn("result.streak", o.streak)}</p>}
      {o && o.newAchievements.length > 0 && (
        <ul className="unlocked" aria-label={t("result.unlocked")}>
          {o.newAchievements.map((id) => (
            <li key={id}>
              <span className="ach-mark" aria-hidden="true" />
              <span>
                <span className="ach-kicker">{t("result.unlocked")}</span>
                <span className="ach-name">{t(`ach.${id}` as Key)}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {game.saveFailed && <p className="note warn">{t("result.saveFailed")}</p>}
      <div className="stack">
        <button type="button" className="btn primary" onClick={onAgain}>
          {game.mode === "puzzle" && game.winner !== game.human ? t("result.retry") : t("result.again")}
        </button>
        <button type="button" className="btn" onClick={onReview}>
          {t("game.review")}
        </button>
        <button type="button" className="btn" onClick={onMenu}>
          {t("pause.menu")}
        </button>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------- forfeit

export function ForfeitDialog({ level, onStart, onBack }: { level: string; onStart: () => void; onBack: () => void }) {
  const { t } = useI18n();
  return (
    <Dialog title={t("forfeit.title")} className="forfeit">
      <p className="note warn">{t("forfeit.text", { level: t(`level.${level}` as Key) })}</p>
      <div className="panel-actions">
        <button type="button" className="btn" onClick={onBack}>
          {t("common.back")}
        </button>
        <button type="button" className="btn primary" onClick={onStart}>
          {t("forfeit.start")}
        </button>
      </div>
    </Dialog>
  );
}
