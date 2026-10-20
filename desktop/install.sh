#!/usr/bin/env bash
# Installs the built Crush Desktop (Electron, owner 2026-10-20) for this user:
#   ~/.local/lib/crush-desktop/app/            Electron, its program named crush-desktop
#   ~/.local/lib/crush-desktop/app/resources/app/  main.cjs, preload.cjs, the screens
#                                              (dist/) and the helper (crush-desktop-core)
#   ~/.local/bin/crush-desktop                 starts it (the menu entry and the terminal)
# Other files in ~/.local/lib/crush-desktop (a crush build the settings may
# name) are left alone. A previous ~/.local/bin/crush-desktop that is a
# program, not this launcher, is kept as crush-desktop.before-electron.
# Build first: tests/build.sh. Quit the app before installing.
set -euo pipefail
cd "$(dirname "$0")"
ROOT="$(pwd)"
LIB="$HOME/.local/lib/crush-desktop/app"
BIN="$HOME/.local/bin/crush-desktop"
ELECTRON="$ROOT/node_modules/electron/dist"
CORE="$ROOT/src-tauri/target/release/crush-desktop-core"

for need in "$ELECTRON/electron" "$CORE" "$ROOT/dist/index.html"; do
  [ -e "$need" ] || { echo "missing $need: run tests/build.sh first" >&2; exit 1; }
done
if pgrep -x crush-desktop >/dev/null; then
  echo "Crush Desktop is running; quit it first" >&2
  exit 1
fi

# A fresh copy in a new folder, then swapped in, so a failed copy leaves the old one.
NEW="$LIB.new"
rm -rf "$NEW"
mkdir -p "$NEW"
cp -a "$ELECTRON/." "$NEW/"
mv "$NEW/electron" "$NEW/crush-desktop"
electron/stage.sh "$NEW/resources/app"
rm -rf "$LIB.old"
[ -d "$LIB" ] && mv "$LIB" "$LIB.old"
mv "$NEW" "$LIB"
rm -rf "$LIB.old"

# The launcher.
if [ -f "$BIN" ] && ! grep -q "crush-desktop/app/crush-desktop" "$BIN" 2>/dev/null; then
  mv "$BIN" "$BIN.before-electron"
fi
mkdir -p "$(dirname "$BIN")"
cat >"$BIN" <<EOS
#!/bin/sh
# Crush Desktop (Electron); installed by $ROOT/install.sh.
exec "$LIB/crush-desktop" "\$@"
EOS
chmod +x "$BIN"
echo "installed: $BIN -> $LIB"
"$BIN" --version
