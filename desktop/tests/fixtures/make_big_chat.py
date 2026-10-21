"""Writes tests/out/big-chat.json: long-chat.json plus "Huge chat", 18,000
messages of about 5 KB each (about 50 MB, like the owner's longest chat,
2026-10-21). Too big to keep in the repo, so big_chat.py's run makes it.

Usage: python3 tests/fixtures/make_big_chat.py [--real SOCKET WORKSPACE SESSION]
With --real, the messages are a read-only copy of a real chat from a running
engine (GET only), so the check runs on real tool calls and sub-agents; the
copy stays in tests/out, which git ignores.
"""
import json, os, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
BIG = "b16c4a75-0000-4000-8000-000000000001"
COUNT = 18000

with open(os.path.join(HERE, "long-chat.json")) as f:
    d = json.load(f)
g = d["get"]
small = g["sessions"][0]
filler = "The session cookie uses SameSite=Strict, so the browser drops it on the redirect back. " * 56
msgs = []
for i in range(COUNT // 2):
    at = 1792000000 + 2 * i
    msgs.append({"id": f"bu{i}", "role": "user", "session_id": BIG, "parts": [{"type": "text", "data": {"text": f"Question {i}: why?"}}],
                 "model": "", "provider": "", "created_at": at, "updated_at": at})
    msgs.append({"id": f"ba{i}", "role": "assistant", "session_id": BIG,
                 "parts": [{"type": "text", "data": {"text": f"Answer {i}. {filler}"}}, {"type": "finish", "data": {"reason": "end_turn", "time": at + 1}}],
                 "model": "opus", "provider": "claude-code", "created_at": at + 1, "updated_at": at + 1})
if len(sys.argv) == 5 and sys.argv[1] == "--real":
    sock, wsid, sid = sys.argv[2:]
    def engine(path):
        out = subprocess.run(["curl", "-sf", "--unix-socket", sock, f"http://x/v1/workspaces/{wsid}/{path}"], capture_output=True, check=True).stdout
        return json.loads(out)
    msgs = engine(f"sessions/{sid}/messages")
    for m in msgs:
        m["session_id"] = BIG
    # The agent tools' chats ("<message id>$$<call id>") the timeline reads too.
    for m in msgs:
        for part in m.get("parts", []):
            data = part.get("data") or {}
            if part.get("type") == "tool_call" and data.get("name") in ("agent", "agentic_fetch"):
                child = f"{m['id']}$${data.get('id')}"
                try:
                    g[f"sessions/{child}/messages"] = engine(f"sessions/{child}/messages")
                except subprocess.CalledProcessError:
                    pass
    q = [m for m in msgs if m["role"] == "user"]
    print("real chat:", len(msgs), "messages,", len(q), "from the user")
    COUNT = len(msgs)
record = dict(small, id=BIG, title="Huge chat", message_count=COUNT, created_at=1792000000, updated_at=1792500000)
g["sessions"] = [record] + g["sessions"]
g[f"sessions/{BIG}"] = record
g[f"sessions/{BIG}/messages"] = msgs
g[f"sessions/{BIG}/history"] = []
out = os.path.join(HERE, "..", "out", "big-chat.json")
os.makedirs(os.path.dirname(out), exist_ok=True)
with open(out, "w") as f:
    json.dump(d, f)
print(out, os.path.getsize(out) // 1_000_000, "MB")
