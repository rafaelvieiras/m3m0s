import { describe, expect, it } from "vitest";
import { ageBucketLabel, formatDecimal, formatInterval, formatPercentValue } from "./format";
import { isKind, kindLabel } from "./kinds";
import { snippet, stripMarkdown } from "./text";

describe("locale-aware formatting", () => {
  it("uses the locale's decimal separator", () => {
    expect(formatPercentValue(86.4, "en-US")).toBe("86.4%");
    expect(formatPercentValue(86.4, "pt-BR")).toBe("86,4%");
    expect(formatDecimal(0.15, "es-ES", 2)).toBe("0,15");
  });

  it("formats intervals with the largest exact unit", () => {
    expect(formatInterval(43200, "en-US")).toBe("12 hours");
    expect(formatInterval(300, "en-US")).toBe("5 minutes");
    expect(formatInterval(90, "en-US")).toBe("90 seconds");
    expect(formatInterval(43200, "pt-BR")).toBe("12 horas");
  });

  it("translates the server's age buckets and keeps unknown ones", () => {
    expect(ageBucketLabel("0-7 days")).toBe("0-7 days");
    expect(ageBucketLabel("over 1 year")).toBe("Over 1 year");
    expect(ageBucketLabel("something else")).toBe("something else");
  });
});

describe("kinds", () => {
  it("recognizes the closed set and labels it", () => {
    expect(isKind("decision")).toBe(true);
    expect(isKind("nope")).toBe(false);
    expect(kindLabel("gotcha")).toBe("Pitfall");
    expect(kindLabel("custom")).toBe("custom");
  });
});

describe("markdown stripping", () => {
  it("turns markdown into a plain snippet", () => {
    expect(stripMarkdown("# D1 — Title\n\n**Status:** accepted, see [docs](http://x.y) and `code`")).toBe(
      "D1 — Title Status: accepted, see docs and code",
    );
    expect(snippet("a ".repeat(200), 10)).toBe("a a a a a…");
  });
});
