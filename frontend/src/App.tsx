import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Window } from "@wailsio/runtime";
import { Backend, BackendError, DailyPuzzle, Difficulty, GameView, MoveResult, NewGameOptions, Pending, Profile, Settings, connect } from "./api/backend";
import { AudioEngine } from "./game/audio";
import { DRAW, GameEvent, NUM_CELLS, State, coords, indexOf } from "./game/rules";
import { axialToXY } from "./game/layout";
import { I18nProvider, Lang, errorText, makeI18n, resolveLang } from "./i18n";
import type { PointerKind } from "./ui/Board";
import { GameScreen, InputMode } from "./ui/GameScreen";
import { ForfeitDialog, HistoryDialog, HowToDialog, PauseDialog, ProfileDialog, ResultDialog, SettingsDialog, SetupDialog } from "./ui/Screens";
import { Title } from "./ui/Title";
import { useGamepadMenus, usePad } from "./ui/gamepadNav";

type Overlay = "setup" | "pause" | "settings" | "howto" | "profile" | "history" | "forfeit";
type Display = { state: State; events: GameEvent[]; key: number };

const RESULT_DELAY_MS = 900; // let the final push play out first
const RESULT_ARM_MS = 650; // ignore input this long after the result appears

function toState(g: GameView): State {
  return {
    cells: g.cells.slice(),
    toMove: (g.toMove === 2 ? 2 : 1) as 1 | 2,
    captured: [g.captured[0], g.captured[1]],
    ply: g.ply,
    // A finished or failed game blocks previews and placement.
    winner: g.winner || (g.failed ? DRAW : 0),
    reason: 0,
  };
}

const prefersReduced = () => matchMedia("(prefers-reduced-motion: reduce)").matches;
const coarse = () => matchMedia("(pointer: coarse)").matches;

export default function App() {
  const audio = useRef(new AudioEngine()).current;
  const beRef = useRef<Backend | null>(null);

  const [bootError, setBootError] = useState<string | null>(null);
  const [booting, setBooting] = useState(true);
  const [profile, setProfile] = useState<Profile | null>(null);
  const [daily, setDaily] = useState<DailyPuzzle | null>(null);
  const [screen, setScreen] = useState<"boot" | "title" | "game">("boot");
  const [overlays, setOverlays] = useState<Overlay[]>([]);
  const [game, setGame] = useState<GameView | null>(null);
  const [display, setDisplay] = useState<Display>({ state: toState({ cells: new Array(NUM_CELLS).fill(0), toMove: 1, captured: [0, 0], ply: 0, winner: 0, failed: false } as unknown as GameView), events: [], key: 0 });
  const [thinking, setThinking] = useState(false);
  const [hintCell, setHintCell] = useState<number | null>(null);
  const [previewCell, setPreviewCell] = useState<number | null>(null);
  const [cursor, setCursor] = useState<number>(indexOf(0, 0));
  const [inputMode, setInputMode] = useState<InputMode>(coarse() ? "touch" : "mouse");
  const [resultOpen, setResultOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);
  const [reducedMotion, setReducedMotion] = useState(prefersReduced());
  const [pendingStart, setPendingStart] = useState<NewGameOptions | null>(null);
  const [setup, setSetup] = useState<{ difficulty: Difficulty; color: string }>({ difficulty: "ember", color: "lava" });

  const lang: Lang = resolveLang(profile?.settings.language);
  const i18n = useMemo(() => makeI18n(lang), [lang]);
  const i18nRef = useRef(i18n);
  i18nRef.current = i18n;
  useEffect(() => {
    document.documentElement.lang = lang === "uz" ? "uz-Latn" : lang;
  }, [lang]);

  const aiCall = useRef<Pending<MoveResult> | null>(null);
  // Settings are saved with a short debounce; flush before anything that
  // reads the profile back from Go, so a newer local change is never lost.
  const saveTimer = useRef<number | undefined>(undefined);
  const pendingSettings = useRef<Settings | null>(null);
  const flushSettings = useRef(async () => {
    window.clearTimeout(saveTimer.current);
    const s = pendingSettings.current;
    pendingSettings.current = null;
    if (!s || !beRef.current) return;
    try {
      await beRef.current.saveSettings(s);
    } catch (e) {
      fail(e);
    }
  });
  const hintCall = useRef<Pending<unknown> | null>(null);
  const resultShownAt = useRef(0);
  const resultTimer = useRef<number | undefined>(undefined);
  const displayKey = useRef(0);
  const cursorX = useRef(0);
  const touchSeen = useRef(coarse());

  // Mirrors for long-lived handlers.
  const live = useRef({ screen, overlays, game, thinking, resultOpen, previewCell, cursor, profile, inputMode });
  live.current = { screen, overlays, game, thinking, resultOpen, previewCell, cursor, profile, inputMode };

  const showToast = useCallback((msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast((t) => (t === msg ? null : t)), 3200);
  }, []);
  const fail = useCallback((e: unknown) => {
    if (e instanceof BackendError && e.code === "cancelled") return;
    showToast(errorText(i18nRef.current, e));
  }, [showToast]);

  const haptic = useCallback((kind: Parameters<Backend["haptic"]>[0]) => {
    if (live.current.profile?.settings.haptics) beRef.current?.haptic(kind);
  }, []);

  // ---------------------------------------------------------------- boot

  const boot = useCallback(async () => {
    setBooting(true);
    setBootError(null);
    try {
      const be = await connect();
      const [p, cur] = await Promise.all([be.getProfile(), be.currentGame()]);
      beRef.current = be;
      setProfile(p);
      audio.setVolume(p.settings.volume);
      setSetup({ difficulty: (p.settings.difficulty as Difficulty) || "ember", color: p.settings.color || "lava" });
      setGame(cur && !cur.winner && !cur.failed ? cur : null);
      be.dailyPuzzle().then(setDaily).catch(() => setDaily(null));
      setScreen("title");
    } catch (e) {
      setBootError(errorText(i18nRef.current, e) + (e instanceof Error && e.message ? ` (${e.message})` : ""));
    } finally {
      setBooting(false);
    }
  }, [audio]);

  useEffect(() => {
    void boot();
  }, [boot]);

  useEffect(() => {
    const mq = matchMedia("(prefers-reduced-motion: reduce)");
    const on = () => setReducedMotion(mq.matches);
    mq.addEventListener?.("change", on);
    return () => mq.removeEventListener?.("change", on);
  }, []);

  // Audio may only start after a gesture; also track the input device.
  useEffect(() => {
    const unlock = () => audio.unlock();
    const onPointer = (e: PointerEvent) => {
      unlock();
      if (e.pointerType === "touch" || e.pointerType === "pen") {
        touchSeen.current = true;
        setInputMode("touch");
      } else if (e.pointerType === "mouse") setInputMode("mouse");
    };
    window.addEventListener("pointerdown", onPointer, true);
    window.addEventListener("keydown", unlock, true);
    const onVis = () => (document.hidden ? audio.suspend() : audio.resume());
    document.addEventListener("visibilitychange", onVis);
    return () => {
      window.removeEventListener("pointerdown", onPointer, true);
      window.removeEventListener("keydown", unlock, true);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [audio]);

  // ---------------------------------------------------------------- game flow

  const stopAI = useCallback(() => {
    aiCall.current?.cancel();
    aiCall.current = null;
    hintCall.current?.cancel();
    hintCall.current = null;
    setThinking(false);
  }, []);

  const showBoard = useCallback((g: GameView, events: GameEvent[] = []) => {
    displayKey.current++;
    setDisplay({ state: toState(g), events, key: displayKey.current });
  }, []);

  const scheduleResult = useCallback((g: GameView) => {
    clearTimeout(resultTimer.current);
    if (!g.winner && !g.failed) return;
    // Every result control (dialog, footer, Back) stays locked until the
    // dialog has been visible for RESULT_ARM_MS.
    resultShownAt.current = performance.now() + RESULT_DELAY_MS;
    resultTimer.current = window.setTimeout(() => {
      resultShownAt.current = performance.now();
      setResultOpen(true);
      const o = g.outcome;
      const good = g.mode === "hotseat" || o?.result === "win";
      audio.play(good ? "win" : o?.result === "draw" ? "ui" : "loss");
      haptic(good ? "success" : "error");
    }, RESULT_DELAY_MS);
  }, [audio, haptic]);

  const resultArmed = () => performance.now() - resultShownAt.current >= RESULT_ARM_MS;

  const playSounds = useCallback((events: GameEvent[]) => {
    audio.play("place");
    haptic("light");
    const pushes = events.filter((e) => e.kind === "push").length;
    const falls = events.filter((e) => e.kind === "fall").length;
    if (pushes) window.setTimeout(() => audio.play("push", Math.min(1.5, 0.7 + pushes * 0.2)), 150);
    if (falls) {
      window.setTimeout(() => audio.play("fall"), 230);
      window.setTimeout(() => haptic("medium"), 230);
    }
    if (events.some((e) => e.kind === "collapse")) window.setTimeout(() => audio.play("collapse"), 320);
  }, [audio, haptic]);

  const requestAI = useCallback((g: GameView) => {
    const be = beRef.current;
    if (!be || !g.aiTurn) return;
    aiCall.current?.cancel();
    const call = be.aiMove(g.id);
    aiCall.current = call;
    setThinking(true);
    call.promise
      .then((r) => {
        if (aiCall.current !== call) return;
        aiCall.current = null;
        setThinking(false);
        setGame(r.game);
        showBoard(r.game, r.events);
        playSounds(r.events);
        scheduleResult(r.game);
        if (r.game.aiTurn) requestAI(r.game);
      })
      .catch((e) => {
        if (aiCall.current === call) {
          aiCall.current = null;
          setThinking(false);
        }
        fail(e);
      });
  }, [fail, playSounds, scheduleResult, showBoard]);

  const enterGame = useCallback((g: GameView) => {
    stopAI();
    clearTimeout(resultTimer.current);
    setGame(g);
    showBoard(g);
    setHintCell(null);
    setPreviewCell(null);
    setResultOpen(false);
    setOverlays([]);
    setCursor(indexOf(0, 0));
    cursorX.current = 0;
    setScreen("game");
    if (g.aiTurn) requestAI(g);
    if (g.winner || g.failed) scheduleResult(g);
  }, [requestAI, scheduleResult, showBoard, stopAI]);

  const startGame = useCallback(async (o: NewGameOptions) => {
    const be = beRef.current;
    if (!be) return;
    stopAI();
    try {
      await flushSettings.current();
      const g = await be.newGame(o);
      audio.play("ui");
      enterGame(g);
      be.getProfile().then(setProfile).catch(() => {});
      if (o.mode === "puzzle") be.dailyPuzzle().then(setDaily).catch(() => {});
    } catch (e) {
      fail(e);
    }
  }, [audio, enterGame, fail, stopAI]);

  const placing = useRef(false);
  const place = useCallback(async (cell: number) => {
    const be = beRef.current;
    const g = live.current.game;
    if (!be || !g || placing.current) return;
    if (g.winner || g.failed || g.aiTurn || live.current.thinking || g.cells[cell] !== 0) {
      audio.play("deny");
      haptic("warning");
      return;
    }
    hintCall.current?.cancel();
    hintCall.current = null;
    setPreviewCell(null);
    setHintCell(null);
    placing.current = true;
    try {
      const r = await be.play(g.id, cell);
      setGame(r.game);
      showBoard(r.game, r.events);
      playSounds(r.events);
      scheduleResult(r.game);
      if (r.game.aiTurn) requestAI(r.game);
      if (r.game.winner || r.game.failed) be.getProfile().then(setProfile).catch(() => {});
    } catch (e) {
      audio.play("deny");
      fail(e);
    } finally {
      placing.current = false;
    }
  }, [audio, fail, haptic, playSounds, requestAI, scheduleResult, showBoard]);

  const undo = useCallback(async () => {
    const be = beRef.current;
    const g = live.current.game;
    if (!be || !g || !g.canUndo) return;
    stopAI();
    try {
      const v = await be.undo(g.id);
      setGame(v);
      showBoard(v);
      setHintCell(null);
      setPreviewCell(null);
      audio.play("back");
      if (v.aiTurn) requestAI(v);
    } catch (e) {
      fail(e);
    }
  }, [audio, fail, requestAI, showBoard, stopAI]);

  const hint = useCallback(async () => {
    const be = beRef.current;
    const g = live.current.game;
    if (!be || !g || g.winner || g.failed || g.aiTurn || live.current.thinking || hintCall.current) return;
    const call = be.hint(g.id);
    hintCall.current = call;
    try {
      const h = await call.promise;
      if (hintCall.current !== call) return;
      setHintCell(h.cell);
      setCursor(h.cell);
      audio.play("hint");
      setGame((cur) => (cur && cur.id === g.id ? { ...cur, hints: cur.hints + 1 } : cur));
    } catch (e) {
      fail(e);
    } finally {
      if (hintCall.current === call) hintCall.current = null;
    }
  }, [audio, fail]);

  const resign = useCallback(async () => {
    const be = beRef.current;
    const g = live.current.game;
    if (!be || !g) return;
    stopAI();
    try {
      const v = await be.resign(g.id);
      setGame(v);
      showBoard(v);
      setOverlays([]);
      scheduleResult(v);
      be.getProfile().then(setProfile).catch(() => {});
    } catch (e) {
      fail(e);
    }
  }, [fail, scheduleResult, showBoard, stopAI]);

  const toTitle = useCallback(() => {
    stopAI();
    beRef.current?.cancelAI();
    clearTimeout(resultTimer.current);
    setOverlays([]);
    setResultOpen(false);
    setPreviewCell(null);
    setHintCell(null);
    setScreen("title");
    const g = live.current.game;
    if (g && (g.winner || g.failed)) setGame(null);
    beRef.current?.getProfile().then(setProfile).catch(() => {});
  }, [stopAI]);

  const again = useCallback(() => {
    const g = live.current.game;
    if (!g) return;
    if (g.mode === "ai") void startGame({ mode: "ai", difficulty: g.difficulty, color: setup.color });
    else void startGame({ mode: g.mode, difficulty: "", color: "" });
  }, [setup.color, startGame]);

  // ---------------------------------------------------------------- input

  const openOverlay = (o: Overlay) => {
    audio.play("ui");
    setPreviewCell(null);
    setOverlays((s) => [...s, o]);
  };
  const closeOverlay = () => {
    audio.play("back");
    setOverlays((s) => s.slice(0, -1));
  };

  /**
   * The single "back" action shared by Esc, gamepad B and the Android Back
   * button. Returns false only on the title screen with nothing open, where
   * Back may leave the app.
   */
  const back = (): boolean => {
    const l = live.current;
    if (l.overlays.length) {
      closeOverlay();
      return true;
    }
    if (l.screen === "game") {
      if (l.resultOpen) {
        // The same arming guard as the result buttons.
        if (resultArmed()) setResultOpen(false);
        return true;
      }
      if (l.previewCell !== null && l.inputMode === "touch") {
        setPreviewCell(null);
        return true;
      }
      openOverlay("pause");
      return true;
    }
    // Title (or boot) with nothing open: let Android leave the app.
    return false;
  };
  const backRef = useRef(back);
  backRef.current = back;
  useEffect(() => {
    const w = window as unknown as { __basaltBack?: () => boolean };
    w.__basaltBack = () => backRef.current();
    return () => {
      delete w.__basaltBack;
    };
  }, []);

  const moveCursor = useCallback((dir: "up" | "down" | "left" | "right") => {
    setCursor((c) => {
      const { q, r } = coords[c];
      let next = -1;
      if (dir === "left" || dir === "right") {
        next = indexOf(q + (dir === "left" ? -1 : 1), r);
        if (next >= 0) cursorX.current = axialToXY(coords[next].q, coords[next].r).x;
      } else {
        const nr = r + (dir === "up" ? -1 : 1);
        const cands = dir === "up" ? [indexOf(q, nr), indexOf(q + 1, nr)] : [indexOf(q - 1, nr), indexOf(q, nr)];
        let best = Infinity;
        for (const x of cands) {
          if (x < 0) continue;
          const d = Math.abs(axialToXY(coords[x].q, coords[x].r).x - cursorX.current);
          if (d < best) {
            best = d;
            next = x;
          }
        }
      }
      if (next < 0) return c;
      audio.play("select");
      return next;
    });
  }, [audio]);

  // Keep the preview on the cursor while steering with keys or a pad.
  useEffect(() => {
    if (inputMode === "keyboard" || inputMode === "gamepad") setPreviewCell(cursor);
  }, [cursor, inputMode]);

  const gameInputActive = screen === "game" && overlays.length === 0 && !resultOpen;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "F11") {
        e.preventDefault();
        toggleFullscreen();
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        backRef.current();
        return;
      }
      const l = live.current;
      if (!(l.screen === "game" && l.overlays.length === 0 && !l.resultOpen)) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      const onButton = target?.tagName === "BUTTON" || target?.tagName === "INPUT";
      const dirs: Record<string, "up" | "down" | "left" | "right"> = {
        ArrowUp: "up", KeyW: "up", ArrowDown: "down", KeyS: "down", ArrowLeft: "left", KeyA: "left", ArrowRight: "right", KeyD: "right",
      };
      const d = dirs[e.code];
      if (d) {
        e.preventDefault();
        setInputMode("keyboard");
        if (l.inputMode !== "keyboard" && l.inputMode !== "gamepad") setPreviewCell(l.cursor);
        else moveCursor(d);
        return;
      }
      if ((e.code === "Enter" || e.code === "NumpadEnter" || e.code === "Space") && !onButton) {
        e.preventDefault();
        if (e.repeat) return;
        setInputMode("keyboard");
        if (l.inputMode === "keyboard" || l.inputMode === "gamepad") void place(l.cursor);
        else setPreviewCell(l.cursor);
        return;
      }
      if (e.code === "KeyU" && !e.repeat) void undo();
      if (e.code === "KeyH" && !e.repeat) void hint();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hint, moveCursor, place, undo]);

  usePad(gameInputActive, {
    dir: (d) => {
      if (live.current.inputMode !== "gamepad") {
        setInputMode("gamepad");
        setPreviewCell(live.current.cursor);
      } else moveCursor(d);
    },
    a: () => {
      if (live.current.inputMode !== "gamepad") {
        setInputMode("gamepad");
        setPreviewCell(live.current.cursor);
      } else void place(live.current.cursor);
    },
    x: () => void undo(),
    y: () => void hint(),
    b: () => backRef.current(),
    start: () => backRef.current(),
  });
  useGamepadMenus(screen !== "boot" && !gameInputActive, () => backRef.current());

  const onHover = useCallback((c: number | null) => {
    if (live.current.inputMode === "keyboard" || live.current.inputMode === "gamepad") setInputMode("mouse");
    setPreviewCell(c);
  }, []);

  const onActivate = useCallback((c: number, kind: PointerKind) => {
    const l = live.current;
    const g = l.game;
    if (!g) return;
    if (kind === "touch" || kind === "pen") {
      setInputMode("touch");
      if (g.cells[c] !== 0) {
        setPreviewCell(null);
        return;
      }
      if (!l.profile?.settings.confirmTouch || l.previewCell === c) {
        void place(c);
      } else {
        setPreviewCell(c);
        setCursor(c);
        audio.play("select");
      }
      return;
    }
    setInputMode("mouse");
    setCursor(c);
    void place(c);
  }, [audio, place]);

  // ---------------------------------------------------------------- settings

  const changeSettings = (s: Settings) => {
    if (!profile) return;
    setProfile({ ...profile, settings: s });
    audio.setVolume(s.volume);
    window.clearTimeout(saveTimer.current);
    pendingSettings.current = s;
    saveTimer.current = window.setTimeout(() => void flushSettings.current(), 250);
  };

  const toggleFullscreen = () => {
    if (beRef.current?.native) {
      void Window.ToggleFullscreen().catch(() => {});
    } else if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else {
      void document.documentElement.requestFullscreen?.().catch(() => {});
    }
  };

  const resetProgress = async () => {
    const be = beRef.current;
    if (!be) return;
    stopAI();
    try {
      const p = await be.resetProfile();
      setProfile(p);
      // The game in progress is gone too: return to the title.
      clearTimeout(resultTimer.current);
      setOverlays([]);
      setResultOpen(false);
      setScreen("title");
      setGame(null);
      be.dailyPuzzle().then(setDaily).catch(() => {});
      showToast(i18n.t("settings.resetDone"));
    } catch (e) {
      fail(e);
    }
  };

  const savedForfeit = game && game.forfeit ? game.difficulty : null;

  // Re-read the game from Go: an AI reply may have landed after we left.
  const continueGame = async () => {
    const be = beRef.current;
    if (!be) return;
    try {
      const g = await be.currentGame();
      if (g && !g.winner && !g.failed) enterGame(g);
      else setGame(null);
    } catch (e) {
      fail(e);
    }
  };

  // Starting another mode would forfeit an AI game you have moved in: ask.
  const startOrConfirm = (o: NewGameOptions) => {
    if (savedForfeit) {
      setPendingStart(o);
      openOverlay("forfeit");
    } else void startGame(o);
  };

  // ---------------------------------------------------------------- render

  const topOverlay = overlays[overlays.length - 1];
  const dialogOpen = overlays.length > 0 || (screen === "game" && resultOpen);
  const inertProps = dialogOpen ? { inert: "" } : {};

  return (
    <I18nProvider value={i18n}>
      {screen === "boot" && (
        <div className="screen boot">
          {bootError ? (
            <div className="boot-error" role="alert">
              <p>{i18n.t("common.bootError", { error: bootError })}</p>
              <button type="button" className="btn primary" disabled={booting} onClick={() => void boot()}>
                {i18n.t("common.retry")}
              </button>
            </div>
          ) : (
            <p className="boot-msg">{i18n.t("common.loading")}</p>
          )}
        </div>
      )}

      {screen === "title" && profile && (
        <div className="layer" {...inertProps}>
          <Title
            saved={game}
            daily={daily}
            preview={!beRef.current?.native}
            lang={lang}
            reducedMotion={reducedMotion}
            onContinue={() => void continueGame()}
            onVsAI={() => openOverlay("setup")}
            onHotseat={() => startOrConfirm({ mode: "hotseat", difficulty: "", color: "" })}
            onDaily={() => startOrConfirm({ mode: "puzzle", difficulty: "", color: "" })}
            onOpen={(w) => openOverlay(w)}
            onLanguage={(l) => profile && changeSettings({ ...profile.settings, language: l })}
          />
        </div>
      )}

      {screen === "game" && game && profile && (
        <div className="layer" {...inertProps}>
          <GameScreen
            game={game}
            display={display}
            settings={profile.settings}
            thinking={thinking}
            hintCell={hintCell}
            previewCell={previewCell}
            cursor={cursor}
            inputMode={inputMode}
            reducedMotion={reducedMotion}
            resultHidden={!resultOpen}
            onHover={onHover}
            onActivate={onActivate}
            onPlace={() => previewCell !== null && void place(previewCell)}
            onUndo={() => void undo()}
            onHint={() => void hint()}
            onMoves={() => openOverlay("history")}
            onMenu={() => openOverlay("pause")}
            onAgain={() => resultArmed() && again()}
            onShowResult={() => {
              resultShownAt.current = performance.now();
              setResultOpen(true);
            }}
          />
        </div>
      )}

      {screen === "game" && game && resultOpen && overlays.length === 0 && (
        <ResultDialog
          game={game}
          onAgain={() => resultArmed() && again()}
          onReview={() => resultArmed() && setResultOpen(false)}
          onMenu={() => resultArmed() && toTitle()}
        />
      )}

      {topOverlay === "setup" && (
        <SetupDialog
          difficulty={setup.difficulty}
          color={setup.color}
          forfeitLevel={savedForfeit}
          onChange={(difficulty, color) => setSetup({ difficulty, color })}
          onStart={() => void startGame({ mode: "ai", difficulty: setup.difficulty, color: setup.color })}
          onBack={closeOverlay}
        />
      )}
      {topOverlay === "pause" && game && (
        <PauseDialog
          canResign={!game.winner && !game.failed && game.history.length > 0}
          onResume={closeOverlay}
          onHowTo={() => openOverlay("howto")}
          onSettings={() => openOverlay("settings")}
          onResign={() => void resign()}
          onMenu={toTitle}
        />
      )}
      {topOverlay === "settings" && profile && (
        <SettingsDialog
          settings={profile.settings}
          native={!!beRef.current?.native}
          touch={touchSeen.current}
          onChange={changeSettings}
          onReset={() => void resetProgress()}
          onFullscreen={toggleFullscreen}
          onBack={closeOverlay}
        />
      )}
      {topOverlay === "forfeit" && savedForfeit && pendingStart && (
        <ForfeitDialog level={savedForfeit} onStart={() => void startGame(pendingStart)} onBack={closeOverlay} />
      )}
      {topOverlay === "howto" && <HowToDialog onBack={closeOverlay} />}
      {topOverlay === "profile" && profile && <ProfileDialog profile={profile} daily={daily} onBack={closeOverlay} />}
      {topOverlay === "history" && game && <HistoryDialog history={game.history} onBack={closeOverlay} />}

      {toast && (
        <div className="toast" role="alert">
          {toast}
        </div>
      )}
    </I18nProvider>
  );
}
