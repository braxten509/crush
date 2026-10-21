#!/usr/bin/env bash
# Puts the app's own files into DEST: main.cjs, preload.cjs, package.json,
# the screens (dist/), the helper (crush-desktop-core), the icon and the
# updater (update.sh). DEST is
# Electron's resources/app for the Linux install (install.sh), or the folder
# @electron/packager wraps into the Mac app (.github/workflows/desktop-mac.yml).
# Build first: pnpm build, and cargo build --release in src-tauri.
set -euo pipefail
cd "$(dirname "$0")/.."
DEST="$1"
for need in dist/index.html src-tauri/target/release/crush-desktop-core; do
  [ -e "$need" ] || { echo "missing $need: build first" >&2; exit 1; }
done
mkdir -p "$DEST"
cp electron/main.cjs electron/preload.cjs electron/package.json "$DEST/"
rm -rf "$DEST/dist"
cp -R dist "$DEST/dist"
cp src-tauri/target/release/crush-desktop-core "$DEST/crush-desktop-core"
cp src-tauri/icons/icon.png "$DEST/icon.png"
cp update.sh "$DEST/update.sh"
