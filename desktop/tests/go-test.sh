#!/usr/bin/env bash
# Runs Crush's Go tests on the newest commit of the desktop branch.
# - In a separate checkout (~/dev/crush-desktop-verify) that no editor or agent
#   watches: a watched folder breaks the file-review tests.
# - Through systemd-run, outside the chat's command watcher, on the AI half of
#   the CPUs, as a named service with its own time limit, so a kill from
#   outside cannot leave the tests running.
# - With a throwaway home, config, data, cache, runtime and temp folder per
#   run, and no address for the real screen, Wayland or the session message
#   bus, so no test can touch the owner's files, show anything or make sound.
#   The outside-world check runs around every run.
# Only committed work is tested: commit on the desktop branch first.
# Usage: tests/go-test.sh [go test args...]   (default: ./...)
# GO_TEST_MAX_SEC sets the time limit (default 1800 seconds).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="$(cd "$HERE/../.." && pwd)"
VERIFY="${CRUSH_VERIFY_TREE:-$HOME/dev/crush-desktop-verify}"
MAX_SEC="${GO_TEST_MAX_SEC:-1800}"
commit="$(git -C "$SRC" rev-parse HEAD)"
[ -d "$VERIFY" ] || git -C "$SRC" worktree add --detach --quiet "$VERIFY" "$commit"
if [ -n "$(git -C "$VERIFY" status --porcelain --untracked-files=no)" ]; then
  echo "the verify checkout has changes; refusing to switch it" >&2; exit 2
fi
git -C "$VERIFY" checkout --detach --quiet "$commit"
stray="$(git -C "$VERIFY" ls-files --others --exclude-standard -- '*.go')"
if [ -n "$stray" ]; then
  echo "untracked Go files in the verify checkout would be tested too; move them out first:" >&2
  echo "$stray" >&2; exit 2
fi
git -C "$SRC" diff --quiet HEAD -- . ':!desktop' || echo "note: uncommitted Go changes are not tested (commit first)"

# A per-run folder holds the throwaway home and the tests' temp folder.
BASE=/var/tmp/crush-test-tmp
mkdir -p "$BASE"
RUN="$(mktemp -d "$BASE/run.XXXXXX")"
cleanup() {
  case "$RUN" in "$BASE"/run.*) chmod -R u+w "$RUN" 2>/dev/null || true; rm -rf -- "$RUN" ;; esac
}
trap cleanup EXIT
T_HOME="$RUN/home"
mkdir -p "$T_HOME/.config" "$T_HOME/.local/share" "$T_HOME/.cache" "$T_HOME/.local/state" "$RUN/tmp"
mkdir -m 0700 "$RUN/runtime"
# A .git in any folder above the tests' temp folder makes Crush treat every
# temp folder as part of a repository, which breaks the file-history tests.
# (/tmp/.git does not matter: the tests never use /tmp. Crush's read-only
# sandbox creates an empty /tmp/.git as a mount point.)
dir="$RUN"
while :; do
  if [ -e "$dir/.git" ]; then echo "a stray .git sits above the test temp folder: $dir/.git" >&2; exit 2; fi
  [ "$dir" = / ] && break
  dir="$(dirname "$dir")"
done

[ $# -gt 0 ] || set -- ./...
logs=/var/tmp/crush-desktop-go-logs; mkdir -p "$logs"
stamp="$(date +%Y%m%d-%H%M%S)"
log="$logs/go-test-$stamp.log"
unit="crush-desktop-go-test-$stamp-$$"
snap="$RUN/outside-before.json"
python3 -I "$HERE/outside-check.py" snapshot "$snap" >/dev/null
echo "testing $(git -C "$VERIFY" log --oneline -1) as unit $unit (limit ${MAX_SEC}s)"
set +e
systemd-run --user --wait --pipe --collect --quiet --unit="$unit" \
  -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec="$MAX_SEC" -p WorkingDirectory="$VERIFY" \
  -E PATH="$PATH" -E HOME="$T_HOME" \
  -E XDG_CONFIG_HOME="$T_HOME/.config" -E XDG_DATA_HOME="$T_HOME/.local/share" \
  -E XDG_CACHE_HOME="$T_HOME/.cache" -E XDG_STATE_HOME="$T_HOME/.local/state" \
  -E XDG_RUNTIME_DIR="$RUN/runtime" -E TMPDIR="$RUN/tmp" \
  -E DISPLAY= -E WAYLAND_DISPLAY= -E XAUTHORITY= -E ICEAUTHORITY= -E SESSION_MANAGER= \
  -E DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent \
  -E GOPATH="$(go env GOPATH)" -E GOCACHE="$(go env GOCACHE)" -E GOMODCACHE="$(go env GOMODCACHE)" \
  go test -count=1 "$@" >"$log" 2>&1
code=$?
set -e
grep -v -E '^(ok|\?) ' "$log" | tail -150 || true
outside=0
python3 -I "$HERE/outside-check.py" verify "$snap" >"$RUN/outside-after.txt" 2>&1 || outside=1
grep -v '^PASS' "$RUN/outside-after.txt" || true
echo "summary: $(grep -c '^ok ' "$log") packages passed, $(grep -c -E '^(FAIL|--- FAIL)' "$log") failure lines, go test exit $code, outside-world check $([ $outside -eq 0 ] && echo unchanged || echo CHANGED)"
echo "full log: $log"
[ $outside -eq 0 ] || exit 3
exit $code
