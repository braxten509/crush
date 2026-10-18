#!/usr/bin/env bash
# Builds the engine (the crush binary) from this worktree into
# desktop/tests/out/bin/crush, where tests/check.sh looks for it
# (desktop-design 6.1). Run it through systemd-run, like tests/build.sh.
set -euo pipefail
cd "$(dirname "$0")/../.."
mkdir -p desktop/tests/out/bin
go build -o desktop/tests/out/bin/crush .
ls -la desktop/tests/out/bin/crush
