// Pure helpers for the memory-health page (D37), kept out of the component so
// they can be unit-tested.
import type { CountItem } from "./schemas";

export function formatCount(n: number): string {
  return n.toLocaleString("en-US");
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

export function tierLabel(tier: string): string {
  return tier === "conservative" ? "Conservative" : "For review";
}

// domainInstance shows the part of a domain after the category colon.
export function domainInstance(domain: string): string {
  const i = domain.indexOf(":");
  return i < 0 ? domain : domain.slice(i + 1);
}
