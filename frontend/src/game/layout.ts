// Pixel geometry for the causeway: pointy-top hexes, squashed vertically to
// suggest a camera looking down at an angle, each column extruded downward.

import { DIR_Q, DIR_R, NUM_CELLS, coords } from "./rules";

export const S = 10; // hex circumradius in SVG units
export const K = 0.8; // vertical squash of the top faces
export const BASE = 9; // column side height at zero elevation
const SQ3 = Math.sqrt(3);

/** Deterministic per-column elevation, so the causeway looks hand-hewn. */
export const elevation: number[] = coords.map(({ q, r }) => {
  const h = Math.sin(q * 12.9898 + r * 78.233) * 43758.5453;
  const f = h - Math.floor(h);
  return Math.round(f * 26) / 10; // 0 … 2.6
});

export function axialToXY(q: number, r: number) {
  return { x: S * SQ3 * (q + r / 2), y: S * 1.5 * K * r };
}

/** Centre of a column's top face. */
export const centers = coords.map(({ q, r }, c) => {
  const p = axialToXY(q, r);
  return { x: p.x, y: p.y - elevation[c] };
});

/** Where a stone pushed off the board in direction d from cell c lands. */
export function offBoard(c: number, d: number, drop = BASE + 4) {
  const { q, r } = coords[c];
  const p = axialToXY(q + DIR_Q[d], r + DIR_R[d]);
  return { x: p.x, y: p.y - elevation[c] + drop };
}

const ANG = [-30, 30, 90, 150, 210, 270].map((a) => (a * Math.PI) / 180);
export const corner = (i: number, scale = 1) => ({ x: S * scale * Math.cos(ANG[i]), y: S * scale * K * Math.sin(ANG[i]) });

export function hexPoints(cx: number, cy: number, scale = 1): string {
  let out = "";
  for (let i = 0; i < 6; i++) {
    const p = corner(i, scale);
    out += `${(cx + p.x).toFixed(2)},${(cy + p.y).toFixed(2)} `;
  }
  return out.trim();
}

/** The two visible side faces of a column (lower-right, lower-left). */
export function sideFaces(c: number) {
  const { x, y } = centers[c];
  const h = BASE + elevation[c];
  const v30 = corner(1);
  const v90 = corner(2);
  const v150 = corner(3);
  const pts = (a: { x: number; y: number }, b: { x: number; y: number }) =>
    `${x + a.x},${y + a.y} ${x + b.x},${y + b.y} ${x + b.x},${y + b.y + h} ${x + a.x},${y + a.y + h}`;
  return { right: pts(v30, v90), left: pts(v90, v150) };
}

/** Bounding box of a set of cells, padded, as an SVG viewBox string. */
export function viewBoxFor(cells: number[], pad = 4): string {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const c of cells) {
    const { x, y } = centers[c];
    x0 = Math.min(x0, x - S);
    x1 = Math.max(x1, x + S);
    y0 = Math.min(y0, y - S * K);
    y1 = Math.max(y1, y + S * K + BASE + elevation[c]);
  }
  return `${(x0 - pad).toFixed(1)} ${(y0 - pad).toFixed(1)} ${(x1 - x0 + 2 * pad).toFixed(1)} ${(y1 - y0 + 2 * pad).toFixed(1)}`;
}

export const ALL_CELLS = Array.from({ length: NUM_CELLS }, (_, i) => i);
/** Columns drawn back to front (top rows first). */
export const DRAW_ORDER = ALL_CELLS.slice().sort((a, b) => coords[a].r - coords[b].r || coords[a].q - coords[b].q);
