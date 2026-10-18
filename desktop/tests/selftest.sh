#!/usr/bin/env bash
# Phase-0 harness self-test. Passes only if:
#   1. the normal self-test passes (fake AI server reachable inside the sandbox,
#      fake tools first on PATH, the real app window opens and reports itself);
#   2. the same run with one deliberately wrong check FAILS;
#   3. the owner's real Crush files are unchanged afterwards (tests/outside-check.py:
#      content hashes for the config and project-list files, size and date for
#      every other file in the two real Crush folders).
set -u
cd "$(dirname "$0")/.."
mkdir -p tests/out
snap=tests/out/outside-before.json
python3 -I tests/outside-check.py snapshot "$snap"
ok=0
tests/sandbox.sh tests/run-in-sandbox.sh selftest.py > tests/out/selftest-normal.log 2>&1
normal=$?
if [ $normal -eq 0 ] && grep -q '^ALL PASSED' tests/out/selftest-normal.log; then
  echo "PASS normal self-test passes"; else echo "FAIL normal self-test (exit $normal)"; ok=1; fi
grep -E '^(PASS|FAIL)' tests/out/selftest-normal.log | sed 's/^/  /'
tests/sandbox.sh tests/run-in-sandbox.sh selftest.py --broken > tests/out/selftest-broken.log 2>&1
broken=$?
if [ $broken -ne 0 ] && grep -q '^FAIL deliberately broken check' tests/out/selftest-broken.log; then
  echo "PASS deliberately broken check is reported as a failure (exit $broken)"; else echo "FAIL broken run did not fail (exit $broken)"; ok=1; fi
python3 -I tests/outside-check.py verify "$snap" > tests/out/outside-after.log 2>&1 || ok=1
sed 's/^/  /' tests/out/outside-after.log
[ $ok -eq 0 ] && echo "SELFTEST OK" || echo "SELFTEST FAILED"
exit $ok
