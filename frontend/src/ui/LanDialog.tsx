import { useEffect, useRef, useState } from "react";
import type { LANGame, LanApi, NetStatus } from "../api/backend";
import { Key, useI18n } from "../i18n";
import { Dialog } from "./common";

type View = "choose" | "host" | "join";

interface Props {
  lan: LanApi;
  net: NetStatus | null;
  /** An unfinished AI game that starting a network game would forfeit. */
  forfeitLevel: string | null;
  onHost: () => Promise<void>;
  /** Resolves to an error message, or null on success or cancellation. */
  onJoin: (code: string) => Promise<string | null>;
  /** Stop hosting/joining and stay in the dialog. */
  onStop: () => void;
  onClose: () => void;
}

/** Crockford base32 as typed: upper case, no I/L/O/U, look-alikes mapped. */
function cleanCode(s: string): string {
  return s
    .toUpperCase()
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1")
    .replace(/[^0-9A-HJKMNP-TV-Z]/g, "")
    .slice(0, 8);
}

const shown = (c: string) => (c.length > 4 ? `${c.slice(0, 4)}-${c.slice(4)}` : c);

// Where connections most often fail: Windows Firewall on the host, and mobile
// data or a VPN taking over a phone's traffic.
const onWindows = /Windows/.test(navigator.userAgent);
const onPhone = /Android|iPhone|iPad/.test(navigator.userAgent);

export function LanDialog(p: Props) {
  const { t } = useI18n();
  const hosting = p.net?.role === "host" && p.net.phase === "hosting";
  const [view, setView] = useState<View>(hosting ? "host" : "choose");

  const back = () => {
    if (view === "host") p.onStop();
    if (view === "choose") p.onClose();
    else setView("choose");
  };

  return (
    <Dialog key={view} title={t("lan.title")} className="lan" initialFocus={view === "choose" ? ".menu-item" : ".lan-back"}>
      {view === "choose" && (
        <>
          <p className="lan-needs">{t("lan.needs")}</p>
          <div className="stack lan-choices">
            <button
              type="button"
              className="menu-item boxed"
              onClick={() => {
                setView("host");
                void p.onHost();
              }}
            >
              <span className="menu-label">{t("lan.host")}</span>
              <span className="menu-sub">{t("lan.hostSub")}</span>
            </button>
            <button type="button" className="menu-item boxed" onClick={() => setView("join")}>
              <span className="menu-label">{t("lan.join")}</span>
              <span className="menu-sub">{t("lan.joinSub")}</span>
            </button>
          </div>
          {p.forfeitLevel && <p className="note warn">{t("setup.forfeit", { level: t(`level.${p.forfeitLevel}` as Key) })}</p>}
          <div className="panel-actions">
            <button type="button" className="btn lan-back" onClick={back}>
              {t("common.back")}
            </button>
          </div>
        </>
      )}
      {view === "host" && <HostView net={p.net} onBack={back} />}
      {view === "join" && <JoinView lan={p.lan} onJoin={p.onJoin} onStop={p.onStop} onBack={back} />}
    </Dialog>
  );
}

function HostView({ net, onBack }: { net: NetStatus | null; onBack: () => void }) {
  const { t } = useI18n();
  const ready = net !== null && net.role === "host" && net.phase === "hosting" && net.code !== "";
  return (
    <>
      <div className="lan-code-block">
        <span className="choice-label">{t("lan.yourCode")}</span>
        {ready ? (
          <span className="lan-code" aria-label={net.code.split("").join(" ")}>
            {net.code}
          </span>
        ) : (
          <span className="lan-code placeholder" aria-hidden="true">
            ····-····
          </span>
        )}
      </div>
      {ready && (
        <>
          <p className="lan-help">{t("lan.hostHelp")}</p>
          {net.altCodes.length > 0 && (
            <p className="lan-alt">
              {t("lan.otherCodes")}{" "}
              {net.altCodes.map((c) => (
                <span key={c} className="lan-code small">
                  {c}
                </span>
              ))}
            </p>
          )}
          {!net.discoverable && <p className="note">{t("lan.hidden")}</p>}
          {onWindows && <p className="lan-tip">{t("lan.tipFirewall")}</p>}
          {onPhone && <p className="lan-tip">{t("lan.tipPhone")}</p>}
          <p className="lan-wait" role="status">
            <span className="pulse" aria-hidden="true" />
            {t("lan.waiting")}
          </p>
        </>
      )}
      <div className="panel-actions">
        <button type="button" className="btn lan-back" onClick={onBack}>
          {t("lan.cancel")}
        </button>
      </div>
    </>
  );
}

function JoinView({ lan, onJoin, onStop, onBack }: { lan: LanApi; onJoin: Props["onJoin"]; onStop: () => void; onBack: () => void }) {
  const { t } = useI18n();
  const [games, setGames] = useState<LANGame[]>([]);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState<string | null>(null); // the code being joined
  const [error, setError] = useState<string | null>(null);
  const mounted = useRef(true);

  // After a failed join, put the caret back in the code box to fix it.
  useEffect(() => {
    if (!busy && error) document.getElementById("lan-code")?.focus();
  }, [busy, error]);

  // Look for games while this view is open.
  useEffect(() => {
    mounted.current = true;
    let timer: number | undefined;
    const poll = async () => {
      try {
        const g = await lan.browse();
        if (mounted.current) setGames(g);
      } catch {
        /* no network: the list stays empty and the code still works */
      }
      if (mounted.current) timer = window.setTimeout(poll, 1000);
    };
    void poll();
    return () => {
      mounted.current = false;
      window.clearTimeout(timer);
      lan.stopBrowse();
    };
  }, [lan]);

  const join = async (c: string) => {
    if (busy) return;
    setBusy(c);
    setError(null);
    const err = await onJoin(c);
    if (!mounted.current) return;
    setBusy(null);
    setError(err);
  };

  const cancel = () => {
    if (busy) {
      onStop();
      setBusy(null);
    } else onBack();
  };

  return (
    <>
      <h3 className="section-title first">{t("lan.nearby")}</h3>
      {games.length ? (
        <ul className="lan-games">
          {games.map((g) => (
            <li key={g.code}>
              <button type="button" className="choice-option lan-game" disabled={!!busy} onClick={() => void join(g.code)}>
                <span className="choice-name">{g.name || t("lan.gameCode", { code: g.code })}</span>
                {g.name && <span className="choice-sub lan-code-inline">{g.code}</span>}
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <>
          <p className="lan-wait muted" role="status">
            <span className="pulse" aria-hidden="true" />
            {t("lan.searching")}
          </p>
          <p className="lan-help">{t("lan.noneFound")}</p>
        </>
      )}

      <form
        className="lan-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (code.length === 8) void join(code);
        }}
      >
        <label className="choice-label" htmlFor="lan-code">
          {t("lan.codeLabel")}
        </label>
        <div className="lan-form-row">
          <input
            id="lan-code"
            className="code-input"
            value={shown(code)}
            onChange={(e) => {
              setCode(cleanCode(e.target.value));
              setError(null);
            }}
            placeholder="ABCD-EFGH"
            inputMode="text"
            autoCapitalize="characters"
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            enterKeyHint="go"
            maxLength={9}
            disabled={!!busy}
            aria-invalid={!!error}
            aria-describedby={error ? "lan-error" : undefined}
          />
          <button type="submit" className="btn primary" disabled={code.length !== 8 || !!busy}>
            {busy ? t("lan.connecting") : t("lan.connect")}
          </button>
        </div>
      </form>
      {error && (
        <p id="lan-error" className="note warn" role="alert">
          {error}
        </p>
      )}
      {onPhone && <p className="lan-tip">{t("lan.tipPhone")}</p>}
      <div className="panel-actions">
        <button type="button" className="btn lan-back" onClick={cancel}>
          {busy ? t("lan.cancel") : t("common.back")}
        </button>
      </div>
    </>
  );
}
