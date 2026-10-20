"""Saves the real engine's scholars-edition theme view to
tests/out/capture_theme/theme.json, for fixtures that need a theme (the
recorded engines have none). Run on the engine leg, without CHECK_ARGS:
    tests/check.sh capture_theme.py
"""
import json, os, sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, engine_get, finish, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
view = engine_get(probe_path, "/v1/themes/scholars-edition")
check(bool(view and view.get("resolved")), "the engine sends the scholars-edition view")
with open(os.path.join(out_dir, "theme.json"), "w") as f:
    json.dump(view, f, indent=1)
finish()
