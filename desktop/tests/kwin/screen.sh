#!/usr/bin/env bash
# Inside tests/sandbox.sh --kwin: a private session bus (session.conf) and a hidden KWin
# (kwin_wayland --virtual, nothing reaches the owner's screen) in place of
# Xvfb, then the command with WAYLAND_DISPLAY set. For checks of what only a
# real compositor does: window moves, snapping, keep-above, the window menu.
# KWin's own config and cache stay in the sandbox's throwaway home. KWin's
# log, with what check scripts print, goes to /tmp/kwin.log.
# Usage: tests/kwin/screen.sh WxH command args...
set -u
SIZE="$1"; shift
export XDG_SESSION_TYPE=wayland XDG_CURRENT_DESKTOP=KDE KDE_FULL_SESSION=true
exec dbus-run-session --config-file="$DESKTOP_ROOT/tests/kwin/session.conf" -- bash -c '
  SIZE="$1"; shift
  mkdir -p "$DESKTOP_ROOT/tests/out/kwin"
  QT_FORCE_STDERR_LOGGING=1 QT_LOGGING_RULES="*.debug=false;kwin_scripting.debug=true;js.debug=true;qml.debug=true" \
    kwin_wayland --virtual --no-lockscreen --width "${SIZE%x*}" --height "${SIZE#*x}" --socket wayland-check >"$DESKTOP_ROOT/tests/out/kwin/kwin.log" 2>&1 &
  kwin=$!
  for _ in $(seq 100); do [ -S "$XDG_RUNTIME_DIR/wayland-check" ] && break; sleep 0.1; done
  WAYLAND_DISPLAY=wayland-check "$@"
  code=$?
  kill $kwin 2>/dev/null; wait $kwin 2>/dev/null
  exit $code' screen "$SIZE" "$@"
