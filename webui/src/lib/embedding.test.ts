import { describe, expect, it } from "vitest";
import { buildPayload, formatCost, formatEta, parseDimensions, progressPercent, type EmbeddingFormState } from "./embedding";
import { embeddingConfigSchema, reembedEstimateSchema, reembedProgressSchema } from "./schemas";

const base: EmbeddingFormState = {
  enabled: true,
  baseUrl: " ",
  model: " text-embedding-3-small ",
  dimensions: "",
  apiKey: "",
  clearKey: false,
};

describe("buildPayload", () => {
  it("omits api_key when the field is empty (keeps the saved key)", () => {
    const p = buildPayload(base);
    expect(p).toEqual({ enabled: true, base_url: "", model: "text-embedding-3-small", dimensions: 0 });
    expect("api_key" in (p as object)).toBe(false);
  });

  it("sends the typed key, trimmed", () => {
    expect(buildPayload({ ...base, apiKey: " sk-x " })).toMatchObject({ api_key: "sk-x" });
  });

  it("sends an empty api_key to clear the saved one", () => {
    expect(buildPayload({ ...base, clearKey: true, apiKey: "ignored" })).toMatchObject({ api_key: "" });
  });

  it("adds confirm_reembed only when asked", () => {
    expect(buildPayload(base)).not.toHaveProperty("confirm_reembed");
    expect(buildPayload(base, true)).toMatchObject({ confirm_reembed: true });
  });

  it("validates model and dimensions", () => {
    expect(buildPayload({ ...base, model: " " })).toHaveProperty("error");
    expect(buildPayload({ ...base, dimensions: "12a" })).toHaveProperty("error");
    expect(buildPayload({ ...base, dimensions: "512" })).toMatchObject({ dimensions: 512 });
  });
});

describe("helpers", () => {
  it("parseDimensions", () => {
    expect(parseDimensions("").value).toBe(0);
    expect(parseDimensions("-1").error).toBeTruthy();
  });

  it("formatEta", () => {
    expect(formatEta(0)).toBe("unknown");
    expect(formatEta(45)).toBe("45s");
    expect(formatEta(90)).toBe("1min 30s");
    expect(formatEta(7500)).toBe("2h 5min");
  });

  it("progressPercent clamps", () => {
    expect(progressPercent(0, 0)).toBe(0);
    expect(progressPercent(50, 200)).toBe(25);
    expect(progressPercent(300, 200)).toBe(100);
  });

  it("formatCost never invents a price", () => {
    expect(formatCost({ estimated_cost_usd: null, price_per_million_tokens_usd: null, prices_as_of: "x" })).toMatch(/unknown/);
    expect(formatCost({ estimated_cost_usd: 0.206, price_per_million_tokens_usd: 0.02, prices_as_of: "2026-10-08" })).toContain("~US$ 0.21");
  });
});

describe("embedding schemas (mirror docs/api-contract.md)", () => {
  it("parses the documented GET body, which has no key field", () => {
    const body = {
      enabled: true, base_url: "", model: "text-embedding-3-small", dimensions: 0, vector_dims: 1536,
      source: "environment", has_api_key: true, key_source: "environment", active: true,
      vectors: { total: 10, current: 10, stale: 0 },
      reembed: { running: false, total: 0, done: 0, errors: 0, eta_seconds: 0 },
      updated_at: "2026-10-08T15:00:00Z",
    };
    const r = embeddingConfigSchema.parse(body);
    expect(r.has_api_key).toBe(true);
    expect(Object.keys(r)).not.toContain("api_key");
  });

  it("parses a null-priced estimate and running progress", () => {
    expect(
      reembedEstimateSchema.parse({
        model: "m", dimensions: 0, pending: 1, characters: 4, estimated_tokens: 1, approximate: true,
        price_per_million_tokens_usd: null, estimated_cost_usd: null, prices_as_of: "2026-10-08",
      }).estimated_cost_usd,
    ).toBeNull();
    expect(
      reembedProgressSchema.parse({
        running: true, total: 5, done: 2, errors: 0, eta_seconds: 3, started_at: "2026-10-08T15:00:00Z",
      }).running,
    ).toBe(true);
  });
});
