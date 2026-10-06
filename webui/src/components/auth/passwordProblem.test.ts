import { describe, expect, it } from "vitest";
import { passwordProblem } from "./PasswordFields";
import { usernameRegex } from "@/lib/schemas";

describe("passwordProblem (mirrors internal/auth.ValidatePassword)", () => {
  it("requires the minimum length first", () => {
    expect(passwordProblem("short", "short")).toMatch(/at least 10/);
  });
  it("requires both fields to match", () => {
    expect(passwordProblem("a-long-enough-pass", "a-long-enough-pasx")).toMatch(/do not match/);
  });
  it("accepts a long matching pair, with no composition rules", () => {
    expect(passwordProblem("aaaaaaaaaaaa", "aaaaaaaaaaaa")).toBeNull();
  });
  it("rejects more than 128 characters", () => {
    const long = "x".repeat(129);
    expect(passwordProblem(long, long)).toMatch(/at most 128/);
  });
});

describe("usernameRegex (mirrors internal/auth.ValidateUsername)", () => {
  it("accepts slugs and rejects the rest", () => {
    for (const ok of ["abc", "rafael", "a.b-c_d9"]) expect(usernameRegex.test(ok)).toBe(true);
    for (const bad of ["ab", "Has Space", "-lead", "a".repeat(33)]) expect(usernameRegex.test(bad)).toBe(false);
  });
});
