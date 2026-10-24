// Pure helpers for the memory-health page (D37), kept out of the component so
// they can be unit-tested.
import { getLocale, LOCALE_TAGS, translate, type TKey } from "@/i18n";
import { formatDecimal, formatPercentValue } from "./format";
import type { CountItem, HygieneReport, InsightsOverview } from "./schemas";

export function formatCount(n: number, tag: string = LOCALE_TAGS[getLocale()]): string {
  return n.toLocaleString(tag);
}

// percent returns part/total as a percentage, 0 when total is not positive.
export function percent(part: number, total: number): number {
  if (total <= 0) return 0;
  return (part / total) * 100;
}

export function formatPercent(part: number, total: number, tag: string = LOCALE_TAGS[getLocale()]): string {
  const p = percent(part, total);
  if (p > 0 && p < 0.1) return `<${formatDecimal(0.1, tag, 1)}%`;
  return formatPercentValue(p, tag, p >= 10 ? 0 : 1);
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

// ── Memory health summary (Simple mode) ──

export type HealthIssueId = "noEmbeddings" | "expired" | "duplicates" | "forgetting" | "neverRecalled" | "noAnalysis";

export interface HealthIssue {
  id: HealthIssueId;
  /** Number the message mentions (0 when the message has none). */
  count: number;
  /** Where the fix lives: an in-page anchor or an app route. */
  to: { anchor: string } | { route: "/embedding" };
}

export interface MemoryHealth {
  status: "ok" | "attention";
  /** At most MAX_HEALTH_ISSUES, most important first. */
  issues: HealthIssue[];
}

export const MAX_HEALTH_ISSUES = 3;
// Below this many memories, "never recalled" just means a young collection.
const NEVER_RECALLED_MIN_ACTIVE = 20;
const NEVER_RECALLED_PCT = 70;

// memoryHealth turns the overview (and the last hygiene report, when there is
// one; null if never run, undefined if unknown) into a verdict plus a few things worth doing. It only reads data the
// Insights page already has.
export function memoryHealth(ov: InsightsOverview, report: HygieneReport | null | undefined): MemoryHealth {
  const found: HealthIssue[] = [];
  const t = ov.totals;
  if (t.active > 0 && ov.vectors_indexed === 0) {
    found.push({ id: "noEmbeddings", count: 0, to: { route: "/embedding" } });
  }
  if (t.expired > 0) found.push({ id: "expired", count: t.expired, to: { anchor: "overview" } });
  if (report && report.exact_duplicates.redundant > 0) {
    found.push({ id: "duplicates", count: report.exact_duplicates.redundant, to: { anchor: "hygiene" } });
  }
  if (ov.forgetting.cold_now > 0) {
    found.push({ id: "forgetting", count: ov.forgetting.cold_now, to: { anchor: "forgetting" } });
  }
  if (t.active >= NEVER_RECALLED_MIN_ACTIVE && t.never_accessed_pct >= NEVER_RECALLED_PCT) {
    found.push({ id: "neverRecalled", count: t.never_accessed, to: { anchor: "overview" } });
  }
  // null = never analysed; undefined = unknown (not loaded), so say nothing.
  if (report === null && t.active > 0) found.push({ id: "noAnalysis", count: 0, to: { anchor: "hygiene" } });
  return { status: found.length === 0 ? "ok" : "attention", issues: found.slice(0, MAX_HEALTH_ISSUES) };
}
