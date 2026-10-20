#!/usr/bin/env bash
# Builds Crush Desktop: installs packages, builds the screens (Vite), then the
# helper program (cargo, release). electron/crush-desktop starts the result.
# Run it through systemd-run (see README.md).
set -euo pipefail
cd "$(dirname "$0")/.."
pnpm install --prefer-offline --reporter=append-only
pnpm build
cargo build --release --manifest-path src-tauri/Cargo.toml
ls -la src-tauri/target/release/crush-desktop-core node_modules/electron/dist/electron
