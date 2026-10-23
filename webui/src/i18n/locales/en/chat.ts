// Keys are "chat.*": the floating memory chat widget.
export const chat = {
  "chat.cmd.clean": "Clears this conversation's history",
  "chat.cleared": "Conversation cleared",
  "chat.unknownCommand": "Unknown command: /{name}",
  "chat.failed": "Chat failed: {message}",
  "chat.closeMemoryChat": "Close memory chat",
  "chat.openMemoryChat": "Open memory chat",
  "chat.region": "Memory chat",
  "chat.title": "Memory assistant",
  "chat.collapse": "Collapse panel",
  "chat.expand": "Expand panel",
  "chat.close": "Close chat",
  "chat.empty":
    "Ask about your memories, or ask me to save something new. I answer using what M3m0s remembers. Type {slash} to see the commands. If I say chat is not enabled, the server needs an AI provider key for chat.",
  "chat.typing": "typing…",
  "chat.placeholder": "Ask or request to save something… (/ for commands)",
  "chat.send": "Send message",
} as const;
