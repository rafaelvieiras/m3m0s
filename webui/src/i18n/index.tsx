// Tiny i18n runtime: flat message tables per locale (English is the source of
// truth and types the keys), `{name}` interpolation and plural suffixes
// (`key.one` / `key.other`, picked with Intl.PluralRules when `count` is given).
// The chosen language lives in localStorage; the default is English.
import { useSyncExternalStore } from "react";
import { en } from "./locales/en";
import { pt } from "./locales/pt";
import { es } from "./locales/es";

export const LOCALES = ["en", "pt", "es"] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_LABELS: Record<Locale, string> = { en: "English", pt: "Português", es: "Español" };
/** BCP 47 tags for Intl / toLocale*String. */
export const LOCALE_TAGS: Record<Locale, string> = { en: "en-US", pt: "pt-BR", es: "es-ES" };

export type MessageKey = keyof typeof en;
type Plural<K> = K extends `${infer B}.${"one" | "other"}` ? B : never;
/** Keys accepted by t(): full keys plus bare plural bases. */
export type TKey = MessageKey | Plural<MessageKey>;
export type Vars = Record<string, string | number>;

const tables: Record<Locale, Record<string, string>> = { en, pt, es };
const STORAGE_KEY = "m3m0s.lang";

function isLocale(v: unknown): v is Locale {
  return typeof v === "string" && (LOCALES as readonly string[]).includes(v);
}

function readStored(): Locale {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (isLocale(v)) return v;
  } catch {
    // storage blocked: fall back to the default
  }
  return DEFAULT_LOCALE;
}

let current: Locale = readStored();
const listeners = new Set<() => void>();

function applyHtmlLang(l: Locale) {
  if (typeof document !== "undefined") document.documentElement.lang = LOCALE_TAGS[l];
}
applyHtmlLang(current);

export function getLocale(): Locale {
  return current;
}

export function setLocale(l: Locale) {
  if (l === current) return;
  current = l;
  try {
    localStorage.setItem(STORAGE_KEY, l);
  } catch {
    // ignore
  }
  applyHtmlLang(l);
  listeners.forEach((fn) => fn());
}

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** Translate outside React (toasts built in hooks, helpers). Prefer useT() in components. */
export function translate(key: TKey, vars?: Vars): string {
  const table = tables[current];
  let k: string = key;
  if (vars && typeof vars.count === "number") {
    const cat = new Intl.PluralRules(LOCALE_TAGS[current]).select(vars.count);
    const specific = `${key}.${cat === "one" ? "one" : "other"}`;
    if (specific in en) k = specific;
  }
  let msg = table[k] ?? en[k as MessageKey] ?? k;
  if (vars) msg = msg.replace(/\{(\w+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
  return msg;
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribe, getLocale, getLocale);
}

/** Hook returning t() bound to the current locale; re-renders on language change. */
export function useT() {
  const locale = useLocale();
  return {
    t: translate,
    locale,
    tag: LOCALE_TAGS[locale],
  };
}
