import { memo, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { BASE, DRAW_ORDER, K, S, ALL_CELLS, centers, elevation, hexPoints, offBoard, sideFaces, viewBoxFor } from "../game/layout";
import { ASH, EMPTY, GAP, GameEvent, LAVA, REASON_LINE, State, legal, play, winningLine } from "../game/rules";

export type PointerKind = "mouse" | "touch" | "pen" | "keyboard";

interface Props {
  state: State;
  /** Events of the move that produced `state` (animated once per moveKey). */
  events: GameEvent[];
  moveKey: number;
  interactive?: boolean;
  previewCell?: number | null;
  showPushes?: boolean;
  cursor?: number | null;
  hintCell?: number | null;
  winLine?: number[];
  reducedMotion?: boolean;
  /** Crop the view to these cells and hide the rest (rule diagrams). */
  viewCells?: number[];
  intro?: boolean;
  label: string;
  onHover?: (cell: number | null) => void;
  onActivate?: (cell: number, kind: PointerKind) => void;
}

interface Sprite {
  id: number;
  cell: number;
  color: number;
  x: number;
  y: number;
  fresh?: boolean;
  falling?: boolean;
}

interface Splash {
  id: number;
  x: number;
  y: number;
}

let nextSpriteId = 1;

/** Sprites for `cells`, reusing ids of stones already standing there. */
function spritesFor(cells: number[], prev: Sprite[]): Sprite[] {
  const byCell = new Map<number, Sprite>();
  for (const s of prev) if (!s.falling) byCell.set(s.cell, s);
  const out: Sprite[] = [];
  cells.forEach((v, c) => {
    if (v !== LAVA && v !== ASH) return;
    const old = byCell.get(c);
    const { x, y } = centers[c];
    out.push({ id: old && old.color === v ? old.id : nextSpriteId++, cell: c, color: v, x, y });
  });
  return out;
}

const STONE_H = 1.6; // a stone's thickness in SVG units
const PHASE_PUSH_MS = 150;
const PHASE_DONE_MS = 760;

function BoardImpl(props: Props) {
  const {
    state, events, moveKey, interactive = false, previewCell = null, showPushes = true, cursor = null, hintCell = null,
    winLine = [], reducedMotion = false, viewCells, intro = false, label, onHover, onActivate,
  } = props;
  const cells = state.cells;
  const [sprites, setSprites] = useState<Sprite[]>(() => spritesFor(cells, []));
  const [crumbling, setCrumbling] = useState<number[]>([]);
  const [splashes, setSplashes] = useState<Splash[]>([]);
  const [settled, setSettled] = useState(true);
  const prevCells = useRef<number[]>(cells);
  const prevKey = useRef(moveKey);
  const spritesRef = useRef(sprites);
  spritesRef.current = sprites;
  const timers = useRef<number[]>([]);
  const lastPointer = useRef<PointerKind>("mouse");

  // Animate each new move: drop the stone, slide the pushed stones, sink the
  // fallen ones and crumble their columns, then settle on the final state.
  useLayoutEffect(() => {
    if (prevKey.current === moveKey) return;
    prevKey.current = moveKey;
    timers.current.forEach(clearTimeout);
    timers.current = [];
    const before = prevCells.current;
    prevCells.current = cells;
    const finalize = () => {
      setSprites((cur) => spritesFor(cells, cur));
      setCrumbling([]);
      setSplashes([]);
      setSettled(true);
    };
    const place = events.find((e) => e.kind === "place");
    if (!place || reducedMotion || before.length !== cells.length) {
      finalize();
      return;
    }
    const base = spritesFor(before, spritesRef.current);
    const placed: Sprite = { id: nextSpriteId++, cell: place.from, color: place.color, ...centers[place.from], fresh: true };
    setSprites([...base, placed]);
    setSettled(false);
    setCrumbling([]);
    setSplashes([]);
    timers.current.push(
      window.setTimeout(() => {
        const byCell = new Map(base.map((s) => [s.cell, s]));
        const moved = new Map<number, Sprite>();
        const sp: Splash[] = [];
        const crumble: number[] = [];
        for (const e of events) {
          const s = byCell.get(e.from);
          if (e.kind === "push" && s) moved.set(s.id, { ...s, cell: e.to, ...centers[e.to] });
          if (e.kind === "fall" && s) {
            const target = e.to >= 0 ? { x: centers[e.to].x, y: centers[e.to].y + BASE } : offBoard(e.from, e.dir);
            moved.set(s.id, { ...s, falling: true, ...target });
            sp.push({ id: nextSpriteId++, ...target });
          }
          if (e.kind === "collapse") crumble.push(e.from);
        }
        setSprites([...base.map((s) => moved.get(s.id) ?? s), { ...placed, fresh: false }]);
        setCrumbling(crumble);
        setSplashes(sp);
      }, PHASE_PUSH_MS),
      window.setTimeout(finalize, PHASE_DONE_MS),
    );
  }, [moveKey, cells, events, reducedMotion]);

  useEffect(() => () => timers.current.forEach(clearTimeout), []);

  const preview = useMemo(() => {
    if (previewCell === null || !legal(state, previewCell)) return null;
    const r = play(state, previewCell);
    const line = r.state.winner && r.state.reason === REASON_LINE ? winningLine(r.state, r.state.winner) : null;
    return { events: r.events, winner: r.state.winner, line };
  }, [state, previewCell]);

  const viewBox = useMemo(() => viewBoxFor(viewCells ?? ALL_CELLS, viewCells ? 3 : 6), [viewCells]);
  const inView = useMemo(() => (viewCells ? new Set(viewCells) : null), [viewCells]);
  const lastPlace = events.find((e) => e.kind === "place");

  const arrow = (from: { x: number; y: number }, to: { x: number; y: number }, cls: string, key: string) => {
    const dx = to.x - from.x;
    const dy = to.y - from.y;
    const len = Math.hypot(dx, dy) || 1;
    const ux = dx / len;
    const uy = dy / len;
    const a = { x: from.x + ux * 3.2, y: from.y + uy * 3.2 };
    const b = { x: to.x - ux * 3.6, y: to.y - uy * 3.6 };
    const head = `${b.x + ux * 2.6},${b.y + uy * 2.6} ${b.x - uy * 1.7},${b.y + ux * 1.7} ${b.x + uy * 1.7},${b.y - ux * 1.7}`;
    return (
      <g key={key} className={`arrow ${cls}`}>
        <line className="arrow-halo" x1={a.x} y1={a.y} x2={b.x} y2={b.y} />
        <line className="arrow-line" x1={a.x} y1={a.y} x2={b.x} y2={b.y} />
        <polygon className="arrow-head" points={head} />
      </g>
    );
  };

  const eventArrows = (evs: GameEvent[], cls: string) =>
    evs.map((e, i) => {
      if (e.kind === "push") return arrow(centers[e.from], centers[e.to], cls, `${cls}${i}`);
      if (e.kind === "fall") {
        const to = e.to >= 0 ? centers[e.to] : offBoard(e.from, e.dir, 0);
        return arrow(centers[e.from], to, `${cls} sink`, `${cls}${i}`);
      }
      return null;
    });

  return (
    <svg
      className={`board${intro ? " intro" : ""}${interactive ? " interactive" : ""}`}
      viewBox={viewBox}
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label={label}
      onPointerLeave={(e) => {
        // Touch fires pointerleave after every tap; only a mouse leaving the
        // board should clear the hover preview.
        if (e.pointerType === "mouse") onHover?.(null);
      }}
    >
      <defs>
        <radialGradient id="g-lava" cx="40%" cy="35%" r="70%">
          <stop offset="0" stopColor="#FFE0A3" />
          <stop offset="0.45" stopColor="#FF8A3D" />
          <stop offset="1" stopColor="#B83A0E" />
        </radialGradient>
        <radialGradient id="g-ash" cx="40%" cy="35%" r="70%">
          <stop offset="0" stopColor="#FFFFFF" />
          <stop offset="0.6" stopColor="#E9E4DA" />
          <stop offset="1" stopColor="#B3AA9A" />
        </radialGradient>
        <radialGradient id="g-glow" cx="50%" cy="50%" r="50%">
          <stop offset="0" stopColor="#FF7A2E" stopOpacity="0.55" />
          <stop offset="1" stopColor="#FF7A2E" stopOpacity="0" />
        </radialGradient>
        <radialGradient id="g-sea" cx="50%" cy="50%" r="50%">
          <stop offset="0" stopColor="var(--sea-deep)" />
          <stop offset="0.72" stopColor="var(--sea)" stopOpacity="0.85" />
          <stop offset="1" stopColor="var(--sea)" stopOpacity="0" />
        </radialGradient>
        <linearGradient id="g-side-l" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="var(--col-side-l)" />
          <stop offset="1" stopColor="var(--col-foot)" />
        </linearGradient>
        <linearGradient id="g-side-r" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="var(--col-side-r)" />
          <stop offset="1" stopColor="var(--col-foot)" />
        </linearGradient>
      </defs>

      {!viewCells && <ellipse className="sea" cx={0} cy={BASE * 0.6} rx={S * 9.4} ry={S * 6.4} />}

      {/* Columns, back to front. */}
      <g className="columns">
        {DRAW_ORDER.map((c) => {
          if (inView && !inView.has(c)) return null;
          const v = cells[c];
          const isGap = v === GAP && !crumbling.includes(c);
          const { x, y } = centers[c];
          const d = Math.hypot(x, y);
          const style = intro ? { animationDelay: `${Math.round(d * 9)}ms` } : undefined;
          if (isGap) {
            return (
              <g key={c} className="gap" style={style}>
                <polygon className="gap-water" points={hexPoints(x, y + BASE * 0.7 + elevation[c], 0.96)} />
                <polygon className="gap-ripple" points={hexPoints(x, y + BASE * 0.7 + elevation[c], 0.6)} />
              </g>
            );
          }
          const f = sideFaces(c);
          return (
            <g key={c} className={`col${crumbling.includes(c) ? " crumbling" : ""}`} style={style}>
              <polygon className="side-l" points={f.left} />
              <polygon className="side-r" points={f.right} />
              <polygon className="top" points={hexPoints(x, y)} />
              <polygon className="top-inner" points={hexPoints(x, y, 0.8)} />
            </g>
          );
        })}
      </g>

      {/* Last move: faint trails of what was pushed, once the dust settles. */}
      {settled && !preview && <g className="trails">{eventArrows(events, "trail")}</g>}
      {settled && lastPlace && cells[lastPlace.from] !== EMPTY && (
        <ellipse className="last-ring" cx={centers[lastPlace.from].x} cy={centers[lastPlace.from].y} rx={S * 0.86} ry={S * 0.86 * K} />
      )}

      {/* Stones. */}
      <g className="stones">
        {sprites.map((s) => (
          <g key={s.id} className={`stone-pos${s.falling ? " falling" : ""}`} style={{ transform: `translate(${s.x}px, ${s.y}px)` }}>
            <g className={`stone ${s.color === LAVA ? "lava" : "ash"}${s.fresh ? " fresh" : ""}${s.falling ? " sinking" : ""}`}>
              {/* The stone stands on the column: its base is centred on the
                  top face and its top is lifted by its thickness, so the
                  footprint (not the top) matches the hex. */}
              {s.color === LAVA && <circle className="glow" cy={-STONE_H / 2} r={S * 1.05} />}
              <ellipse className="shadow" cx={0.6} cy={1} rx={S * 0.6} ry={S * 0.6 * K * 0.85} />
              <ellipse className="body-side" cx={0} cy={0} rx={S * 0.58} ry={S * 0.58 * K} />
              <ellipse className="body" cx={0} cy={-STONE_H} rx={S * 0.58} ry={S * 0.58 * K} />
              <ellipse className="sheen" cx={-1.6} cy={-1.5 - STONE_H} rx={S * 0.2} ry={S * 0.12} />
            </g>
          </g>
        ))}
      </g>

      {splashes.map((s) => (
        <ellipse key={s.id} className="splash" cx={s.x} cy={s.y} rx={S * 0.7} ry={S * 0.7 * K * 0.6} />
      ))}

      {winLine.length > 0 && settled && (
        <g className="win-line">
          <polyline points={winLine.map((c) => `${centers[c].x},${centers[c].y}`).join(" ")} />
        </g>
      )}

      {/* Overlays: hint, preview, cursor. */}
      {hintCell !== null && (
        <ellipse className="hint-ring" cx={centers[hintCell].x} cy={centers[hintCell].y} rx={S * 0.9} ry={S * 0.9 * K} />
      )}
      {preview && previewCell !== null && (
        <g className="preview" aria-hidden="true">
          {preview.line && (
            <polyline className={`preview-line${preview.winner === state.toMove ? "" : " danger"}`} points={preview.line.map((c) => `${centers[c].x},${centers[c].y}`).join(" ")} />
          )}
          <g transform={`translate(${centers[previewCell].x} ${centers[previewCell].y})`} className={`ghost ${state.toMove === LAVA ? "lava" : "ash"}`}>
            <ellipse className="body" rx={S * 0.58} ry={S * 0.58 * K} />
          </g>
          {showPushes && (
            <>
              {preview.events.map((e, i) =>
                e.kind === "fall" ? (
                  <ellipse key={`r${i}`} className="sink-ring" cx={centers[e.from].x} cy={centers[e.from].y} rx={S * 0.74} ry={S * 0.74 * K} />
                ) : e.kind === "blocked" ? (
                  <line
                    key={`b${i}`}
                    className="blocked"
                    x1={centers[e.from].x + (centers[e.to].x - centers[e.from].x) * 0.42 - (centers[e.to].y - centers[e.from].y) * 0.25}
                    y1={centers[e.from].y + (centers[e.to].y - centers[e.from].y) * 0.42 + (centers[e.to].x - centers[e.from].x) * 0.25}
                    x2={centers[e.from].x + (centers[e.to].x - centers[e.from].x) * 0.42 + (centers[e.to].y - centers[e.from].y) * 0.25}
                    y2={centers[e.from].y + (centers[e.to].y - centers[e.from].y) * 0.42 - (centers[e.to].x - centers[e.from].x) * 0.25}
                  />
                ) : null,
              )}
              {eventArrows(preview.events, "push")}
            </>
          )}
        </g>
      )}
      {cursor !== null && (
        <polygon className="cursor" points={hexPoints(centers[cursor].x, centers[cursor].y, 1.02)} />
      )}

      {/* Hit targets on top of everything, one per column. */}
      {interactive && (
        <g className="hits">
          {ALL_CELLS.map((c) => (
            <polygon
              key={c}
              points={hexPoints(centers[c].x, centers[c].y, 1.08)}
              className={cells[c] === EMPTY ? "hit free" : "hit"}
              onPointerDown={(e) => {
                lastPointer.current = (e.pointerType as PointerKind) || "mouse";
              }}
              onPointerEnter={(e) => {
                if (e.pointerType === "mouse") onHover?.(c);
              }}
              onClick={() => onActivate?.(c, lastPointer.current)}
            />
          ))}
        </g>
      )}
    </svg>
  );
}

export const Board = memo(BoardImpl);
