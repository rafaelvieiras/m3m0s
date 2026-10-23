import { describe, expect, it } from "vitest";
import { en } from "./locales/en";
import { es } from "./locales/es";
import { pt } from "./locales/pt";

const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe("locale parity", () => {
  const enKeys = Object.keys(en).sort();

  for (const [name, table] of [
    ["pt", pt],
    ["es", es],
  ] as const) {
    it(`${name} defines exactly the English keys`, () => {
      expect(Object.keys(table).sort()).toEqual(enKeys);
    });

    it(`${name} keeps the same {placeholders} as English`, () => {
      const bad = enKeys.filter(
        (k) =>
          JSON.stringify(placeholders((en as Record<string, string>)[k])) !==
          JSON.stringify(placeholders((table as Record<string, string>)[k])),
      );
      expect(bad).toEqual([]);
    });

    it(`${name} has no empty values`, () => {
      const empty = Object.entries(table).filter(([, v]) => (v as string).trim() === "");
      expect(empty).toEqual([]);
    });
  }
});
