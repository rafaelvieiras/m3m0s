import asyncio
import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
import m3m0s_memory_filter as mod  # noqa: E402


class Fake:
    """Tiny m3m0s stand-in that records requests."""

    def __init__(self):
        self.requests = []
        self.search_response = {
            "results": [
                {"content": "Compose lives in the infra repo", "domain": "code:x",
                 "source": "claude-code", "createdAt": "2026-07-15T00:00:00Z", "score": 0.8},
                {"content": "Unrelated", "domain": "code:y", "score": 0.4},
            ],
            "contextBlock": "## Relevant memories\n\n1. both",
        }
        self.status = 200
        fake = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                n = int(self.headers.get("Content-Length", 0))
                req = json.loads(self.rfile.read(n))
                fake.requests.append((self.path, self.headers.get("Authorization"), req))
                out = fake.search_response if self.path.endswith("/search") else {"status": "pending"}
                self.send_response(fake.status)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                self.wfile.write(json.dumps(out).encode())

            def log_message(self, *a):
                pass

        self.srv = HTTPServer(("127.0.0.1", 0), H)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()
        self.url = f"http://127.0.0.1:{self.srv.server_port}"


@pytest.fixture
def fake():
    f = Fake()
    yield f
    f.srv.shutdown()


def make(fake, **kw):
    f = mod.Filter()
    f.valves.m3m0s_url = fake.url
    for k, v in kw.items():
        setattr(f.valves, k, v)
    return f


def run(coro):
    return asyncio.run(coro)


def body(q="Where does the compose file for the server live?"):
    return {"messages": [{"role": "user", "content": q}]}


def test_inlet_injects_filtered_block_and_auth(fake):
    out = run(make(fake, api_key="k").inlet(body()))
    sys_msg = out["messages"][0]
    assert sys_msg["role"] == "system"
    assert "Compose lives" in sys_msg["content"]
    assert "Unrelated" not in sys_msg["content"]
    path, auth, req = fake.requests[0]
    assert path == "/api/v1/search" and auth == "Bearer k"
    assert req["maxTokens"] == 600 and req["mode"] == "vector"


def test_inlet_uses_server_block_when_nothing_dropped(fake):
    fake.search_response["results"].pop()
    out = run(make(fake).inlet(body()))
    assert "1. both" in out["messages"][0]["content"]


def test_inlet_no_duplicate_on_regenerate(fake):
    f = make(fake)
    b = run(f.inlet(body()))
    b = run(f.inlet(b))
    assert sum(m["content"].count(mod.FENCE_OPEN) for m in b["messages"]) == 1


def test_inlet_appends_to_existing_system(fake):
    b = {"messages": [{"role": "system", "content": "Be brief."},
                      {"role": "user", "content": "Where does the compose file live?"}]}
    out = run(make(fake).inlet(b))
    assert out["messages"][0]["content"].startswith("Be brief.")
    assert mod.FENCE_OPEN in out["messages"][0]["content"]
    assert len(out["messages"]) == 2


def test_inlet_short_prompt_skips(fake):
    out = run(make(fake).inlet(body("ok thanks")))
    assert len(out["messages"]) == 1 and not fake.requests


def test_inlet_survives_server_down():
    f = mod.Filter()
    f.valves.m3m0s_url = "http://127.0.0.1:1"
    f.valves.timeout_seconds = 0.5
    assert len(run(f.inlet(body()))["messages"]) == 1


def test_inlet_survives_server_error(fake):
    fake.status = 500
    assert len(run(make(fake).inlet(body()))["messages"]) == 1


def test_allowed_users(fake):
    f = make(fake, allowed_users="rafael, Other@Example.com")
    assert f._allowed({"name": "Rafael"})
    assert f._allowed({"email": "other@example.com"})
    assert not f._allowed({"name": "someone"})
    assert not f._allowed(None)
    assert len(run(f.inlet(body(), {"name": "someone"}))["messages"]) == 1
    assert make(fake)._allowed(None)


def capture(f, b, user=None):
    async def go():
        out = await f.outlet(b, user)
        if f._tasks:
            await asyncio.gather(*list(f._tasks))
        return out

    return run(go())


def turn():
    return {"model": "my model", "chat_id": "c1", "messages": [
        {"role": "user", "content": "How do I restart the service?"},
        {"role": "assistant", "content": "Run systemctl restart on the unit."}]}


def test_outlet_captures(fake):
    capture(make(fake), turn())
    path, _, req = fake.requests[0]
    assert path == "/api/v1/memories"
    assert req["source"] == "openwebui" and req["domain"] == "personal:openwebui"
    assert req["model"] == "my-model"
    assert req["content"].startswith("User: How do I restart")
    assert "Assistant: Run systemctl" in req["content"]


def test_outlet_skips_short_disabled_and_unallowed(fake):
    short = {"messages": [{"role": "user", "content": "hi"},
                          {"role": "assistant", "content": "yo"}]}
    capture(make(fake), short)
    capture(make(fake, capture_enabled=False), turn())
    capture(make(fake, allowed_users="rafael"), turn(), {"name": "x"})
    capture(make(fake, domain="Bad Domain"), turn())
    assert not fake.requests


def test_outlet_does_not_raise_when_down():
    f = mod.Filter()
    f.valves.m3m0s_url = "http://127.0.0.1:1"
    f.valves.capture_timeout_seconds = 0.5
    b = turn()
    assert capture(f, b) == b


def test_multimodal_content(fake):
    b = {"messages": [{"role": "user", "content": [
        {"type": "text", "text": "Where does the compose file live?"},
        {"type": "image_url", "image_url": {"url": "x"}}]}]}
    out = run(make(fake).inlet(b))
    assert out["messages"][0]["role"] == "system"
