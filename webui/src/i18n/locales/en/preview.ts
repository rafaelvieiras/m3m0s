// Keys are "preview.*": context injection preview page and the Add-screen extraction preview.
export const preview = {
  "preview.mode.hook": "Automatic (Claude Code)",
  "preview.mode.mcp": "On request (MCP)",
  "preview.mode.manual": "Copy and paste",
  "preview.copy": "Copy",
  "preview.copied": "Copied",
  "preview.title": "What the agent will see",
  "preview.description":
    "The memories your AI agent receives for a topic, exactly as they would be handed over today.",
  "preview.domain": "Topic",
  "preview.domainPlaceholder": "code:my-app (empty = all topics)",
  "preview.apply": "Apply",
  "preview.loadFailed": "Failed to load: {message}",
  "preview.allDomains": "all topics",
  "preview.fetched": "{count} memories found",
  "preview.used": "{count} used in the result",
  "preview.empty.title": "The agent would receive nothing",
  "preview.empty.description":
    "There are no memories for this topic and mode yet. Add a few, or pick another topic.",
  "preview.extraction.on": "Fact extraction is on: {count} fact(s) were created from your text.",
  "preview.extraction.off": "Fact extraction is off, so the text was saved as written.",
  "preview.extraction.original": "Original text",
  "preview.extraction.fact": "Fact {n}",
  "preview.extraction.created": "Memory created",
  "preview.extraction.deduped": "deduped (merged with existing)",
  "preview.extraction.new": "new",
  "preview.extraction.addAnother": "Add another memory",
} as const;
