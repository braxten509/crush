#!/usr/bin/env bash
# Inside the sandbox: start the fake AI server, make the throwaway project and
# start the app on it, then run a driver.
# Usage (through tests/sandbox.sh): tests/run-in-sandbox.sh [--no-app] DRIVER.py [args...]
# --no-app: everything as below except starting the app; the driver starts it
# through tests/driver/apps.py's App (desktop-design-5-6 8.2).
# The project is tests/project/ copied to /tmp/home/proj, with git init and a
# crush.json that points at the fake AI server. The app gets --probe, --cwd
# and $DESKTOP_ARGS (for example --fixture FILE); CRUSH_DESKTOP_CRUSH (when
# DESKTOP_CRUSH is set) and a random CRUSH_DESKTOP_ENV_MARK are set for the
# app alone, never for the driver. The mark is saved in tests/out/<driver>/env-mark
# for the leak check (desktop-design 2.9); the app's output in tests/out/<driver>/app.log.
set -u
ROOT="$DESKTOP_ROOT"
NO_APP=""
if [ "${1:-}" = "--no-app" ]; then NO_APP=1; shift; fi
DRIVER="$1"; shift
OUT="$ROOT/tests/out/$(basename "$DRIVER" .py)"; mkdir -p "$OUT"
APP="${DESKTOP_APP:-$ROOT/src-tauri/target/release/crush-desktop}"
python3 -I "$ROOT/tests/fakes/fake_ai_server.py" --port-file /tmp/fake-ai.port >/tmp/fake-ai.out 2>&1 &
srv=$!
for _ in $(seq 50); do [ -s /tmp/fake-ai.port ] && break; sleep 0.1; done
PORT="$(cat /tmp/fake-ai.port 2>/dev/null)"

PROJ=/tmp/home/proj
mkdir -p "$PROJ"
tar -C "$ROOT/tests/project" --exclude=crush.json.in -cf - . | tar -C "$PROJ" -xf -
sed "s/@PORT@/$PORT/" "$ROOT/tests/project/crush.json.in" >"$PROJ/crush.json"
git -C "$PROJ" init -q

# The hidden screen's keymap gives Ctrl+Alt+F12 to the X server's console
# switch, which the server keeps, so the page never sees the probe's
# frame-meter key. Make F12 plain F12 at every level on this screen alone.
for code in $(xmodmap -pke | awk '/= F12/ {print $2}'); do
  xmodmap -e "keycode $code = F12 F12 F12 F12 F12 F12 F12"
done

MARK="leak-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')"
printf '%s\n' "$MARK" >"$OUT/env-mark"
app=""
if [ -z "$NO_APP" ]; then
  # $DESKTOP_ARGS is split into words on purpose (paths without spaces).
  # shellcheck disable=SC2086
  env ${DESKTOP_CRUSH:+"CRUSH_DESKTOP_CRUSH=$DESKTOP_CRUSH"} "CRUSH_DESKTOP_ENV_MARK=$MARK" \
    "$APP" --probe /tmp/probe.json --cwd "$PROJ" ${DESKTOP_ARGS:-} >"$OUT/app.log" 2>&1 &
  app=$!
fi
python3 -I "$ROOT/tests/driver/$(basename "$DRIVER")" /tmp/probe.json "$OUT" "$PORT" "$@"
code=$?
kill $app $srv 2>/dev/null; wait 2>/dev/null
exit $code
