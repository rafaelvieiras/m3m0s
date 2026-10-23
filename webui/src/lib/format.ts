// Locale-aware formatting helpers shared by the pages. Everything here takes
// the BCP 47 tag from useT() so dates, decimals and units follow the language
// the person picked, not the browser default.
import { translate, type TKey } from "@/i18n";

export function formatDateTime(iso: string, tag: string): string {
  return new Intl.DateTimeFormat(tag, { dateStyle: "medium", timeStyle: "short" }).format(new Date(iso));
}

export function formatDate(iso: string, tag: string): string {
  return new Intl.DateTimeFormat(tag, { dateStyle: "medium" }).format(new Date(iso));
}

/** Formats a decimal with a fixed number of fraction digits, e.g. 86.4 -> "86,4" in pt. */
export function formatDecimal(n: number, tag: string, digits = 1): string {
  return new Intl.NumberFormat(tag, { minimumFractionDigits: digits, maximumFractionDigits: digits }).format(n);
}

export function formatPercentValue(p: number, tag: string, digits = 1): string {
  return `${formatDecimal(p, tag, digits)}%`;
}

const INTERVAL_UNITS = [
  { unit: "hour", seconds: 3600 },
  { unit: "minute", seconds: 60 },
  { unit: "second", seconds: 1 },
] as const;

/** Formats a whole number of seconds as "12 hours", "5 minutes" or "90 seconds" in the given locale. */
export function formatInterval(totalSeconds: number, tag: string): string {
  const best =
    INTERVAL_UNITS.find((u) => totalSeconds >= u.seconds && totalSeconds % u.seconds === 0) ??
    INTERVAL_UNITS[INTERVAL_UNITS.length - 1];
  return new Intl.NumberFormat(tag, { style: "unit", unit: best.unit, unitDisplay: "long" }).format(
    totalSeconds / best.seconds,
  );
}

const AGE_BUCKET_KEYS: Record<string, TKey> = {
  "0-7 days": "insights.age.0-7",
  "8-30 days": "insights.age.8-30",
  "31-60 days": "insights.age.31-60",
  "61-90 days": "insights.age.61-90",
  "91-180 days": "insights.age.91-180",
  "181-365 days": "insights.age.181-365",
  "over 1 year": "insights.age.over1y",
};

/** The server names age buckets in English; show them in the current language. */
export function ageBucketLabel(bucket: string): string {
  const key = AGE_BUCKET_KEYS[bucket];
  return key ? translate(key) : bucket;
}
