import { createContext, ReactNode, useContext, useMemo } from "react";
import { en } from "./en";
import { ru } from "./ru";
import { uz } from "./uz";

export type Lang = "en" | "ru" | "uz";
export const LANGS: { id: Lang; label: string; short: string }[] = [
  { id: "en", label: "English", short: "EN" },
  { id: "ru", label: "Русский", short: "RU" },
  { id: "uz", label: "Oʻzbekcha", short: "UZ" },
];

type Plural = { zero?: string; one?: string; two?: string; few?: string; many?: string; other: string };
type En = typeof en;
/** Every language must define every English key, with the same shape. */
export type Dict = { [K in keyof En]: En[K] extends string ? string : Plural };
export type Key = keyof En;
export type Params = Record<string, string | number>;

const DICTS: Record<Lang, Dict> = { en, ru, uz };
const LOCALE: Record<Lang, string> = { en: "en-US", ru: "ru-RU", uz: "uz-Latn-UZ" };

/** Map a stored setting ("" = follow the device) to a concrete language. */
export function resolveLang(setting: string | undefined): Lang {
  if (setting === "en" || setting === "ru" || setting === "uz") return setting;
  const prefs = typeof navigator !== "undefined" ? [...(navigator.languages ?? []), navigator.language] : [];
  for (const p of prefs) {
    const l = (p || "").toLowerCase();
    if (l.startsWith("uz")) return "uz";
    if (l.startsWith("ru")) return "ru";
    if (l.startsWith("en")) return "en";
  }
  return "en";
}

export interface I18n {
  lang: Lang;
  locale: string;
  t: (key: Key, params?: Params) => string;
  /** Pluralised lookup; `n` is also available as {n}. */
  tn: (key: Key, n: number, params?: Params) => string;
  /** Lookup for keys built at runtime (ids and codes from Go). */
  td: (key: string, params?: Params, fallback?: string) => string;
  num: (n: number) => string;
}

export function makeI18n(lang: Lang): I18n {
  const dict = DICTS[lang] as Record<string, string | Plural>;
  const base = en as Record<string, string | Plural>;
  const locale = LOCALE[lang];
  let nf: Intl.NumberFormat;
  let plural: Intl.PluralRules;
  try {
    nf = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });
    plural = new Intl.PluralRules(locale);
  } catch {
    nf = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });
    plural = new Intl.PluralRules("en-US");
  }
  const fill = (s: string, params?: Params) =>
    params
      ? s.replace(/\{(\w+)\}/g, (m, k: string) => {
          const v = params[k];
          if (v === undefined) return m;
          return typeof v === "number" ? nf.format(v) : v;
        })
      : s;
  const asText = (v: string | Plural | undefined, n?: number): string | undefined => {
    if (v === undefined) return undefined;
    if (typeof v === "string") return v;
    const cat = plural.select(n ?? 0) as keyof Plural;
    return v[cat] ?? v.other;
  };
  const raw = (key: string) => dict[key] ?? base[key];
  return {
    lang,
    locale,
    t: (key, params) => fill(asText(raw(key)) ?? key, params),
    tn: (key, n, params) => fill(asText(raw(key), n) ?? key, { n, ...params }),
    td: (key, params, fallback) => {
      const v = asText(raw(key));
      return v === undefined ? (fallback ?? key) : fill(v, params);
    },
    num: (n) => nf.format(n),
  };
}

const Ctx = createContext<I18n>(makeI18n("en"));

export function I18nProvider({ value, children }: { value: I18n; children: ReactNode }) {
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n() {
  return useContext(Ctx);
}

export function useMakeI18n(lang: Lang) {
  return useMemo(() => makeI18n(lang), [lang]);
}

/** Translate an error from the Go service (its code arrives in `code`). */
export function errorText(i18n: I18n, e: unknown): string {
  const code = (e as { code?: string })?.code;
  if (code && code !== "generic") return i18n.td(`error.${code}`, undefined, i18n.t("error.generic"));
  return i18n.t("error.generic");
}
