"""Settings → Accounts, the two-pane page (owner, 2026-10-24). In the real
window on the hidden screen, with real mouse clicks, on a recorded engine
(tests/fixtures/accounts-providers.json): the left bar lists the agent CLIs
first and the API providers after, with Hyper, GitHub Copilot and ChatGPT
gone; Claude Code's detail shows its two accounts, and Codex's its own; a
provider with a key offers Change… and another account and, when it has
none, Add key… opening that provider's key sheet; one without a key is not
offered a second account. Pictures go to tests/out/accounts_providers/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/accounts-providers.json accounts_providers.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def app_window(timeout=20):
    """The largest visible window of the app, found by its class."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d and (best is None or int(d["WIDTH"]) * int(d["HEIGHT"]) > best[1]):
                best = (wid, int(d["WIDTH"]) * int(d["HEIGHT"]))
        if best and best[1] > 100 * 100:
            return best[0]
        time.sleep(0.3)
    return None


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def center(selector, text=None):
    find = f"[...document.querySelectorAll({json.dumps(selector)})]" + (f".find((e) => e.textContent.trim().startsWith({json.dumps(text)}))" if text else "[0]")
    return run(f"""
      const el = {find};
      if (!el) return null;
      el.scrollIntoView({{ block: 'nearest' }});
      const r = el.getBoundingClientRect();
      return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};
    """)


def click(selector, text=None):
    point = center(selector, text)
    if not point:
        return False
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))
    xdo("click", 1)
    time.sleep(0.5)
    return True


def picture(name):
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name)], check=True)


def rows():
    return run("""
      return [...document.querySelectorAll('[data-probe="provider-accounts"] [data-account]')].map((row) => (
        { text: row.querySelector('.name').textContent.trim() }));
    """) or []


def actions():
    """The detail's buttons by their data-action."""
    return run("""
      const d = document.querySelector('[data-probe="provider-accounts"] .detail');
      return [...(d?.querySelectorAll('button[data-action]') ?? [])].map((b) => b.dataset.action);
    """) or []


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# 1. Settings > Accounts: the CLIs first, the API providers after.
click("nav.chats button.settings")
click(".settings-nav .row", "Accounts")
ids = run("""
  const nav = document.querySelector('[data-probe="accounts-providers"]');
  return [...(nav?.querySelectorAll('[data-provider]') ?? [])].map((el) => el.getAttribute('data-provider'));
""") or []
check("claude-code" in ids and "openrouter" in ids and ids.index("claude-code") < ids.index("openrouter"),
      f"the CLIs come before the API providers ({ids})")
check(not any(i in ids for i in ("hyper", "copilot", "openai")), f"Hyper, GitHub Copilot and ChatGPT are gone ({ids})")
picture("bar.png")

# 2. Claude Code's detail: its own login and its added account.
text = run("return document.querySelector('[data-probe=\"provider-accounts\"] .detail-name')?.textContent.trim() ?? '';")
check(text == "Claude Code", f"the first CLI shows its detail ({text})")
r = rows()
check([x["text"] for x in r] == ["Claude 1", "Claude 2"], f"Claude Code lists just Claude 1 and Claude 2 ({[x['text'] for x in r]})")
picture("claude.png")

# 3. Codex's detail is one switch away.
click('[data-probe="accounts-providers"] [data-provider="codex-cli"]')
r = rows()
check(len(r) == 1 and r[0]["text"].startswith("Codex 1"), f"Codex shows its own login ({[x['text'] for x in r]})")

# 4. A provider with a key: offers Change… and another account.
click('[data-probe="accounts-providers"] [data-provider="openrouter"]')
acts = actions()
check("change" in acts, f"OpenRouter's key is set, with Change… ({acts})")
check("add" in acts, f"OpenRouter offers another account ({acts})")
picture("openrouter.png")

# 5. A provider with no key offers Add key…, which opens its sheet.
click('[data-probe="accounts-providers"] [data-provider="deepinfra"]')
acts = actions()
check("add-key" in acts, f"DeepInfra offers Add key… ({acts})")
check("add" not in acts, f"an unconfigured provider is not offered a second account ({acts})")
click('[data-probe="provider-accounts"] .detail button[data-action="add-key"]')
sheet = run("return document.querySelector('[data-sheet=\"api_key_input\"] [data-provider]')?.getAttribute('data-provider') ?? '';") or ""
check(sheet == "deepinfra", f"Add key… opens DeepInfra's key sheet ({sheet!r})")
picture("key-sheet.png")
xdo("key", "Escape")
finish()
