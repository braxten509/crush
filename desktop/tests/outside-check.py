"""Outside-world protection check. Records fingerprints of the owner's real
Crush files before a test run and confirms afterwards that none changed,
appeared or disappeared. Contents are never printed.

- Config and project-list files: a sha256 of the contents.
- Every other file in the two real Crush folders: size and modification time
  only, so no contents are read. The private sub-folder inside the config
  folder is skipped entirely.

Usage: outside-check.py snapshot FILE | verify FILE"""
import hashlib, json, os, sys

HOME = os.path.expanduser("~")
CONFIG_DIR = os.path.join(HOME, ".config/crush")
DATA_DIR = os.path.join(HOME, ".local/share/crush")
HASHED = [
    os.path.join(DATA_DIR, "projects.json"),
    os.path.join(DATA_DIR, "crush.json"),
    os.path.join(CONFIG_DIR, "crush.json"),
    os.path.join(CONFIG_DIR, "crushrc"),
]
SKIP_DIRS = {os.path.join(CONFIG_DIR, "secrets")}
# Lock files change whenever any running Crush takes a lock; they hold nothing.
SKIP_SUFFIXES = (".lock",)


def fingerprints():
    out = {}
    for p in HASHED:
        try:
            with open(p, "rb") as f:
                out[p] = "sha256:" + hashlib.sha256(f.read()).hexdigest()
        except FileNotFoundError:
            out[p] = None
    for top in (CONFIG_DIR, DATA_DIR):
        for dirpath, dirnames, filenames in os.walk(top):
            dirnames[:] = [d for d in dirnames if os.path.join(dirpath, d) not in SKIP_DIRS]
            for name in filenames:
                p = os.path.join(dirpath, name)
                if p in out or name.endswith(SKIP_SUFFIXES):
                    continue
                try:
                    st = os.stat(p)
                    out[p] = f"meta:{st.st_size}:{st.st_mtime_ns}"
                except FileNotFoundError:
                    pass
    return out


mode, path = sys.argv[1], sys.argv[2]
if mode == "snapshot":
    with open(path, "w") as f:
        json.dump(fingerprints(), f)
    print("outside-world snapshot taken")
    sys.exit(0)
with open(path) as f:
    before = json.load(f)
now = fingerprints()
changed = sorted(p for p in set(before) | set(now) if before.get(p) != now.get(p))
for p in sorted(set(before) | set(now)):
    print(("FAIL changed: " if p in changed else "PASS unchanged: ") + p)
print(f"{len(changed)} changed of {len(set(before) | set(now))} watched files")
sys.exit(1 if changed else 0)
