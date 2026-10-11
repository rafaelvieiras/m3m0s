import { describe, expect, it } from "vitest";
import { barWidths, domainInstance, formatDuration, formatPercent, percent, progressPercent, tierLabel } from "./insights";
import { hygieneResponseSchema, insightsOverviewSchema } from "./schemas";

describe("insights helpers", () => {
  it("computes percentages safely", () => {
    expect(percent(1, 4)).toBe(25);
    expect(percent(1, 0)).toBe(0);
    expect(formatPercent(1, 1000)).toBe("0.1%");
    expect(formatPercent(1, 100000)).toBe("<0.1%");
    expect(formatPercent(0, 10)).toBe("0.0%");
    expect(formatPercent(966, 1000)).toBe("97%");
  });

  it("scales bars against the largest bucket and keeps small ones visible", () => {
    const w = barWidths([
      { key: "a", count: 100 },
      { key: "b", count: 1 },
      { key: "c", count: 0 },
    ]);
    expect(w[0]).toBe(100);
    expect(w[1]).toBe(2);
    expect(w[2]).toBe(0);
    expect(barWidths([{ key: "a", count: 0 }])).toEqual([0]);
    expect(barWidths([])).toEqual([]);
  });

  it("formats durations and progress", () => {
    expect(formatDuration(450)).toBe("450ms");
    expect(formatDuration(15700)).toBe("16s");
    expect(formatDuration(5200)).toBe("5.2s");
    expect(formatDuration(125000)).toBe("2min 5s");
    expect(progressPercent(5, 20)).toBe(25);
    expect(progressPercent(5, 0)).toBe(0);
    expect(progressPercent(30, 20)).toBe(100);
  });

  it("labels tiers and domains", () => {
    expect(tierLabel("conservative")).toBe("Conservative");
    expect(tierLabel("review")).toBe("For review");
    expect(domainInstance("codigo:m3m0s")).toBe("m3m0s");
    expect(domainInstance("plain")).toBe("plain");
  });
});

describe("insights schemas", () => {
  const forgetting = {
    eligible: 10, exempt: 1, cold_now: 2, soft_deleted: 3, hard_delete_due: 1,
    threshold: 0.15, age_half_life_days: 30, grace_days: 30, retention_histogram: null,
  };

  it("accepts null lists from the Go server", () => {
    const parsed = insightsOverviewSchema.parse({
      generated_at: "2026-10-08T12:00:00Z",
      totals: { total: 4, active: 3, soft_deleted: 1, with_valid_until: 0, expired: 0, pinned: 0, extracted: 3, raw: 0, never_accessed: 3, never_accessed_pct: 100 },
      text: { avg_chars: 10, median_chars: 9, p90_chars: 20, max_chars: 30 },
      soft_deleted_by_day: null, by_category: null, by_source: [], by_age: [], domain_count: 0, top_domains: null,
      vectors_indexed: 0, forgetting,
    });
    expect(parsed.top_domains).toEqual([]);
    expect(parsed.forgetting.retention_histogram).toEqual([]);
    expect(parsed.by_kind).toBeUndefined(); // servers that predate the nature tag
  });

  it("parses the nature distribution", () => {
    const parsed = insightsOverviewSchema.parse({
      generated_at: "2026-10-08T12:00:00Z",
      totals: { total: 0, active: 0, soft_deleted: 0, with_valid_until: 0, expired: 0, pinned: 0, extracted: 0, raw: 0, never_accessed: 0, never_accessed_pct: 0 },
      text: { avg_chars: 0, median_chars: 0, p90_chars: 0, max_chars: 0 },
      soft_deleted_by_day: [], by_category: [], by_source: [], by_age: [], domain_count: 0, top_domains: [],
      vectors_indexed: 0, forgetting,
      by_kind: [{ key: "decision", count: 2 }, { key: "other", count: 5 }],
      by_kind_source: null,
    });
    expect(parsed.by_kind?.[0]).toEqual({ key: "decision", count: 2 });
    expect(parsed.by_kind_source).toEqual([]);
  });

  it("parses the ingestion filter section and tolerates its absence", () => {
    const base = {
      generated_at: "2026-10-08T12:00:00Z",
      totals: { total: 0, active: 0, soft_deleted: 0, with_valid_until: 0, expired: 0, pinned: 0, extracted: 0, raw: 0, never_accessed: 0, never_accessed_pct: 0 },
      text: { avg_chars: 0, median_chars: 0, p90_chars: 0, max_chars: 0 },
      soft_deleted_by_day: [], by_category: [], by_source: [], by_age: [], domain_count: 0, top_domains: [],
      vectors_indexed: 0, forgetting,
    };
    expect(insightsOverviewSchema.parse(base).filter).toBeUndefined();
    const parsed = insightsOverviewSchema.parse({
      ...base,
      filter: { mode: "default", rules_version: 1, rules: [{ id: "diff-trivia-fact", scope: "fact", action: "tag", seen: 5, dropped: 0, tagged: 2 }] },
    });
    expect(parsed.filter?.rules[0].tagged).toBe(2);
    expect(insightsOverviewSchema.parse({ ...base, filter: { mode: "off", rules_version: 1, rules: null } }).filter?.rules).toEqual([]);
  });

  it("parses the never-analysed state", () => {
    const parsed = hygieneResponseSchema.parse({ status: { running: false, done: 0, total: 0 }, report: null });
    expect(parsed.report).toBeNull();
    expect(parsed.status.running).toBe(false);
  });
});
