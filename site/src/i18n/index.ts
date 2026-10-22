import { en } from "./en";
import { pt } from "./pt";
import { es } from "./es";

export const LOCALES = ["en", "pt", "es"] as const;
export type Lang = (typeof LOCALES)[number];
export const DEFAULT_LANG: Lang = "en";

export const LANG_META: Record<Lang, { label: string; html: string; og: string }> = {
  en: { label: "English", html: "en", og: "en_US" },
  pt: { label: "Português", html: "pt-BR", og: "pt_BR" },
  es: { label: "Español", html: "es", og: "es_ES" },
};

const dicts = { en, pt, es };
export type Dict = typeof en;

export function t(lang: Lang): Dict {
  return dicts[lang];
}

export function isLang(value: string | undefined): value is Lang {
  return !!value && (LOCALES as readonly string[]).includes(value);
}

/** Prefix a site path ("/guide/cli/") with the locale; English stays unprefixed. */
export function localePath(lang: Lang, path: string): string {
  if (lang === DEFAULT_LANG) return path;
  return `/${lang}${path === "/" ? "/" : path}`;
}

/** Split "/pt/guide/cli/" into its locale and the locale-less path. */
export function splitPath(pathname: string): { lang: Lang; path: string } {
  const m = pathname.match(/^\/(pt|es)(\/.*)?$/);
  if (m) return { lang: m[1] as Lang, path: m[2] || "/" };
  return { lang: DEFAULT_LANG, path: pathname };
}

export const langPaths = LOCALES.filter((l) => l !== DEFAULT_LANG).map((lang) => ({ params: { lang } }));
