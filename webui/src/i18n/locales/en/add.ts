// Keys are "add.*". English is the source of truth; pt/es must define the same keys.
export const add = {
  "add.added": "Memory added",
  "add.title": "Add memory",
  "add.subtitle":
    "POST /memories — if fact extraction is enabled on the server, the text becomes N atomic facts (Phase 4/D12).",
  "add.newEntry": "New entry",
  "add.content": "Content",
  "add.contentRequired": "content is required",
  "add.contentPlaceholder": "The text/fact to remember...",
  "add.domainHelp": "category:instance — e.g. writing:book-x",
  "add.sourcePlaceholder": "manual (default)",
  "add.agent": "agent (optional)",
  "add.tags": "tags (comma-separated)",
  "add.metadata": "metadata (JSON, optional)",
  "add.metadataHelp": "Values must be strings (same limitation as chromem-go's map[string]string).",
  "add.metadataJson": "metadata must be valid JSON",
  "add.metadataFlat": "metadata must be a flat JSON object of string:string (no nesting)",
  "add.submit": "Add memory",
} as const;
