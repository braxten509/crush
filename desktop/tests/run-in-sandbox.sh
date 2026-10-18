#!/usr/bin/env bash
# Inside the sandbox: start the fake AI server and the app, then run a driver.
# Usage (through tests/sandbox.sh): tests/run-in-sandbox.sh DRIVER.py [args...]
set -u
ROOT="$DESKTOP_ROOT"
DRIVER="$1"; shift
OUT="$ROOT/tests/out/$(basename "$DRIVER" .py)"; mkdir -p "$OUT"
APP="${DESKTOP_APP:-$ROOT/src-tauri/target/release/crush-desktop}"
python3 -I "$ROOT/tests/fakes/fake_ai_server.py" --port-file /tmp/fake-ai.port >/tmp/fake-ai.out 2>&1 &
srv=$!
for _ in $(seq 50); do [ -s /tmp/fake-ai.port ] && break; sleep 0.1; done
"$APP" --probe /tmp/probe.json >/tmp/app.log 2>&1 &
app=$!
python3 -I "$ROOT/tests/driver/$(basename "$DRIVER")" /tmp/probe.json "$OUT" "$(cat /tmp/fake-ai.port 2>/dev/null)" "$@"
code=$?
kill $app $srv 2>/dev/null; wait 2>/dev/null
cp /tmp/app.log "$OUT/app.log" 2>/dev/null
exit $code
