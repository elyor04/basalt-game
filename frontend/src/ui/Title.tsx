import { useMemo } from "react";
import type { DailyPuzzle, GameView } from "../api/backend";
import { LANGS, Lang, useI18n } from "../i18n";
import { ASH, LAVA, State, indexOf, newState } from "../game/rules";
import { Board } from "./Board";

interface Props {
  saved: GameView | null;
  daily: DailyPuzzle | null;
  preview: boolean;
  lang: Lang;
  reducedMotion: boolean;
  onContinue: () => void;
  onVsAI: () => void;
  onHotseat: () => void;
  /** Local-network play; absent in the browser preview. */
  onLan?: () => void;
  onDaily: () => void;
  onOpen: (what: "howto" | "profile" | "settings") => void;
  onLanguage: (l: Lang) => void;
}

/** A still life for the title: a few stones mid-duel on the causeway. */
function heroState(): State {
  const s = newState();
  const put = (q: number, r: number, v: number) => {
    s.cells[indexOf(q, r)] = v;
  };
  put(0, 0, LAVA);
  put(1, -1, LAVA);
  put(-1, 1, LAVA);
  put(2, -2, ASH);
  put(1, 0, ASH);
  put(-2, 2, ASH);
  put(0, 1, ASH);
  put(-1, 0, LAVA);
  return s;
}

export function Title(p: Props) {
  const { t } = useI18n();
  const hero = useMemo(heroState, []);
  const continueSub = p.saved
    ? p.saved.mode === "ai"
      ? t("title.continueAI", { level: t(`level.${p.saved.difficulty}` as "level.ember"), n: p.saved.ply + 1 })
      : p.saved.mode === "hotseat"
        ? t("title.continueHotseat", { n: p.saved.ply + 1 })
        : t("title.continuePuzzle")
    : "";
  return (
    <main className="screen title-screen">
      <div className="title-hero" aria-hidden="true">
        <Board state={hero} events={[]} moveKey={0} intro={!p.reducedMotion} label="" />
      </div>
      <div className="title-copy">
        <h1 className="wordmark">{t("app.name")}</h1>
        <p className="tagline">{t("app.tagline")}</p>
        <nav className="title-menu" aria-label={t("app.name")}>
          {p.saved && (
            <button type="button" className="menu-item primary" onClick={p.onContinue}>
              <span className="menu-label">{t("title.continue")}</span>
              <span className="menu-sub">{continueSub}</span>
            </button>
          )}
          <button type="button" className={`menu-item${p.saved ? "" : " primary"}`} onClick={p.onVsAI}>
            <span className="menu-label">{t("title.vsAI")}</span>
          </button>
          <button type="button" className="menu-item" onClick={p.onHotseat}>
            <span className="menu-label">{t("title.hotseat")}</span>
            <span className="menu-sub">{t("title.hotseatSub")}</span>
          </button>
          {p.onLan && (
            <button type="button" className="menu-item" onClick={p.onLan}>
              <span className="menu-label">{t("title.lan")}</span>
              <span className="menu-sub">{t("title.lanSub")}</span>
            </button>
          )}
          {p.daily && (
            <button type="button" className="menu-item" onClick={p.onDaily}>
              <span className="menu-label">{t("title.daily")}</span>
              <span className="menu-sub">{p.daily.solved ? t("title.dailySolved") : t("title.dailySub")}</span>
            </button>
          )}
          <div className="menu-minor">
            <button type="button" className="link-btn" onClick={() => p.onOpen("howto")}>
              {t("title.howto")}
            </button>
            <button type="button" className="link-btn" onClick={() => p.onOpen("profile")}>
              {t("title.profile")}
            </button>
            <button type="button" className="link-btn" onClick={() => p.onOpen("settings")}>
              {t("title.settings")}
            </button>
          </div>
        </nav>
        <div className="lang-switch" role="group" aria-label={t("title.language")}>
          {LANGS.map((l) => (
            <button
              key={l.id}
              type="button"
              lang={l.id === "uz" ? "uz-Latn" : l.id}
              aria-pressed={p.lang === l.id}
              aria-label={l.label}
              className={p.lang === l.id ? "on" : ""}
              onClick={() => p.onLanguage(l.id)}
            >
              {l.short}
            </button>
          ))}
        </div>
        {p.preview && <p className="preview-note">{t("common.previewNote")}</p>}
      </div>
    </main>
  );
}
