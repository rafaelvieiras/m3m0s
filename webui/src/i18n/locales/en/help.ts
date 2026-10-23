// Keys are "help.*". English is the source of truth; pt/es must define the same keys.
export const help = {
  "help.about": "About this page",
  "help.more": "What is this?",
  "help.close": "Close",
  "help.term": "Explain: {term}",
  "help.term.memory.name": "Memory",
  "help.term.memory.body":
    "A short note M3m0s keeps for you and your AI agents: a decision, a rule, a preference or anything worth remembering.",
  "help.term.kind.name": "Type of memory",
  "help.term.kind.body":
    "What the note is: a decision, a rule, a preference, a pitfall, a how-to, a status or something else. It helps M3m0s decide what is worth keeping.",
  "help.term.domain.name": "Topic",
  "help.term.domain.body":
    "The subject a memory belongs to, written as area:name, for example code:my-app or personal:health. It keeps different parts of your life apart.",
  "help.term.project.name": "Project",
  "help.term.project.body":
    "An optional label that ties a memory to one project. Memories without a project are shared by all of them.",
  "help.term.embedding.name": "Embeddings",
  "help.term.embedding.body":
    "A way to compare the meaning of texts. With embeddings on, searching for \"car\" can also find a memory about \"vehicle\".",
  "help.term.jobs.name": "Jobs",
  "help.term.jobs.body":
    "Routines the server runs in the background, such as cleaning up old memories. You rarely need to touch them.",
  "help.term.insights.name": "Insights",
  "help.term.insights.body":
    "A health report on your memories: how many there are, how old, and which ones look duplicated or low value.",
  "help.term.preview.name": "What the agent will see",
  "help.term.preview.body":
    "A preview of the memories your AI agent receives when it starts working, so you can check they make sense.",
  "help.term.forgetting.name": "Forgetting",
  "help.term.forgetting.body":
    "Memories that are old and never used slowly fade and are hidden, so the useful ones stay on top. Nothing is lost right away.",
  "help.term.apiKey.name": "API key",
  "help.term.apiKey.body":
    "A secret password that lets a tool, such as Claude Code, read and write your memories. Treat it like a password.",
  "help.page.home":
    "M3m0s is your long-term memory for AI agents. Add notes, search them, and connect your agent so it remembers what you told it.",
  "help.page.search":
    "Type a few words about what you want to recall. Results are your saved memories, best match first.",
  "help.page.add":
    "Save something you want your agents to remember. Short and specific works best, one idea at a time.",
  "help.page.recent":
    "Your most recently saved memories. Use the filters to narrow down by topic or project.",
  "help.page.stats":
    "Numbers about your memory: how many notes you have, where they come from and what the server is using.",
  "help.page.insights":
    "A health check of your memories. It shows what is old, unused, duplicated or low value, and never changes anything by itself.",
  "help.page.jobs":
    "Routines the server runs in the background. They keep your memory tidy, and you can run any of them now.",
  "help.page.embedding":
    "Embeddings let search understand meaning. Set them up here with an AI provider key; without them search only matches words.",
  "help.page.preview":
    "See exactly which memories an AI agent would receive for a topic, before you connect one.",
  "help.page.connect":
    "Give your AI tools the address of this server and a key, and they can read and save memories here.",
} as const;
