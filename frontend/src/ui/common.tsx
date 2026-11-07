import { ReactNode, useEffect, useId, useRef, useState } from "react";

const FOCUSABLE = 'button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex="0"], a[href]';

export function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => el.offsetParent !== null && !el.closest("[inert]"));
}

interface DialogProps {
  title: string;
  children: ReactNode;
  /** Extra class on the panel (e.g. "wide"). */
  className?: string;
  /** Selector of the element to focus first; defaults to the first control. */
  initialFocus?: string;
  /** Hide the visible heading (it still labels the dialog). */
  hideTitle?: boolean;
}

/**
 * A modal panel. Focus moves into it on open, Tab and Shift+Tab cycle inside
 * it, and focus returns to where it was when it closes. The app marks the
 * rest of the page `inert` while a dialog is open.
 */
export function Dialog({ title, children, className = "", initialFocus, hideTitle }: DialogProps) {
  const id = useId();
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const root = ref.current!;
    const first = (initialFocus && root.querySelector<HTMLElement>(initialFocus)) || focusables(root)[0];
    first?.focus({ preventScroll: true });
    return () => {
      if (prev && document.contains(prev)) prev.focus({ preventScroll: true });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== "Tab") return;
    // Roving-tabindex groups keep only their selected option in the Tab order.
    const items = focusables(ref.current!).filter((el) => el.tabIndex >= 0);
    if (!items.length) return;
    const i = items.indexOf(document.activeElement as HTMLElement);
    e.preventDefault();
    const next = e.shiftKey ? (i <= 0 ? items.length - 1 : i - 1) : i === items.length - 1 ? 0 : i + 1;
    items[next].focus();
  };
  return (
    <div className="scrim">
      <div ref={ref} className={`panel ${className}`} role="dialog" aria-modal="true" aria-labelledby={id} onKeyDown={onKeyDown}>
        <h2 id={id} className={hideTitle ? "visually-hidden" : "panel-title"}>
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}

const ARM_MS = 450;

interface ConfirmProps {
  label: string;
  confirmLabel: string;
  onConfirm: () => void;
  className?: string;
}

/**
 * A destructive action that needs a second, deliberate press. The confirm
 * state ignores presses for a moment after arming (no double-click or held
 * Enter can skip it) and survives the blur WebKit fires when a button is
 * clicked without taking focus. It cancels when focus moves elsewhere, on a
 * press outside it, or after a few seconds.
 */
export function ConfirmButton({ label, confirmLabel, onConfirm, className = "btn" }: ConfirmProps) {
  const [asking, setAsking] = useState(false);
  const shownAt = useRef(0);
  const ref = useRef<HTMLButtonElement>(null);

  // One element for both states, so keyboard focus survives arming and
  // disarming.
  useEffect(() => {
    if (!asking) return;
    shownAt.current = performance.now();
    const outside = (e: PointerEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setAsking(false);
    };
    const timer = window.setTimeout(() => setAsking(false), 8000);
    document.addEventListener("pointerdown", outside, true);
    return () => {
      document.removeEventListener("pointerdown", outside, true);
      clearTimeout(timer);
    };
  }, [asking]);

  return (
    <button
      ref={ref}
      type="button"
      className={asking ? `${className} danger` : className}
      aria-live="polite"
      onPointerDown={(e) => {
        // WebKit blurs the focused element when a button is pressed; keep the
        // armed state from being cancelled by its own click.
        if (asking) e.preventDefault();
      }}
      onClick={(e) => {
        if (!asking) {
          if (e.detail > 1) return;
          setAsking(true);
          return;
        }
        if (e.detail > 1 || performance.now() - shownAt.current < ARM_MS) return;
        setAsking(false);
        onConfirm();
      }}
      onKeyDown={(e) => {
        if (e.repeat) e.preventDefault();
      }}
      onBlur={(e) => {
        if (e.relatedTarget) setAsking(false);
      }}
    >
      {asking ? confirmLabel : label}
    </button>
  );
}

interface ToggleProps {
  label: string;
  sub?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}

export function Toggle({ label, sub, checked, onChange }: ToggleProps) {
  return (
    <button type="button" role="switch" aria-checked={checked} className="row toggle" onClick={() => onChange(!checked)}>
      <span className="row-text">
        <span className="row-label">{label}</span>
        {sub && <span className="row-sub">{sub}</span>}
      </span>
      <span className="switch" aria-hidden="true">
        <span className="knob" />
      </span>
    </button>
  );
}

interface ChoiceProps<T extends string> {
  label: string;
  value: T;
  options: { id: T; label: string; sub?: string }[];
  onChange: (v: T) => void;
  className?: string;
}

/** A radio group drawn as a row of buttons; arrow keys move the choice. */
export function Choice<T extends string>({ label, value, options, onChange, className = "" }: ChoiceProps<T>) {
  const id = useId();
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const onKey = (e: React.KeyboardEvent, i: number) => {
    const delta = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!delta) return;
    e.preventDefault();
    e.stopPropagation();
    const n = (i + delta + options.length) % options.length;
    onChange(options[n].id);
    refs.current[n]?.focus();
  };
  return (
    <div className={`choice ${className}`} role="radiogroup" aria-labelledby={id}>
      <span id={id} className="choice-label">
        {label}
      </span>
      <div className="choice-options">
        {options.map((o, i) => (
          <button
            key={o.id}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={value === o.id}
            tabIndex={value === o.id ? 0 : -1}
            className={`choice-option${value === o.id ? " on" : ""} opt-${o.id}`}
            onClick={() => onChange(o.id)}
            onKeyDown={(e) => onKey(e, i)}
          >
            <span className="choice-name">{o.label}</span>
            {o.sub && <span className="choice-sub">{o.sub}</span>}
          </button>
        ))}
      </div>
    </div>
  );
}

/** A small static stone used in the HUD and menus. */
export function StoneIcon({ color, size = 18, hollow = false }: { color: "lava" | "ash"; size?: number; hollow?: boolean }) {
  return (
    <svg className={`stone-icon ${color}${hollow ? " hollow" : ""}`} width={size} height={size} viewBox="-10 -10 20 20" aria-hidden="true">
      <ellipse className="body-side" cx="0" cy="1.6" rx="8" ry="6.6" />
      <ellipse className="body" cx="0" cy="0" rx="8" ry="6.6" />
    </svg>
  );
}
