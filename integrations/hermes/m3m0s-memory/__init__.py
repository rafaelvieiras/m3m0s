"""m3m0s-memory: long-term memory for a Hermes agent, as a MemoryProvider (D50).

Hermes wraps what a memory provider returns from prefetch() in a fenced
<memory-context> block marked "NOT new user input" before appending it to the
user message. A plain pre_llm_call hook skips that fence, and a model can then
treat the user message as empty - so recall must go through a provider.

The provider reuses the `m3m0s` binary, so the relevance gates, redaction,
checkpoints and the durable queue are the ones every other client uses:

- prefetch:        `m3m0s hook claude-code --inject` with the user message.
- sync_turn:       appends the turn to a per-session JSONL transcript and runs
                   the capture path as "Stop" in the background.
- on_session_end:  runs the capture path as "SessionEnd" (always captures).

Configuration (read from /opt/data/.env, then the environment):
  M3M0S_URL, M3M0S_API_KEY   server and key (required)
  M3M0S_HOOK_DOMAIN          domain of captured memories (default personal:hermes)
  M3M0S_BIN                  binary path (default <plugin dir>/bin/m3m0s)

Activate with `memory.provider: m3m0s-memory` in config.yaml. Every failure is
silent: the provider must never block the agent.
"""

import json
import logging
import os
import subprocess
import threading

from agent.memory_provider import MemoryProvider

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
    safe = "".join(c if c.isalnum() or c in "-_" else "_" for c in str(session_id))
    return os.path.join(STATE_DIR, "sessions", safe + ".jsonl")


class M3m0sMemoryProvider(MemoryProvider):
    def __init__(self):
        self._session_id = ""
        self._platform = ""
        self._primary = True
        self._lock = threading.Lock()

    @property
    def name(self):
        return "m3m0s-memory"

    def is_available(self):
        return bool(_read_env("M3M0S_URL") and _read_env("M3M0S_API_KEY") and os.path.exists(_binary()))

    def initialize(self, session_id, **kwargs):
        self._session_id = str(session_id)
        self._platform = str(kwargs.get("platform", ""))
        self._primary = kwargs.get("agent_context", "primary") == "primary"

    def _skip(self):
        return (not self._primary) or self._platform in SKIP_PLATFORMS

    def get_tool_schemas(self):
        return []  # context-only: recall is injected, capture is automatic

    def prefetch(self, query, *, session_id=""):
        if self._skip() or not query:
            return ""
        sid = str(session_id or self._session_id)
        out = _run(
            ["--inject"],
            {
                "session_id": "hermes-" + sid,
                "hook_event_name": "UserPromptSubmit",
                "cwd": "/hermes",
                "prompt": query,
            },
            timeout=6,
        )
        try:
            return json.loads(out)["hookSpecificOutput"]["additionalContext"] or ""
        except Exception:
            return ""

    def sync_turn(self, user_content, assistant_content, *, session_id="", messages=None):
        if self._skip() or not (user_content or assistant_content):
            return
        sid = str(session_id or self._session_id)

        def work():
            try:
                with self._lock:
                    path = _transcript(sid)
                    os.makedirs(os.path.dirname(path), exist_ok=True)
                    with open(path, "a") as f:
                        for role, text in (("user", user_content), ("assistant", assistant_content)):
                            if text and str(text).strip():
                                f.write(json.dumps({
                                    "type": "message",
                                    "message": {"role": role, "content": [{"type": "text", "text": str(text)}]},
                                }) + "\n")
                    _run([], {
                        "session_id": "hermes-" + sid,
                        "transcript_path": path,
                        "hook_event_name": "Stop",
                        "cwd": "/hermes",
                    }, timeout=20)
            except Exception as exc:
                logger.debug("m3m0s-memory: capture failed: %s", exc)

        threading.Thread(target=work, daemon=True, name="m3m0s-sync").start()

    def on_session_end(self, messages):
        if self._skip():
            return
        try:
            path = _transcript(self._session_id)
            if os.path.exists(path):
                with self._lock:
                    _run([], {
                        "session_id": "hermes-" + self._session_id,
                        "transcript_path": path,
                        "hook_event_name": "SessionEnd",
                        "cwd": "/hermes",
                    }, timeout=30)
        except Exception as exc:
            logger.debug("m3m0s-memory: session end failed: %s", exc)

    def on_session_switch(self, new_session_id, *, parent_session_id="", reset=False, rewound=False, **kwargs):
        self._session_id = str(new_session_id)


def register(ctx):
    ctx.register_memory_provider(M3m0sMemoryProvider())
