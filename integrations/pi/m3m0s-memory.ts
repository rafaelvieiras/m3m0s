// @ts-nocheck
// m3m0s memory for pi: before each agent run, search long-term memory by the
// prompt and inject the relevant hits as a custom message. Reuses the same
// client-side logic as the Claude Code hook (`m3m0s hook claude-code --inject`,
// D44): relevance floor, per-session dedup, silent on any failure.
// Capture: after each agent run ("Stop") and on shutdown ("SessionEnd") the
// same hook reads this session file and POSTs new text (D46), source "pi".
import { spawn } from "node:child_process";

const BIN = `${process.env.HOME}/go/bin/m3m0s`;

function inject(prompt: string, cwd: string, sessionId: string): Promise<string> {
  return new Promise((resolve) => {
    let out = "";
    let done = false;
    const finish = (v: string) => { if (!done) { done = true; resolve(v); } };
    const p = spawn("sh", ["-c", `set -a; . "$HOME/.m3m0s/hook.env"; exec "${BIN}" hook claude-code --inject 2>>"$HOME/.m3m0s/hooks/claude-code.log"`], {
      stdio: ["pipe", "pipe", "ignore"],
    });
    const t = setTimeout(() => { p.kill(); finish(""); }, 6000);
    p.stdout.on("data", (d) => (out += d));
    p.on("error", () => finish(""));
    p.on("close", () => {
      clearTimeout(t);
      try {
        finish(JSON.parse(out).hookSpecificOutput.additionalContext || "");
      } catch {
        finish("");
      }
    });
    p.stdin.end(JSON.stringify({
      session_id: `pi-${sessionId || "x"}`,
      hook_event_name: "UserPromptSubmit",
      cwd,
      prompt,
    }));
  });
}

// capture fires the Claude Code capture path against pi's session file,
// detached so it outlives a quitting pi. Frequent "Stop" events are gated
// inside the hook (growth + minimum new text); "SessionEnd" always captures.
function capture(event: string, ctx: any) {
  try {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (!file) return;
    const sid = String(ctx.sessionManager.getSessionId?.() ?? "");
    const p = spawn("sh", ["-c", `set -a; . "$HOME/.m3m0s/hook.env"; M3M0S_HOOK_SOURCE=pi exec "${BIN}" hook claude-code 2>>"$HOME/.m3m0s/hooks/claude-code.log"`], {
      stdio: ["pipe", "ignore", "ignore"],
      detached: true,
    });
    p.on("error", () => {});
    p.stdin.on("error", () => {});
    p.stdin.end(JSON.stringify({
      session_id: `pi-${sid}`,
      transcript_path: file,
      hook_event_name: event,
      cwd: ctx.cwd ?? process.cwd(),
    }));
    p.unref();
  } catch {}
}

export default function (pi) {
  pi.on("agent_end", async (_event, ctx) => capture("Stop", ctx));
  pi.on("session_shutdown", async (_event, ctx) => capture("SessionEnd", ctx));

  pi.on("before_agent_start", async (event, ctx) => {
    try {
      const sid = ctx?.sessionManager?.getSessionId?.() ?? ctx?.sessionId ?? "";
      const block = await inject(event.prompt ?? "", ctx?.cwd ?? process.cwd(), String(sid));
      if (!block) return;
      return {
        message: {
          customType: "m3m0s-memory",
          content:
            "<memory-context>\n" +
            "[System note: the following is recalled long-term memory, NOT new user input. " +
            "It may be outdated or from another project; ignore it if unrelated and prefer what the user says now.]\n\n" +
            `${block}\n` +
            "</memory-context>",
          display: false,
        },
      };
    } catch {
      return;
    }
  });
}
