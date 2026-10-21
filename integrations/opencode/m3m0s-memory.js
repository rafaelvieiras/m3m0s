// m3m0s memory for opencode.
//
// Recall: the text of each user message is searched against m3m0s with the
// same client logic as the Claude Code hook (`m3m0s hook claude-code
// --inject`, D44/D47) and the relevant hits are appended to that turn's
// system prompt.
// Capture: when a session goes idle, its text is written as a transcript and
// handed to `m3m0s hook claude-code` (source "opencode", D49), which already
// handles checkpoints, redaction, digests and the durable queue.
//
// Every failure is silent: this plugin must never block opencode.
import { spawn } from "node:child_process";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const HOME = homedir();
// M3M0S_BIN wins, then ~/go/bin/m3m0s, then whatever is on PATH.
const GO_BIN = join(HOME, "go", "bin", "m3m0s");
const BIN = process.env.M3M0S_BIN || (existsSync(GO_BIN) ? GO_BIN : "m3m0s");
const LOG = join(HOME, ".m3m0s", "hooks", "claude-code.log");
const DIR = join(HOME, ".m3m0s", "hooks", "opencode");

function runHook(args, payload, { wait, source }) {
  return new Promise((resolve) => {
    let out = "";
    let done = false;
    const finish = (v) => {
      if (!done) {
        done = true;
        resolve(v);
      }
    };
    const script = `set -a; . "$HOME/.m3m0s/hook.env"; M3M0S_HOOK_SOURCE=${source} exec "${BIN}" hook claude-code ${args} 2>>"${LOG}"`;
    const p = spawn("sh", ["-c", script], { stdio: ["pipe", "pipe", "ignore"], detached: !wait });
    const timer = wait ? setTimeout(() => { p.kill(); finish(""); }, 6000) : null;
    p.on("error", () => finish(""));
    p.stdin.on("error", () => {});
    p.stdout.on("data", (d) => (out += d));
    p.on("close", () => {
      if (timer) clearTimeout(timer);
      finish(out);
    });
    p.stdin.end(JSON.stringify(payload));
    if (!wait) {
      p.unref();
      finish("");
    }
  });
}

function textOf(parts) {
  return (parts || [])
    .filter((p) => p && p.type === "text" && !p.synthetic && typeof p.text === "string")
    .map((p) => p.text)
    .join("\n\n");
}

export const M3m0sMemory = async ({ client, directory }) => {
  const prompts = new Map(); // sessionID -> latest user prompt
  const sessions = new Set(); // sessions with something to capture

  async function capture(sessionID, event) {
    try {
      const res = await client.session.messages({ path: { id: sessionID } });
      const messages = res?.data ?? res ?? [];
      const lines = [];
      for (const m of messages) {
        const role = m?.info?.role;
        const text = textOf(m?.parts);
        if ((role !== "user" && role !== "assistant") || !text.trim()) continue;
        lines.push(
          JSON.stringify({
            type: "message",
            message: { role, model: m.info.modelID || m.info.model?.modelID || "", content: [{ type: "text", text }] },
          }),
        );
      }
      if (!lines.length) return;
      mkdirSync(DIR, { recursive: true });
      const file = join(DIR, `${sessionID}.jsonl`);
      writeFileSync(file, lines.join("\n") + "\n");
      await runHook("", {
        session_id: `opencode-${sessionID}`,
        transcript_path: file,
        hook_event_name: event,
        cwd: directory,
      }, { wait: false, source: "opencode" });
    } catch {}
  }

  return {
    "chat.message": async (input, output) => {
      try {
        const text = textOf(output?.parts);
        if (text.trim()) prompts.set(input.sessionID, text);
        sessions.add(input.sessionID);
      } catch {}
    },

    "experimental.chat.system.transform": async (input, output) => {
      try {
        const prompt = prompts.get(input.sessionID);
        if (!prompt) return;
        prompts.delete(input.sessionID); // once per user message
        const raw = await runHook("--inject", {
          session_id: `opencode-${input.sessionID}`,
          hook_event_name: "UserPromptSubmit",
          cwd: directory,
          prompt,
        }, { wait: true, source: "opencode" });
        const block = JSON.parse(raw).hookSpecificOutput?.additionalContext;
        if (block) {
          output.system.push(
            `${block}\n\n(Long-term memory notes; they may be outdated — prefer what the user says now.)`,
          );
        }
      } catch {}
    },

    event: async ({ event }) => {
      try {
        if (event?.type === "session.idle" && event.properties?.sessionID) {
          await capture(event.properties.sessionID, "Stop");
        }
      } catch {}
    },

    dispose: async () => {
      for (const id of sessions) await capture(id, "SessionEnd");
    },
  };
};
