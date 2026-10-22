// WebUI floating chat (D23, docs/decisions/) — mounted once in AppShell,
// outside the <Outlet/>, so it survives route changes without remounting (the
// conversation state lives in this component and deliberately does not
// persist across page reloads — see D23). Two capabilities are exercised
// against the backend via POST /api/v1/chat (internal/httpapi/chat.go):
// asking about the memory (automatic RAG, no command needed) and saving
// something to it (add_memory tool, triggered by the LLM itself when the
// user explicitly asks). The only slash command today is /clean, executed
// locally without calling the API.
import { useEffect, useRef, useState } from "react";
import { MessageCircle, X, Maximize2, Minimize2, Send, Bot, User } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Markdown } from "@/components/ui/markdown";
import { cn } from "@/lib/utils";
import { useChatMessage } from "@/hooks/useMemoryApi";
import type { ChatMessagePayload } from "@/lib/schemas";
import { matchChatCommands, parseSlashCommand } from "./chat-commands";
import { toast } from "sonner";
import { useT } from "@/i18n";

export function ChatWidget() {
  const { t } = useT();
  const [isOpen, setIsOpen] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const [messages, setMessages] = useState<ChatMessagePayload[]>([]);
  const [input, setInput] = useState("");
  const [commandIndex, setCommandIndex] = useState(0);

  const chatMutation = useChatMessage();
  const listRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const commandQuery = parseSlashCommand(input);
  const commandMatches = commandQuery !== null ? matchChatCommands(commandQuery) : [];
  const showCommandMenu = commandQuery !== null && commandMatches.length > 0;

  useEffect(() => {
    setCommandIndex(0);
  }, [input]);

  useEffect(() => {
    if (!listRef.current) return;
    listRef.current.scrollTop = listRef.current.scrollHeight;
  }, [messages, chatMutation.isPending]);

  useEffect(() => {
    if (isOpen) inputRef.current?.focus();
  }, [isOpen]);

  function applyCommand(name: string) {
    setInput(`/${name} `);
    setCommandIndex(0);
  }

  function runCommand(name: string) {
    switch (name) {
      case "clean":
        setMessages([]);
        toast.success(t("chat.cleared"));
        break;
      default:
        toast.error(t("chat.unknownCommand", { name }));
    }
  }

  function handleSend() {
    const trimmed = input.trim();
    if (!trimmed || chatMutation.isPending) return;

    const exactCommand = trimmed.match(/^\/(\S+)\s*$/);
    if (exactCommand) {
      runCommand(exactCommand[1]);
      setInput("");
      return;
    }

    const nextMessages: ChatMessagePayload[] = [...messages, { role: "user", content: trimmed }];
    setMessages(nextMessages);
    setInput("");

    chatMutation.mutate(
      { messages: nextMessages },
      {
        onSuccess: (res) => {
          setMessages((prev) => [...prev, { role: "assistant", content: res.reply }]);
        },
        onError: (err) => {
          toast.error(t("chat.failed", { message: err.message }));
        },
      },
    );
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (showCommandMenu) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setCommandIndex((i) => (i + 1) % commandMatches.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setCommandIndex((i) => (i - 1 + commandMatches.length) % commandMatches.length);
        return;
      }
      if (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey)) {
        e.preventDefault();
        applyCommand(commandMatches[commandIndex].name);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setInput("");
        return;
      }
    }

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  return (
    <>
      <Button
        type="button"
        size="icon-lg"
        onClick={() => setIsOpen((v) => !v)}
        aria-label={isOpen ? t("chat.closeMemoryChat") : t("chat.openMemoryChat")}
        className="fixed right-4 bottom-4 z-50 rounded-full shadow-lg"
      >
        {isOpen ? <X /> : <MessageCircle />}
      </Button>

      {isOpen && (
        <div
          role="complementary"
          aria-label={t("chat.region")}
          className={cn(
            "fixed top-4 right-4 bottom-20 z-40 flex flex-col overflow-hidden rounded-xl border bg-card text-card-foreground shadow-2xl ring-1 ring-foreground/10 transition-[width] duration-150",
            isExpanded ? "w-[min(40rem,calc(100vw-2rem))]" : "w-[min(22rem,calc(100vw-2rem))]",
          )}
        >
          <header className="flex items-center justify-between gap-2 border-b px-3 py-2">
            <div className="flex items-center gap-1.5 text-sm font-semibold tracking-tight">
              <Bot className="size-4" />
              {t("chat.title")}
            </div>
            <div className="flex items-center gap-0.5">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setIsExpanded((v) => !v)}
                aria-label={isExpanded ? t("chat.collapse") : t("chat.expand")}
              >
                {isExpanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setIsOpen(false)}
                aria-label={t("chat.close")}
              >
                <X className="size-3.5" />
              </Button>
            </div>
          </header>

          <div ref={listRef} className="flex-1 space-y-3 overflow-y-auto px-3 py-3">
            {messages.length === 0 && (
              <p className="text-sm text-muted-foreground">
                {t("chat.empty", { slash: "/" })}
              </p>
            )}
            {messages.map((m, i) => (
              <ChatBubble key={i} message={m} />
            ))}
            {chatMutation.isPending && (
              <div className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Bot className="size-4 shrink-0" />
                <span className="animate-pulse">{t("chat.typing")}</span>
              </div>
            )}
          </div>

          <div className="relative border-t p-2">
            {showCommandMenu && (
              <ul className="absolute inset-x-2 bottom-full mb-1 overflow-hidden rounded-lg border bg-popover text-popover-foreground shadow-md">
                {commandMatches.map((cmd, i) => (
                  <li key={cmd.name}>
                    <button
                      type="button"
                      onMouseDown={(e) => e.preventDefault()}
                      onClick={() => applyCommand(cmd.name)}
                      className={cn(
                        "flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-sm",
                        i === commandIndex ? "bg-muted text-foreground" : "hover:bg-muted/60",
                      )}
                    >
                      <span className="font-mono">/{cmd.name}</span>
                      <span className="truncate text-xs text-muted-foreground">{t(cmd.description)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <div className="flex items-end gap-2">
              <Textarea
                ref={inputRef}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={t("chat.placeholder")}
                rows={1}
                disabled={chatMutation.isPending}
                className="min-h-8 resize-none py-1.5 text-sm"
              />
              <Button
                type="button"
                size="icon"
                onClick={handleSend}
                disabled={chatMutation.isPending || !input.trim()}
                aria-label={t("chat.send")}
              >
                <Send className="size-4" />
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

function ChatBubble({ message }: { message: ChatMessagePayload }) {
  const isUser = message.role === "user";
  return (
    <div className={cn("flex items-start gap-1.5", isUser && "flex-row-reverse")}>
      {isUser ? (
        <User className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      ) : (
        <Bot className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      )}
      <div
        className={cn(
          "max-w-[85%] rounded-lg px-2.5 py-1.5 text-sm",
          isUser ? "bg-primary text-primary-foreground whitespace-pre-wrap" : "bg-muted text-foreground",
        )}
      >
        {isUser ? message.content : <Markdown content={message.content} />}
      </div>
    </div>
  );
}
