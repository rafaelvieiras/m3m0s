// Slash (`/`) commands of the WebUI floating chat (D23). Central list so
// autocomplete (ChatWidget) and execution (runChatCommand) stay in sync —
// adding a new command is just one item here + 1 case in the runChatCommand
// switch.
import type { TKey } from "@/i18n";

export interface ChatCommand {
  name: string;
  /** i18n key of the description shown in the autocomplete menu. */
  description: TKey;
}

export const CHAT_COMMANDS: ChatCommand[] = [
  { name: "clean", description: "chat.cmd.clean" },
];

export function matchChatCommands(query: string): ChatCommand[] {
  const needle = query.toLowerCase();
  return CHAT_COMMANDS.filter((c) => c.name.startsWith(needle));
}

// parseSlashCommand recognizes "/something" typed alone (no space after) —
// used both to decide whether the autocomplete dropdown should appear and to
// recognize a complete command on send.
export function parseSlashCommand(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed.startsWith("/")) return null;
  const rest = trimmed.slice(1);
  if (rest.includes(" ") || rest.includes("\n")) return null;
  return rest;
}
