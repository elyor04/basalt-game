import { useEffect, useRef } from "react";
import { focusables } from "./common";

// Gamepad support. In menus the d-pad or left stick moves focus to the
// nearest control in that direction (spatial navigation), A activates it and
// B goes back. On a focused slider, left/right change its value. During play
// the game screen takes the pad over through usePadButtons.

type Dir = "up" | "down" | "left" | "right";

/** The top-most open surface: dialogs render after the screen. */
function activeRoot(): HTMLElement | null {
  const roots = document.querySelectorAll<HTMLElement>(".scrim, .screen");
  return roots.length ? roots[roots.length - 1] : null;
}

export function markGamepad() {
  document.documentElement.classList.add("gamepad-nav");
}

function stepSlider(el: HTMLInputElement, dir: 1 | -1) {
  if (dir > 0) el.stepUp();
  else el.stepDown();
  // stepUp/stepDown bypass React's value tracking; an input event fixes that.
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

function move(dir: Dir) {
  const root = activeRoot();
  if (!root) return;
  const items = focusables(root);
  if (!items.length) return;
  const cur = document.activeElement as HTMLElement | null;
  if (cur instanceof HTMLInputElement && cur.type === "range" && (dir === "left" || dir === "right") && items.includes(cur)) {
    stepSlider(cur, dir === "right" ? 1 : -1);
    return;
  }
  if (!cur || !items.includes(cur)) {
    items[0].focus();
    return;
  }
  const a = cur.getBoundingClientRect();
  const ax = a.left + a.width / 2;
  const ay = a.top + a.height / 2;
  let best: HTMLElement | null = null;
  let bestScore = Infinity;
  for (const el of items) {
    if (el === cur) continue;
    const b = el.getBoundingClientRect();
    const dx = b.left + b.width / 2 - ax;
    const dy = b.top + b.height / 2 - ay;
    const primary = dir === "up" ? -dy : dir === "down" ? dy : dir === "left" ? -dx : dx;
    if (primary <= 4) continue;
    const cross = dir === "up" || dir === "down" ? Math.abs(dx) : Math.abs(dy);
    const score = primary + cross * 2;
    if (score < bestScore) {
      bestScore = score;
      best = el;
    }
  }
  if (best) {
    best.focus();
    best.scrollIntoView({ block: "nearest" });
  }
}

function activate() {
  const root = activeRoot();
  if (!root) return;
  const cur = document.activeElement as HTMLElement | null;
  const items = focusables(root);
  if (cur && items.includes(cur)) {
    if (!(cur instanceof HTMLInputElement && cur.type === "range")) cur.click();
  } else if (items[0]) {
    items[0].focus();
  }
}

export interface PadHandlers {
  dir?: (d: Dir) => void;
  a?: () => void;
  b?: () => void;
  x?: () => void;
  y?: () => void;
  start?: () => void;
}

/**
 * Poll the first connected gamepad while `active`, calling handlers on
 * presses (directions auto-repeat). Buttons already held when polling starts
 * are ignored until released, so a press that closed one screen cannot
 * trigger the next.
 */
export function usePad(active: boolean, handlers: PadHandlers) {
  const h = useRef(handlers);
  h.current = handlers;
  useEffect(() => {
    if (!active) return;
    let raf = 0;
    let prev: boolean[] | null = null;
    let lastDir: Dir | "" = "";
    let repeatAt = 0;
    const poll = (now: number) => {
      raf = requestAnimationFrame(poll);
      const pads = navigator.getGamepads ? navigator.getGamepads() : [];
      const pad = [...pads].find((p) => p && p.connected);
      if (!pad) {
        prev = null;
        return;
      }
      const b = pad.buttons.map((x) => x.pressed);
      if (prev === null) {
        prev = b;
        return;
      }
      const ax = pad.axes[0] ?? 0;
      const ay = pad.axes[1] ?? 0;
      const dir: Dir | "" = b[12] || ay < -0.6 ? "up" : b[13] || ay > 0.6 ? "down" : b[14] || ax < -0.6 ? "left" : b[15] || ax > 0.6 ? "right" : "";
      if (dir && (dir !== lastDir || now >= repeatAt)) {
        markGamepad();
        h.current.dir?.(dir);
        repeatAt = now + (dir !== lastDir ? 380 : 130);
      }
      lastDir = dir;
      const pressed = (i: number) => b[i] && !prev![i];
      if (pressed(0)) {
        markGamepad();
        h.current.a?.();
      }
      if (pressed(1)) h.current.b?.();
      if (pressed(2)) h.current.x?.();
      if (pressed(3)) h.current.y?.();
      if (pressed(9)) h.current.start?.();
      prev = b;
    };
    raf = requestAnimationFrame(poll);
    return () => cancelAnimationFrame(raf);
  }, [active]);
}

/** Spatial focus navigation for menus and dialogs. */
export function useGamepadMenus(active: boolean, onBack: () => void) {
  useEffect(() => {
    const off = () => document.documentElement.classList.remove("gamepad-nav");
    window.addEventListener("pointerdown", off);
    window.addEventListener("mousemove", off);
    return () => {
      window.removeEventListener("pointerdown", off);
      window.removeEventListener("mousemove", off);
    };
  }, []);
  usePad(active, { dir: move, a: activate, b: onBack, start: onBack });
}
