#!/usr/bin/env bash
# Runs one window driver in the sandbox (desktop-design 6.1).
# Usage: tests/check.sh [--no-app] [--fixture FILE] [--size WxH] DRIVER.py [driver args]
# --no-app goes on to tests/run-in-sandbox.sh: the driver starts the app
# itself (tests/driver/apps.py, desktop-design-5-6 8.2). Without --fixture the app joins the engine built by tests/build-crush.sh;
# with it, the app plays the recorded engine in FILE (6.3). The owner's real
# Crush files are fingerprinted before and checked after (tests/outside-check.py).
# Exits with the driver's code (or 1 when the outside check fails); the
# driver's PASS/FAIL lines are also in tests/out/<driver>/check.log, beside
# its screenshots.
set -u
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
FIXTURE=""
SIZE=""
NO_APP=""
while [ $# -gt 0 ]; do
  case "$1" in
    --fixture) FIXTURE="$(realpath "$2")"; shift 2 ;;
    --size) SIZE="$2"; shift 2 ;;
    --no-app) NO_APP="--no-app"; shift ;;
    *) break ;;
  esac
done
if [ $# -lt 1 ]; then
  echo "usage: tests/check.sh [--no-app] [--fixture FILE] [--size WxH] DRIVER.py [driver args]" >&2
  exit 2
fi
DRIVER="$1"; shift
APP="$ROOT/electron/crush-desktop"
# CHECK_CRUSH lets a driver run the app with another crush, such as tests/fakes/crush-wrap.
CRUSH="${CHECK_CRUSH:-$ROOT/tests/out/bin/crush}"
if [ ! -x "$APP" ]; then echo "FAIL no app build at $APP (run tests/build.sh)"; exit 1; fi
if [ -z "$FIXTURE" ] && [ ! -x "$CRUSH" ]; then echo "FAIL no engine build at $CRUSH (run tests/build-crush.sh)"; exit 1; fi
if [ -n "$FIXTURE" ] && [ ! -f "$FIXTURE" ]; then echo "FAIL no fixture at $FIXTURE"; exit 1; fi

NAME="$(basename "$DRIVER" .py)"
OUT="$ROOT/tests/out/$NAME"
mkdir -p "$OUT"
snap="$OUT/outside-before.json"
python3 -I tests/outside-check.py snapshot "$snap"

# CHECK_ARGS adds app arguments, such as -s CHAT to open a chat.
export DESKTOP_ARGS="${FIXTURE:+--fixture $FIXTURE}${CHECK_ARGS:+ $CHECK_ARGS}"
if [ -x "$CRUSH" ]; then export DESKTOP_CRUSH="$CRUSH"; fi
tests/sandbox.sh ${SIZE:+-s "$SIZE"} tests/run-in-sandbox.sh $NO_APP "$DRIVER" "$@" 2>&1 | tee "$OUT/check.log"
code=${PIPESTATUS[0]}

if ! python3 -I tests/outside-check.py verify "$snap" >"$OUT/outside-after.log" 2>&1; then
  echo "FAIL the owner's Crush files changed during the run (tests/out/$NAME/outside-after.log)"
  [ "$code" -eq 0 ] && code=1
fi
exit "$code"
