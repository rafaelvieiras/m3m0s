"""m3m0s-memory: long-term memory for a Hermes agent (D50).

Reuses the `m3m0s` binary, so the relevance gates, redaction, checkpoints and
the durable queue are the ones every other client uses:

- pre_llm_call: `m3m0s hook claude-code --inject` with the user message;
  the relevant memories come back as {"context": ...}.
- post_llm_call: appends the turn to a per-session JSONL transcript and runs
  the capture path as "Stop" (gated by growth and minimum new text).
- on_session_end: runs the capture path as "SessionEnd" (always captures).

Configuration (read from /opt/data/.env, then the environment):
  M3M0S_URL, M3M0S_API_KEY   server and key (required)
  M3M0S_HOOK_DOMAIN           domain of captured memories (default personal:hermes)
  M3M0S_BIN                   binary path (default <plugin dir>/bin/m3m0s)

Every failure is silent: the plugin must never block the agent.
"""

import json
import logging
import os
import subprocess

logger = logging.getLogger(__name__)

PLUGIN_DIR = os.path.dirname(os.path.abspath(__file__))
ENV_FILE = "/opt/data/.env"
STATE_DIR = "/opt/data/m3m0s"  # on the persistent volume: checkpoints, spool, transcripts
SKIP_PLATFORMS = {"cron", "webhook"}


def _read_env(name, default=""):
    value = os.environ.get(name)
    if value:
        return value
    try:
        with open(ENV_FILE) as f:
            for line in f:
                line = line.strip()
                if line.startswith(name + "="):
                    return line.split("=", 1)[1].strip().strip('"').strip("'")
    except Exception:
        pass
    return default


def _binary():
    return _read_env("M3M0S_BIN", os.path.join(PLUGIN_DIR, "bin", "m3m0s"))


def _run(args, payload, timeout):
    url = _read_env("M3M0S_URL")
    key = _read_env("M3M0S_API_KEY")
    binary = _binary()
    if not (url and key and os.path.exists(binary)):
        return ""
    env = dict(os.environ)
    env.update({
        "M3M0S_URL": url,
        "M3M0S_API_KEY": key,
        "M3M0S_HOOK_SOURCE": "hermes",
        "M3M0S_HOOK_DOMAIN": _read_env("M3M0S_HOOK_DOMAIN", "personal:hermes"),
        "HOME": STATE_DIR,
    })
    try:
        out = subprocess.run(
            [binary, "hook", "claude-code"] + args,
            input=json.dumps(payload).encode(),
            capture_output=True,
            timeout=timeout,
            env=env,
        )
        return out.stdout.decode(errors="replace")
    except Exception as exc:  # timeout, exec failure...
        logger.debug("m3m0s-memory: %s", exc)
        return ""


def _transcript(session_id):
    os.makedirs(STATE_DIR, exist_ok=True)
    safe = "".join(c if c.isalnum() or c in "-_" else "_" for c in str(session_id))
    return os.path.join(STATE_DIR, "sessions", safe + ".jsonl")


def pre_llm_call(session_id="", user_message="", platform="", **kwargs):
    if platform in SKIP_PLATFORMS or not user_message:
        return None
    out = _run(
        ["--inject"],
        {
            "session_id": "hermes-" + str(session_id),
            "hook_event_name": "UserPromptSubmit",
            "cwd": "/hermes",
            "prompt": user_message,
        },
        timeout=6,
    )
    try:
        block = json.loads(out)["hookSpecificOutput"]["additionalContext"]
    except Exception:
        return None
    if not block:
        return None
    return {
        "context": block
        + "\n\n(Long-term memory notes; they may be outdated - prefer what the user says now.)"
    }


def post_llm_call(session_id="", user_message="", assistant_response="", platform="", model="", **kwargs):
    if platform in SKIP_PLATFORMS or not (user_message or assistant_response):
        return None
    try:
        path = _transcript(session_id)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "a") as f:
            for role, text in (("user", user_message), ("assistant", assistant_response)):
                if text and str(text).strip():
                    f.write(json.dumps({
                        "type": "message",
                        "message": {"role": role, "model": model or "", "content": [{"type": "text", "text": str(text)}]},
                    }) + "\n")
        _run([], {
            "session_id": "hermes-" + str(session_id),
            "transcript_path": path,
            "hook_event_name": "Stop",
            "cwd": "/hermes",
        }, timeout=20)
    except Exception as exc:
        logger.debug("m3m0s-memory: capture failed: %s", exc)
    return None


def on_session_end(session_id="", **kwargs):
    try:
        path = _transcript(session_id)
        if os.path.exists(path):
            _run([], {
                "session_id": "hermes-" + str(session_id),
                "transcript_path": path,
                "hook_event_name": "SessionEnd",
                "cwd": "/hermes",
            }, timeout=30)
    except Exception as exc:
        logger.debug("m3m0s-memory: session end failed: %s", exc)
    return None


def register(ctx):
    ctx.register_hook("pre_llm_call", pre_llm_call)
    ctx.register_hook("post_llm_call", post_llm_call)
    ctx.register_hook("on_session_end", on_session_end)
    logger.info("Plugin m3m0s-memory registered (pre_llm_call, post_llm_call, on_session_end)")
