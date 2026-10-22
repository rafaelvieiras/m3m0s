// Pure helpers for the memory-health page (D37), kept out of the component so
// they can be unit-tested.
import { getLocale, LOCALE_TAGS, translate, type TKey } from "@/i18n";
import type { CountItem } from "./schemas";

export function formatCount(n: number, tag: string = LOCALE_TAGS[getLocale()]): string {
  return n.toLocaleString(tag);
}

// percent returns part/total as a percentage, 0 when total is not positive.
export function percent(part: number, total: number): number {
  if (total <= 0) return 0;
  return (part / total) * 100;
}

export function formatPercent(part: number, total: number): string {
  const p = percent(part, total);
  if (p > 0 && p < 0.1) return "<0.1%";
  return `${p.toFixed(p >= 10 ? 0 : 1)}%`;
}

// barWidths scales counts to 0-100 against the largest one, keeping a sliver
// visible for non-zero values so small buckets do not disappear.
export function barWidths(items: CountItem[]): number[] {
  const max = Math.max(0, ...items.map((i) => i.count));
  if (max === 0) return items.map(() => 0);
  return items.map((i) => (i.count === 0 ? 0 : Math.max(2, (i.count / max) * 100)));
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(s < 10 ? 1 : 0)}s`;
  return `${Math.floor(s / 60)}min ${Math.round(s % 60)}s`;
}

// progressPercent clamps done/total to 0-100; 0 when the total is unknown.
export function progressPercent(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.floor((done / total) * 100)));
}

export function tierLabel(tier: string, t: (key: TKey) => string = translate): string {
  return tier === "conservative" ? t("insights.tier.conservative") : t("insights.tier.review");
}

// domainInstance shows the part of a domain after the category colon.
export function domainInstance(domain: string): string {
  const i = domain.indexOf(":");
  return i < 0 ? domain : domain.slice(i + 1);
}

// horizonLabel names a shadow horizon: 0 is "now", others "+30 d".
export function horizonLabel(days: number, t: (key: TKey) => string = translate): string {
  return days <= 0 ? t("insights.horizon.now") : `+${days} ${t("insights.unit.days")}`;
}

// shadowDelta describes how many fewer (or more) memories the per-kind model
// forgets than the legacy one at a horizon, e.g. "-120" or "+3" or "same".
export function shadowDelta(legacy: number, perKind: number, t: (key: TKey) => string = translate): string {
  const d = perKind - legacy;
  if (d === 0) return t("insights.delta.same");
  return `${d > 0 ? "+" : "-"}${formatCount(Math.abs(d))}`;
}
