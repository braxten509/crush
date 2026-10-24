"""Settings → Accounts, the two-pane page (owner, 2026-10-24). In the real
window on the hidden screen, with real mouse clicks, on a recorded engine
(tests/fixtures/accounts-providers.json): the left bar lists the agent CLIs
first and the API providers after, under their headings, with Hyper, GitHub
Copilot and ChatGPT gone; Claude Code's detail shows its two accounts and
Codex's its own; every account row spells its dot with spaces and keeps Rename
last, in one place; a provider with a key says so and offers Change…, another
account and, when it has none, Add key… opening that provider's key sheet;
one without a key says it is not set up yet. Pictures go to
tests/out/accounts_providers/.

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
      return [...document.querySelectorAll('[data-probe="provider-accounts"] [data-account]')].map((row) => {
        const acts = [...row.querySelectorAll('button.act')];
        return { text: row.querySelector('.name').textContent.trim(), last: acts.at(-1)?.textContent.trim() ?? '',
                 right: Math.round((acts.at(-1)?.getBoundingClientRect().right ?? 0) * 10) / 10 };
      });
    """) or []


def detail():
    return run("""
      const d = document.querySelector('[data-probe="provider-accounts"] .detail');
      return { text: d?.textContent.replace(/\\s+/g, ' ').trim() ?? '',
               acts: [...(d?.querySelectorAll('button.act') ?? [])].map((b) => b.textContent.trim()) };
    """) or {}


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# 1. Settings > Accounts: the two headings, the CLIs first, the API providers after.
click("nav.chats button.settings")
click(".settings-nav .row", "Accounts")
bar = run("""
  const nav = document.querySelector('[data-probe="accounts-providers"]');
  const out = [];
  for (const el of nav?.children ?? []) {
    if (el.classList.contains('providers-head')) out.push({ head: el.textContent.trim() });
    else if (el.hasAttribute('data-provider')) out.push({ id: el.getAttribute('data-provider'), name: el.textContent.trim() });
  }
  return out;
""") or []
heads = [e["head"] for e in bar if "head" in e]
ids = [e["id"] for e in bar if "id" in e]
check(heads == ["Agent CLIs", "API providers"], f"the bar groups the CLIs first, the API providers after ({heads})")
check(ids.index("claude-code") < ids.index("openrouter"), f"a CLI comes before an API provider ({ids})")
check(not any(i in ids for i in ("hyper", "copilot", "openai")), f"Hyper, GitHub Copilot and ChatGPT are gone ({ids})")
picture("bar.png")

# 2. Claude Code's detail: its own login and its added account.
text = run("return document.querySelector('[data-probe=\"provider-accounts\"] .detail-name')?.textContent.trim() ?? '';")
check(text == "Claude Code", f"the first CLI shows its detail ({text})")
r = rows()
check([x["text"].split(" · ")[0] for x in r] == ["Claude 1", "Claude 2"], f"Claude Code lists Claude 1 and Claude 2 ({[x['text'] for x in r]})")
check(all(" · " in x["text"] for x in r), f"each account name has a space on both sides of its dot ({[x['text'] for x in r]})")
check(all(x["last"] == "Rename" for x in r) and len({x["right"] for x in r}) == 1, f"Rename is last on every row, in one place ({r})")
check("Another Claude Code account" in detail()["text"], f"the detail offers another Claude Code account ({detail()['text'][:120]})")
picture("claude.png")

# 3. Codex's detail is one switch away.
click('[data-probe="accounts-providers"] .provider', "Codex")
r = rows()
check(len(r) == 1 and r[0]["text"].startswith("Codex 1"), f"Codex shows its own login ({[x['text'] for x in r]})")

# 4. A provider with a key: says so, and offers Change… and another account.
click('[data-probe="accounts-providers"] .provider', "OpenRouter")
d = detail()
check("API key set" in d["text"] and "Change…" in d["acts"], f"OpenRouter says its key is set, with Change… ({d})")
check("Another OpenRouter account" in d["text"] and "Add…" in d["acts"], f"OpenRouter offers another account ({d['acts']})")
picture("openrouter.png")

# 5. A provider with no key says it is not set up yet, and Add key… opens its sheet.
click('[data-probe="accounts-providers"] .provider', "DeepInfra")
d = detail()
check("No API key" in d["text"] and "Add key…" in d["acts"], f"DeepInfra says it has no key, with Add key… ({d})")
check("Not set up yet" in d["text"] and "Another DeepInfra account" not in d["text"], f"an unconfigured provider is not offered a second account ({d['text'][:160]})")
click('[data-probe="provider-accounts"] .detail button.act', "Add key…")
title = run("return document.querySelector('[data-sheet=\"api_key_input\"]')?.textContent.replace(/\\s+/g, ' ').trim() ?? '';") or ""
check("DeepInfra" in title and "Key" in title, f"Add key… opens DeepInfra's key sheet ({title[:120]})")
picture("key-sheet.png")
xdo("key", "Escape")
time.sleep(0.4)
finish()
