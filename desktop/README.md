# Crush Desktop

A desktop app for Crush (Tauri 2 + Svelte + Vite), for Linux and Mac. It
shares chats with the terminal app. The plan is in [PLAN.md](PLAN.md), the
feature checklist in [CHECKLIST.md](CHECKLIST.md), and the product record the
design tools read in [PRODUCT.md](PRODUCT.md).

## Build

Needs Node with pnpm, Rust, and on Linux the WebKitGTK 4.1 development files.

```sh
tests/build.sh        # pnpm install, front-end build, release app build
```

The app lands at `src-tauri/target/release/crush-desktop`.

## Check

Every check runs on a hidden screen inside a sandbox, so nothing shows on the
real desktop and nothing makes a sound:

- `tests/sandbox.sh CMD...` runs a command with a read-only computer (only
  `tests/out/` is writable), the owner's home hidden except this worktree, a
  bare `/dev`, its own `/tmp` and `/run` (no message buses, Wayland or sound
  sockets), its own network (loopback only), process list, shared memory,
  host name and terminal session, a throwaway home, a clean environment, a
  hidden X screen, and the fake AI tools from `tests/fakes/bin` first on
  `PATH`. `DESKTOP_APP=PATH` runs another build of the app.
- `tests/run-in-sandbox.sh DRIVER.py` (inside the sandbox) starts the fake AI
  server and the app with `--probe`, then runs a driver from `tests/driver/`.
  The app writes what is on its screen to the probe file every 200 ms;
  drivers read it and use the real mouse and keyboard through `xdotool`.
- `tests/selftest.sh` proves the harness: the sandbox is sealed, the fake AI
  server answers inside it, the app window opens at full size, a deliberately
  wrong check fails, and the owner's real Crush files stay unchanged
  (`tests/outside-check.py` compares fingerprints, never contents).
- `pnpm test` runs the front-end unit tests.
- `tests/go-test.sh [packages]` runs Crush's Go tests on the newest commit, in
  a separate checkout (`~/dev/crush-desktop-verify`), as a named
  `systemd-run` service with its own time limit (`GO_TEST_MAX_SEC`, default
  1800), a throwaway home and temp folder, and no address for the real screen
  or message bus. It runs the outside-world check around the tests and exits
  3 if any real Crush file changed. Commit first: uncommitted work is not
  tested.

Run checks in the background with a time limit, for example
`crush bg -- 'timeout 300 bash tests/selftest.sh'`.

## Content rule

The window only runs the app's own scripts (`tauri.conf.json`, `csp`). Keep
`index.html` free of inline `<style>` tags: Tauri would add a nonce to
`style-src`, which turns off `'unsafe-inline'` and breaks `style="..."`
attributes.

## Fakes

- `tests/fakes/fake_ai_server.py`: an OpenAI-style server with scripted
  replies (`--script FILE`), streaming, and a request log (`FAKE_AI_LOG`).
- `tests/fakes/fake-tool`: stands in for `claude`, `codex`, `grok`,
  `opencode` and `agy`; logs each call to `FAKE_TOOL_LOG`.
