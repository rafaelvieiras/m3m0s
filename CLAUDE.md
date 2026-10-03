# M3m0s — Claude Code entry point

Claude Code auto-loads only `CLAUDE.md` (walking up from cwd to root), not
`AGENTS.md`. This file exists purely to import the real instructions, so any
tool that reads `AGENTS.md` and any tool that reads `CLAUDE.md` stay in sync
from a single source of truth:

@AGENTS.md
