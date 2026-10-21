#!/usr/bin/env bash
# Crush Desktop's own updater (owner, 2026-10-20: "an update button ... that
# automatically installs new versions ... and restarts it"). It ships next to
# the helper (electron/stage.sh) and the helper runs it (src-tauri/src/
# appupdate.rs); its last output line is JSON.
#
#   update.sh check   {"current","latest","newer"}: the build this app runs
#                     (build.json beside it) against the newest commit of the
#                     fork's desktop branch.
#   update.sh run     {"ok","commit","error","log"}: a fresh copy of that
#                     branch in the cache (never the owner's checkout) is
#                     built (the screens, the helper, the terminal crush);
#                     only when every build passed are the app (install.sh,
#                     while it runs) and the terminal crush (old one backed
#                     up) installed. The app restarts itself afterwards.
#
# Settings (environment, for tests): CRUSH_UPDATE_REPO, CRUSH_UPDATE_BRANCH,
# CRUSH_UPDATE_WORK (cache folder), CRUSH_UPDATE_LIB (app install folder),
# CRUSH_UPDATE_BIN (app launcher), CRUSH_UPDATE_CRUSH (terminal crush to
# replace), CRUSH_UPDATE_BACKUPS, CRUSH_UPDATE_PROVIDERS (the local
# cli_providers.go the terminal crush is built with, when present).
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="${CRUSH_UPDATE_REPO:-https://github.com/braxten509/crush.git}"
BRANCH="${CRUSH_UPDATE_BRANCH:-desktop}"
STATE="${XDG_STATE_HOME:-$HOME/.local/state}"
WORK="${CRUSH_UPDATE_WORK:-${XDG_CACHE_HOME:-$HOME/.cache}/crush-desktop-update}"
LIB="${CRUSH_UPDATE_LIB:-$HOME/.local/lib/crush-desktop/app}"
BIN="${CRUSH_UPDATE_BIN:-$HOME/.local/bin/crush-desktop}"
CRUSH="${CRUSH_UPDATE_CRUSH:-$(command -v crush || echo "$HOME/.local/bin/crush")}"
BACKUPS="${CRUSH_UPDATE_BACKUPS:-$STATE/crush/binary-backups}"
PROVIDERS="${CRUSH_UPDATE_PROVIDERS:-$HOME/dev/crush/internal/config/cli_providers.go}"
SOURCE_DIR="${CRUSH_UPDATE_SOURCE_DIR:-$HOME/dev/crush}"

# An app started from the menu may lack the owner's shell paths (cargo).
export PATH="$HOME/.cargo/bin:$HOME/.local/bin:$PATH"
current() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("commit",""))' "$HERE/build.json" 2>/dev/null; }
latest() { git ls-remote "$REPO" "refs/heads/$BRANCH" 2>/dev/null | cut -f1; }

check() {
  local now new
  now="$(current)"
  new="$(latest)"
  if [ -z "$new" ]; then
    echo '{"error":"could not reach the fork on GitHub"}'
    return 1
  fi
  if [ -n "$now" ] && [ "$now" = "$new" ]; then
    echo "{\"current\":\"$now\",\"latest\":\"$new\",\"newer\":false}"
  else
    echo "{\"current\":\"$now\",\"latest\":\"$new\",\"newer\":true}"
  fi
}

fail() {
  python3 -c 'import json,sys; print(json.dumps({"ok": False, "error": sys.argv[1], "log": sys.argv[2]}))' "$1" "$LOG"
  exit 1
}

run() {
  mkdir -p "$WORK"
  LOG="$WORK/update.log"
  : >"$LOG"
  exec 9>"$WORK/lock"
  flock -n 9 || fail "an update is already running"
  local src="$WORK/src"
  rm -rf "$src"
  echo "== copy $REPO $BRANCH" >>"$LOG"
  git clone -q --depth 1 --branch "$BRANCH" "$REPO" "$src" >>"$LOG" 2>&1 || fail "could not copy the fork's $BRANCH branch"
  local commit
  commit="$(git -C "$src" rev-parse HEAD)"
  cd "$src/desktop" || fail "the copy has no desktop folder"
  echo "== screens" >>"$LOG"
  nice -n 10 pnpm install --frozen-lockfile >>"$LOG" 2>&1 || fail "installing the app's packages failed"
  # pnpm skips packages' own setup steps: Electron fetches its program here
  # (from its download cache when it has it).
  if [ ! -x node_modules/electron/dist/electron ]; then
    node node_modules/electron/install.js >>"$LOG" 2>&1 || fail "fetching Electron failed"
  fi
  nice -n 10 pnpm build >>"$LOG" 2>&1 || fail "building the screens failed"
  echo "== helper" >>"$LOG"
  CARGO_TARGET_DIR="$WORK/target" nice -n 10 cargo build --release --manifest-path src-tauri/Cargo.toml >>"$LOG" 2>&1 || fail "building the helper failed"
  mkdir -p src-tauri/target/release
  cp "$WORK/target/release/crush-desktop-core" src-tauri/target/release/
  echo "== terminal crush" >>"$LOG"
  [ -f "$PROVIDERS" ] && cp "$PROVIDERS" "$src/internal/config/cli_providers.go"
  local base version
  base="$("$CRUSH" --version 2>/dev/null | awk '{print $3}' | sed 's/-local.*//')"
  version="${base:-v0.0.0}-local.$(date +%Y%m%d)-${commit:0:8}"
  (cd "$src" && nice -n 10 go build -buildvcs=false -ldflags="-X github.com/charmbracelet/crush/internal/version.Version=$version -X github.com/charmbracelet/crush/internal/version.Commit=${commit:0:8} -X github.com/charmbracelet/crush/internal/version.SourceDir=$SOURCE_DIR" -o "$WORK/crush" .) >>"$LOG" 2>&1 || fail "building the terminal crush failed"

  # Every build passed: install the app (its old folder swapped out whole),
  # then the terminal crush (backed up first).
  echo "== install" >>"$LOG"
  CRUSH_DESKTOP_LIVE=1 CRUSH_DESKTOP_LIB="$LIB" CRUSH_DESKTOP_BIN="$BIN" CRUSH_DESKTOP_COMMIT="$commit" ./install.sh >>"$LOG" 2>&1 || fail "installing the app failed"
  local real old
  real="$(readlink -f "$CRUSH")"
  if [ -e "$real" ]; then
    old="$("$real" --version 2>/dev/null | awk '{print $3}')"
    mkdir -p "$BACKUPS"
    cp -p "$real" "$BACKUPS/crush-${old:-unknown}.$(date +%Y%m%d%H%M%S)" >>"$LOG" 2>&1 || fail "could not back up the terminal crush"
  fi
  cp "$WORK/crush" "$real.new" && mv "$real.new" "$real" || fail "could not install the terminal crush"
  echo "{\"ok\":true,\"commit\":\"$commit\",\"version\":\"$version\",\"log\":\"$LOG\"}"
}

case "${1:-}" in
  check) check ;;
  run) run ;;
  *) echo "usage: update.sh check|run" >&2; exit 2 ;;
esac
