#!/usr/bin/env bash
# Runs a command inside a sandbox with a hidden screen, so nothing reaches the
# owner's desktop:
# - the whole computer is read-only; only tests/out/ is writable;
# - the owner's home folder is hidden; only this worktree shows (read-only),
#   so none of the owner's own files are even readable;
# - own /tmp and /run (no Wayland, sound, D-Bus session or system bus
#   sockets), a bare /dev (no sound card or GPU), no network except the
#   sandbox's own loopback, own process list, shared memory, host name and
#   terminal session;
# - a throwaway HOME and XDG folders under /tmp/home;
# - the fake AI tools in tests/fakes/bin come first on PATH, then, when
#   DESKTOP_CRUSH names a file called crush, its folder (tests/out/bin), so an
#   agent's plain `crush` finds the test build (desktop-design-5-6 8.4);
# - a clean environment (env -i), so no CRUSH_* variables leak in from a chat;
#   the only settings passed through are DESKTOP_APP (another app build to run),
#   DESKTOP_CRUSH (the crush binary the app should use) and DESKTOP_ARGS (more
#   arguments for the app, such as --fixture FILE).
# - the hidden screen runs with -noreset, so a key map change made by a short
#   client (xmodmap in run-in-sandbox.sh) survives that client exiting.
# Usage: tests/sandbox.sh [-s WxH] command args...
set -euo pipefail
SIZE=1600x1000
if [ "${1:-}" = "-s" ]; then SIZE="$2"; shift 2; fi
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKTREE="$(dirname "$ROOT")"
mkdir -p "$ROOT/tests/out"
uid="$(id -u)"
SANDBOX_PATH="$ROOT/tests/fakes/bin"
if [ -n "${DESKTOP_CRUSH:-}" ] && [ "$(basename "$DESKTOP_CRUSH")" = crush ]; then
  SANDBOX_PATH="$SANDBOX_PATH:$(dirname "$DESKTOP_CRUSH")"
fi
exec bwrap --ro-bind / / --dev /dev --proc /proc \
  --tmpfs /tmp \
  --tmpfs /run --dir /run/user --perms 0700 --tmpfs "/run/user/$uid" \
  --tmpfs "$HOME" --ro-bind "$WORKTREE" "$WORKTREE" \
  --bind "$ROOT/tests/out" "$ROOT/tests/out" \
  --unshare-net --unshare-pid --unshare-ipc --unshare-uts --new-session --die-with-parent \
  -- env -i \
  PATH="$SANDBOX_PATH:$PATH" \
  HOME=/tmp/home USER="${USER:-tester}" LANG="${LANG:-C.UTF-8}" TERM=xterm-256color \
  XDG_CONFIG_HOME=/tmp/home/.config XDG_DATA_HOME=/tmp/home/.local/share \
  XDG_CACHE_HOME=/tmp/home/.cache XDG_STATE_HOME=/tmp/home/.local/state \
  XDG_RUNTIME_DIR="/run/user/$uid" XDG_SESSION_TYPE=x11 GDK_BACKEND=x11 \
  WEBKIT_DISABLE_COMPOSITING_MODE=1 WEBKIT_DISABLE_DMABUF_RENDERER=1 \
  FAKE_TOOL_LOG=/tmp/fake-tools.jsonl FAKE_AI_LOG=/tmp/fake-ai.jsonl \
  DESKTOP_ROOT="$ROOT" OUTER_HOME="$HOME" ${DESKTOP_APP:+"DESKTOP_APP=$DESKTOP_APP"} \
  ${DESKTOP_CRUSH:+"DESKTOP_CRUSH=$DESKTOP_CRUSH"} ${DESKTOP_ARGS:+"DESKTOP_ARGS=$DESKTOP_ARGS"} \
  bash -c 'mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME" && exec xvfb-run -a -s "-screen 0 '"$SIZE"'x24 -nolisten tcp -noreset" "$@"' sandbox "$@"
