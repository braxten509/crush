#!/usr/bin/env bash
# Builds the engine (the crush binary) from this worktree into
# desktop/tests/out/bin/crush, where tests/check.sh looks for it
# (desktop-design 6.1). Run it through systemd-run, like tests/build.sh.
# The crushtest tag carries the test seams (desktop-design-5-6 9.1, G-21 to
# G-24); with CRUSH_TEST_SEAMS unset the binary behaves as a normal build.
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p desktop/tests/out/bin
go build -tags crushtest -o desktop/tests/out/bin/crush .
ls -la desktop/tests/out/bin/crush
