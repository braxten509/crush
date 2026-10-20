# Crush Desktop

A desktop app for Crush (Electron + Svelte + Vite, with a Rust helper
program), for Linux and Mac. It shares chats with the terminal app. The plan is in [PLAN.md](PLAN.md), the
feature checklist in [CHECKLIST.md](CHECKLIST.md), the design system in
[DESIGN.md](DESIGN.md), and the product record the design tools read in
[PRODUCT.md](PRODUCT.md). Steps that were skipped or left open are listed in
[DEBUGGING.md](DEBUGGING.md).

## Build

Needs Node with pnpm and Rust. pnpm installs Electron.

```sh
tests/build.sh        # pnpm install, front-end build, release helper build
electron/crush-desktop   # runs the build from this folder
./install.sh          # installs it for this user (quit the app first)
```

The window is Electron (`electron/main.cjs`, with `electron/preload.cjs` giving
the page `window.crushBridge`). It starts the Rust helper
`src-tauri/target/release/crush-desktop-core`, which does the app's work and
talks to Electron in JSON lines over its stdin and stdout (the messages are
listed in `src-tauri/src/host.rs`, the page's commands in `dispatch.rs`). The
page still imports `@tauri-apps/api/*`; `vite.config.js` points those names at
`src/lib/bridge/`. The app runs the `crush` found on the login shell's PATH
(or the `crush_path` setting), and shares each project's engine with terminal
windows of the same `crush`.

`install.sh` puts Electron and the app in `~/.local/lib/crush-desktop/app/`
(`electron/stage.sh` copies the app's own files into `resources/app/`) and a
launcher at `~/.local/bin/crush-desktop`.

The Mac app is built on GitHub (`.github/workflows/desktop-mac.yml`, on a push
to `main` or `desktop`): Electron packaged with `@electron/packager`
(`--no-asar`, so the helper can run) into a dmg, and the Mac `crush` to put on
PATH.

## Check

Every check runs on a hidden screen inside a sandbox, so nothing shows on the
real desktop and nothing makes a sound:

- `tests/sandbox.sh CMD...` runs a command with a read-only computer (only
  `tests/out/` is writable), the owner's home hidden except this worktree, a
  bare `/dev`, its own `/tmp` and `/run` (no message buses, Wayland or sound
  sockets), its own network (loopback only), process list, shared memory,
  host name and terminal session, a throwaway home, a clean environment, a
  hidden X screen, and the fake AI tools from `tests/fakes/bin` first on
  `PATH`. `DESKTOP_APP=PATH` runs another build of the app (it must be
  inside this worktree: the sandbox hides the rest of the home folder).
  Drivers find the window by its class, `crush-desktop`.
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
  captures the real window (Electron, what the app ships with) at the
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
  the real window: the boxes come from the app's page itself (the probe runs
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

The window only runs the app's own scripts: `electron/main.cjs` serves the
page from `app://localhost` with its content rule (`CSP`), refuses every
navigation away and every new window, and records refused navigations. Keep
`index.html` free of inline scripts; `style="..."` attributes are allowed
(`style-src 'unsafe-inline'`).

`tests/driver/electron_shell.py` checks the parts only Electron has: the
clipboard, the helper's events, the window title, the taskbar marks, the
navigation guard and the reload after a crash.

## Fakes

- `tests/fakes/fake_ai_server.py`: an OpenAI-style server with scripted
  replies (`--script FILE`), streaming, and a request log (`FAKE_AI_LOG`).
- `tests/fakes/fake-tool`: stands in for `claude`, `codex`, `grok`,
  `opencode` and `agy`; logs each call to `FAKE_TOOL_LOG`.
