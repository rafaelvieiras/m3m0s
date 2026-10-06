import { describe, expect, it } from "vitest";
import {
  addFormSchema,
  domainRegex,
  entrySchema,
  errorBodySchema,
  statsResponseSchema,
} from "./schemas";

describe("domainRegex (mirrors internal/schema.ValidateDomain)", () => {
  it("accepts valid domains", () => {
    for (const d of ["code:m3m0s", "writing:ai-practice-book", "personal:social_media", "client:acme.co"]) {
      expect(domainRegex.test(d)).toBe(true);
    }
  });

  it("rejects invalid domains", () => {
    for (const d of ["code", "Code:m3m0s", "code:", ":m3m0s", "code:M3m0s", ""]) {
      expect(domainRegex.test(d)).toBe(false);
    }
  });
});

describe("addFormSchema", () => {
  it("accepts a valid minimal payload", () => {
    const result = addFormSchema.safeParse({
      content: "any content",
      domain: "code:m3m0s",
      tags: [],
      metadata: {},
    });
    expect(result.success).toBe(true);
  });

  it("rejects empty content", () => {
    const result = addFormSchema.safeParse({
      content: "",
      domain: "code:m3m0s",
      tags: [],
      metadata: {},
    });
    expect(result.success).toBe(false);
  });

  it("rejects a domain outside the category:instance format", () => {
    const result = addFormSchema.safeParse({
      content: "content",
      domain: "code-m3m0s",
      tags: [],
      metadata: {},
    });
    expect(result.success).toBe(false);
  });
});

describe("entrySchema", () => {
  it("accepts a MemoryEntry matching docs/api-contract.md", () => {
    const result = entrySchema.safeParse({
      id: "mem_8f2a1c9d",
      content: "plain text",
      domain: "code:m3m0s",
      domainCategory: "code",
      source: "claude-code",
      agent: null,
      tags: ["chapter-3"],
      metadata: { conversationId: "conv_8f2a" },
      createdAt: "2026-07-15T19:00:00Z",
      updatedAt: "2026-07-15T19:00:00Z",
    });
    expect(result.success).toBe(true);
  });

  it("rejects metadata with a non-string value (chromem-go only accepts map[string]string)", () => {
    const result = entrySchema.safeParse({
      id: "mem_1",
      content: "x",
      domain: "code:m3m0s",
      domainCategory: "code",
      source: "manual",
      agent: null,
      tags: [],
      metadata: { nested: { a: 1 } },
      createdAt: "2026-07-15T19:00:00Z",
      updatedAt: "2026-07-15T19:00:00Z",
    });
    expect(result.success).toBe(false);
  });
});

describe("statsResponseSchema", () => {
  const telemetry = {
    apiKeyCount: 1,
    memoryUsageMB: 42.5,
    databaseSizeMB: 3.1,
    uptimeSeconds: 120,
    extractionTokensUsed: 0,
    tokensSavedByCompaction: 0,
  };

  it("accepts extraction disabled with null model", () => {
    const result = statsResponseSchema.safeParse({
      count: 10,
      countByDomainCategory: { code: 10 },
      backend: "chromem-go + OpenAI embeddings",
      model: "text-embedding-3-small",
      extraction: { enabled: false, model: null },
      telemetry,
    });
    expect(result.success).toBe(true);
  });

  it("rejects a response without the telemetry field", () => {
    const result = statsResponseSchema.safeParse({
      count: 10,
      countByDomainCategory: { code: 10 },
      backend: "chromem-go + OpenAI embeddings",
      model: "text-embedding-3-small",
      extraction: { enabled: false, model: null },
    });
    expect(result.success).toBe(false);
  });
});

describe("errorBodySchema", () => {
  it("parses the common error format of the 3 surfaces", () => {
    const result = errorBodySchema.safeParse({
      error: { code: "invalid_domain", message: "domain must match ..." },
    });
    expect(result.success).toBe(true);
  });
});

describe("entrySchema provenance", () => {
  const base = {
    id: "mem_1",
    content: "c",
    domain: "code:x",
    domainCategory: "code",
    source: "claude-code",
    agent: null,
    tags: [],
    metadata: {},
    createdAt: "2026-07-15T19:00:00Z",
    updatedAt: "2026-07-15T19:00:00Z",
  };

  it("accepts model/project as null, string or absent (pre-F2 servers)", () => {
    expect(entrySchema.safeParse({ ...base, model: null, project: null }).success).toBe(true);
    expect(entrySchema.safeParse({ ...base, model: "gpt-5", project: "m3m0s" }).success).toBe(true);
    expect(entrySchema.safeParse(base).success).toBe(true);
  });
});
