# Crush Desktop

A desktop app for Crush (Tauri 2 + Svelte + Vite), for Linux and Mac. It
shares chats with the terminal app. The plan is in [PLAN.md](PLAN.md), the
feature checklist in [CHECKLIST.md](CHECKLIST.md), the design system in
[DESIGN.md](DESIGN.md), and the product record the design tools read in
[PRODUCT.md](PRODUCT.md). Steps that were skipped or left open are listed in
[DEBUGGING.md](DEBUGGING.md).

## Build

Needs Node with pnpm, Rust, and on Linux the WebKitGTK 4.1 development files.

```sh
tests/build.sh        # pnpm install, front-end build, release app build
```

The app lands at `src-tauri/target/release/crush-desktop`. It runs the
`crush` found on the login shell's PATH (or the `crush_path` setting), and
shares each project's engine with terminal windows of the same `crush`.

The Mac app is built on GitHub (`.github/workflows/desktop-mac.yml`, on a push
to `main` or `desktop`): a dmg with the app, and the Mac `crush` to put on
PATH. `src-tauri/tauri.macos.conf.json` turns bundling on for the Mac.

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
- `tests/sandbox.sh -s 1700x1100 tests/run-in-sandbox.sh look_real.py`
  captures the real window (WebKitGTK, what the app ships with) at the
  approved picture's size, 1600 x 992, at the default zoom and at picture
  size, into `tests/out/look_real/`. Build first (`tests/build.sh`).
- `tests/look/align_real.py IMAGE --zoom 8/12 --out DIR` measures alignment
  on such a capture with the align skill's checker: the boxes come from the
  same layout in headless Chromium (`tests/look/align-marks.js` names what is
  measured), the pixels from the real window. `tests/look/capture.py`
  captures the Chromium page itself. It only works on the first screen of the
  design mock; the check below replaces it for the real app.
- `tests/look/align_app.py prepare`, then `tests/check.sh look_align.py`, then
  `tests/look/align_app.py measure` measures alignment on the first screen
  (with a seeded chat) and every dialog that opens without arguments, all in
  the real window: the boxes come from the WebKit page itself (the probe runs
  the align skill's collector in the window, test runs only), the pixels from
  the window's picture. `tests/look/app-marks.js` names what is measured; the
  results are in `tests/out/look_align/summary.json` and `measure/`.
- `tests/go-test.sh [packages]` runs Crush's Go tests on the newest commit, in
  a separate checkout (`~/dev/crush-desktop-verify`), as a named
  `systemd-run` service with its own time limit (`GO_TEST_MAX_SEC`, default
  1800), a throwaway home and temp folder, and no address for the real screen
  or message bus. It runs the outside-world check around the tests and exits
  3 if any real Crush file changed. Commit first: uncommitted work is not
  tested. `CRUSH_ENGINE=shared` or `alone` runs the suite in either engine
  mode.

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
