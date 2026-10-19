# Skipped and partly done steps

What the build process (PLAN.md, the ultrabuild review and test rules) called for but was skipped, cut short or left open. Newest first. Each item says what is missing and where it would be done. Details and decisions stay in PLAN.md; this file is the to-do list of gaps.

## Phase 7, terminal switch-over (engine design 5.13), 2026-10-19

Written by the coordinator (Claude Opus 5.5, high effort), in the same way as waves 2 and 3. Four tests were written (`internal/cmd/workspace_setup_test.go`: alone mode takes the shared lock, alone mode starts no engine, shared mode uses ensure, CRUSH_CLIENT_SERVER still decides), and the whole Go suite runs in both modes (`CRUSH_ENGINE=shared` and `alone` through `tests/go-test.sh`).

- **Not one code path.** The design asks the terminal UI to reach jobs, helpers and usage limits only through the workspace. Instead a window that works alone still calls the agent globals exactly as before, and only a shared window goes through the engine (`internal/ui/common/livework.go`). This keeps "go back" identical to today's behavior, but the two paths can drift.
- **No deliberate breaks, no review yet.** The four tests were not broken on purpose to show they can fail. The phone sharing, secret-entry and limits paths of a shared window have no tests; the final review reads them.
- **Shared window gaps, left as they are:**
  - A secret-entry form that is already open for a chat is not fetched when the window switches to that chat. Only forms that open after the switch show.
  - Prompt alerts in a shared window use the engine's texts, not the terminal's own.
  - An engine started by the phone launcher's window may inherit `remote.LaunchEnv` from that window.
  - The window's own phone requests (new chat, yolo, mode) run in the window. Everything else the phone asks runs in the engine, so a busy window can't refuse those.
- **Mac build path, not yet run.** `.github/workflows/desktop-mac.yml` builds the Mac `crush`, runs `cargo test` and builds the app and dmg on `macos-latest`; it first runs on the push after phase 7. The Mac title bar is the standard one: the design's Mac buttons over the left bar (style-2) were not built, because the page has no room for them and nothing here can check them. The `.icns` icon is made from the 512 px PNG (the 1024 px slot is that image scaled up). `GOOS=darwin go build ./...` passes; the Rust side has no Mac compile check here (no Mac target or SDK).
- **Alignment pass.** `tests/look/align_real.py` (Chromium boxes on the design mock) is replaced for the real app by `align_app.py` with `driver/look_align.py`, which reads the boxes from the real WebKit page through a probe-only `probe_eval` command. The old script and `align-marks.js` are left in place (deleting them needs the owner's OK). Dialogs not measured: reasoning and projects (they close at once with one model without levels and one project), auth_method (needs a provider state), changes (needs a message's tool calls).
- **Alignment results (2026-10-19, real window, `tests/out/look_align/summary.json`):** at picture size (12/12) the first screen and the Sessions sheet pass every check. At the default Compact zoom (8/12) three icons measure 1 pixel off their row's middle (the chat list's search icon and every filter field's icon 1 low, the model picker's arrow 1 high): their boxes are centered, and the offset comes from 50- and 20-unit sizes landing on thirds of a pixel. Left as they are (a fix would have to move them per zoom step). Sheets with only text rows report "no relationships to measure" rather than a pass; the Settings, Models, Commands, Kept skills, Skills, Theme and Notifications sheets pass. Found by eye and fixed: the Quit sheet drew an empty title band (a sheet with no title and no controls now has none).
- **The "go back" setting has no screen.** `options.engine` is set in crush.json or through `CRUSH_ENGINE`. The desktop app's settings screen does not show it, because the desktop app always uses the engine.

## Final reviews (MiniMax t96, Sonnet 5.5 t98), 2026-10-19

Fixed:
- `crush logout` started the project's engine once shared became the default; it now only joins a running engine (`joinServer`, design 5.2). Covered by the updated `TestExplicitHostKeepsLegacyServer`, which now runs in a shared setting.
- Windows defaults to working alone (the engine does not run there yet); an explicit setting still wins. A `CRUSH_ENGINE` value that is not shared or alone now logs a warning.
- Mac build: the opener test that checks streams through `/proc` runs on Linux only, so `cargo test` can pass on macOS.
- `api` uppercases the method before the key redaction, so `get`/`post` can no longer skip it.
- A phone message into a shared window's new chat reports the new chat to the engine before answering, so the engine's "switched to another chat" check no longer races it; an empty new chat fails cleanly instead of crashing.
- A share answer that is neither setup nor on is shown as "sharing did not start" instead of being taken as on.
- Smaller: FilterList counts rows below the view only when capped; the Quit sheet has a screen-reader name (`Sheet` `label`); a misplaced comment in `engine/commands.js`; stale comments on logout.

Checked and not changed:
- FilterList's cap does not change when scrolled (both edges move together); the Global "enabled" label in MCP toggles matches the terminal (`toggles.go:312-328`); the Mac download expects `crush` on PATH, as the README says.

Left open:
- `crush login` in a shared window starts the engine, like any other window (the design names only logout as join-only).
- Secret copies not wiped: the Go string in `secure_remote.go` (the JSON wire format needs strings) and the request buffer `uds_http.rs` builds; the `connection.rs` comment says all copies are wiped.
- In a shared window the secret form's submit blocks the terminal for up to 30 s if the engine hangs, and a submit error shows under whichever field comes first (the server's field name is not passed back through the client).
- Phone sharing in a shared window: presence changes are sent one goroutine each and can arrive out of order; after an engine restart the window still shows REMOTE until the next status; a share that comes up just as a cancel errors is left running; a stale stop can arrive after a newer start.
- The background row can show the old chat's jobs for up to 1 s after a switch; a shared window counts services as unfinished work in the session host.
- Mac, unconfirmed until the first Mac run: PTY tabs use `poll(2)` on the PTY, which macOS may not support for devices (a tab would close at once; fix would be `select`); socket paths in tests may pass macOS's 104-byte limit; the workflow's pnpm and unpinned Tauri CLI versions.

## Tests added later for waves 2 and 3 (2026-10-19, owner-approved plan)

Risk-based only (logic that could break unnoticed), 36 checks in 9 files, each file shown to fail once on a deliberate break and then restored: secret hiding in `commands.rs` (`redact_answer`, 3 new Rust tests beside the scrub test), question-form answers (`ask/form.test.js`), theme color math and readability (`themes/tokens.test.js`), model picker groups (`models/groups.test.js`), custom command arguments (`palette/customcommands.test.js`), Sessions ages and project rule (`sessions/match.test.js`), log lines (`system/loglines.test.js`), picture picker rules (`pictures/picker.test.js`), and the two fixed phone-sharing bugs (`remote/share.test.js`). No app code changed; no real bug found (two first failures were test mistakes: a provider not built the way the app reads it, and a promise about argument values the terminal does not keep either). Still without tests, on purpose: glue, wording, layout, alerts, badge, title, settings setters, sign-in mapping, and FilterList's row cap (jsdom measures no layout).

## Phases 5-6, wave 2 (PAL, MOD, AUTH, SES, ASK), 2026-10-19

Owner instruction: the coordinator builds wave 2 itself, with no sub-agents, **no tests and no ultrabuild process** (no plan review, no independent review, no deliberate breaks, no window drivers, no look checks).

- **Written by Claude Opus at high effort, not max.** The plan called for max effort on brand-new screens; wave 2 was written by the coordinator (Claude Opus 5.5) running at high effort.
- **No tests at all.** None of the design's `u:`, `r:` or `w:` checks for PAL, MOD, AUTH, SES or ASK were written. The only evidence is that the existing unit tests, `cargo test` and `pnpm build` still pass.
- **Reviewed only after it was written.** No reviewer read wave 2 while it was built; three Sol 6.1 high reviews (t87, t88, t89) read it afterwards (below).
- **No builder report.** No row-by-row list of what each piece covers or where it differs from the design; whatever the coordinator records in PLAN.md is all there is.
- **P4.4** (tree and restore dialogs) still waits; it was not part of this request.
- **Places it differs from the design's file plan** (not checked by anyone):
  - `lib/dialogs.js` (S3's file) got `dialogRegistered(id)`, so the palette hides entries and keys whose sheet is not built yet.
  - AUTH added `components/signin/SignOut.svelte` (the sign-out confirm sheet), not in the file list.
  - ASK's parts are fewer files than listed: `QuestionForm.svelte` draws every question type, the tabs and the Confirm page; `Picture.svelte`, `SecureField.svelte` and `SecureForm.svelte` are the rest. `askYesNo` is built on the same form (slot kind `update`).
  - Not done: the question arrival sound and notification (2.6, 2.18) wait for SYS (wave 3). Multi choice preselection waits for G-13 (the engine sends no `selected` list). The narrow-window "N/M" tab label is not built.
  - Known leftover: LIVE's Ctrl+C binding takes the key in the helper view even with no selection, so the quit sheet does not open there.
- **Checks that did run:** `pnpm test` 83 files / 1889 tests, `pnpm build` with no warnings, `cargo test` 98. None of them exercise wave 2's code.
- **Review t87 (Codex Sol 6.1 high) and the coordinator's fixes**, all 16 fixed by the coordinator with no new tests:
  - High: provider keys and sign-in tokens reached the page in GET config (now cut in Rust before the answer crosses, `commands.rs` `redact_answer`, which also drops MCP/LSP env, headers and commands, and the catalog's key templates); a secret could be pasted into a sheet covering a secure form (the form now drops every paste and middle click while a sheet is open); palette prompts ran as shell commands in "!" mode (new `sendPromptText` in `composer.svelte.js` sends them as prompts and leaves the draft alone); a cancelled MCP prompt could still send (fetches now carry a ticket, and closing the palette or the arguments sheet cancels).
  - Medium: `projects.json` temp file shared between writers (now a unique name, created fresh, and one write at a time); Esc during a slow sign-in start left the flow running; re-authentication ignored the coder model (API-key users got the subscription sheet); question choices ignored hover and the "Something else?" focus; an erased answer was kept; a pasted secret was changed (line breaks to spaces); unavailable secure fields blocked the submit; an old secure-forms list could reopen a closed form; Sessions rename/delete could act on another row.
  - Low: tabs of different heights moved the chat (pages now share one grid cell); the palette needed a double click (KIT's `FilterList` got a `hoverRuns` option).
  - Files outside wave 2's own folders touched by these fixes: `src-tauri/src/commands.rs` (S5), `src/lib/stores/composer.svelte.js` (P3.7), `src/components/kit/FilterList.svelte` (KIT).
- **Second review t88 (Codex Sol 6.1 high), after the fixes:** 14 of the 16 confirmed fixed, 2 partly. New: 2 high, 4 medium, all fixed by the coordinator, no new tests: config's top-level `env` (startup environment values) still reached the page (now dropped too); the Copilot import answered with its token through the plain call (`redact_answer` now keeps only `success` for `config/import-copilot`); clicking Yes in a batch recorded No; an older answer's completion closed a newer question form; Esc while the sign-in method sheet was still reading could open sign-in anyway; a pasted secret with two trailing newlines lost both (now only validation strips one). Then the owner asked for a third one (below).
- **Third review t89 (Codex Sol 6.1 high).** 5 findings, all fixed by the coordinator, no new tests, no fourth review:
  1. A question notice for another batch could clear the shown question. The notice now has to name the shown `batch_id`, and each notice is counted (`questionNoticeCount` in `prompts.svelte.js`).
  2. A slow read of the waiting question could bring back a form that a later notice had already closed. The read is dropped when a notice arrived meanwhile (`ask/store.svelte.js`).
  3. The API key sheet could save a key that was edited after it was checked. An edit clears the check, and Save checks the key again (`signin/ApiKey.svelte`, `verifiedKey`).
  4. Opening a choice's note with the mouse over another row used the old row. It now takes the hovered row (`QuestionForm.svelte`, `openNote`).
  5. Selecting text in a choice with the mouse also picked the choice. A click while text is selected is ignored (`clickChoice`).

## Phases 5-6, wave 3 (EXT, SET, THM, VIEW, SHARE, SYS), 2026-10-19

The owner asked for this wave to be written like wave 2, and for this note.

- **Written by Claude Opus at high effort, not max.** The coordinator (Claude Opus 5.5) wrote all six pieces itself at high effort. The plan called for max effort on brand-new screens.
- **No tests at all.** None of the design's `u:`, `r:` or `w:` checks for EXT, SET, THM, VIEW, SHARE or SYS were written. There is no window driver, no deliberate break and no look picture for any wave 3 screen.
- **No ultrabuild, no builder report.** No piece briefs, no separate builders, no row-by-row list of what is covered.
- **Evidence:** only that the existing unit tests, `pnpm build` and `cargo test` still pass. None of them exercise wave 3's code.
- **Places it differs from the design** (not checked by anyone):
  - SYS update checks run in the app's first window (`app_info().first_window`), not "the focused one, else the first window opened".
  - The crash notice's Restart only reloads the window after a web view crash; after a panic the notice already shows in the restarted app, so Restart just closes it.
  - `crush_dirs` also answers with the log file and the crash folder (`log`, `crashes`) for About; design 6.4 lists only the folder lines.
  - The memory pop-up is also checked once when a window joins (`GET /v1/memory`), not only on `memory_status` events, so a window that opens while memory is already high still claims the episode.
  - The log viewer keeps at most 5,000 lines while following (the terminal keeps everything).
  - `api.cliUpdates` and `api.crushUpdate` now take the `manual` flag.
  - Window title needs the new capability `core:window:allow-set-title`.
- **Not done in wave 3:** P4.4 (tree and restore dialogs) still waits.
- **Review t90 (Codex Sol 6.1 high), after wave 3 was written:** 3 high and 13 medium. All 16 fixed by the coordinator, no new tests, no second review:
  1. (high) An MCP server's URL waiting for sign-in reached the page whole, so a password or key in it could too. `redact_answer` now keeps only scheme, host and path for `mcp/pending-auth`. A key placed in the path itself still crosses.
  2. (high) Cancel in the phone-share setup never stopped a share that came up late. The cancelled start is now remembered and stopped unless a newer start wants it (`remote/share.js`).
  3. (high) A late phone "new chat" message could land in another chat. Every phone action now needs the chat the phone saw to still be the window's chat, as the terminal does (`remote/actions.js`).
  4. Saving the sub-agent instructions before they were read wrote an empty file; Enter in Memory Limit before the limit was read removed it. Both now wait for the read (and the instructions are never saved over a file that failed to read).
  5. Local MCP toggles ignored the global off switch. The local state is now the terminal's `localDisabled` (`McpToggles.svelte`).
  6. The image picker could hang on a named pipe. Only regular files are opened to test readability (`dirlist.rs`).
  7. A closed window's badge or progress could stay on the taskbar. Window destruction now removes it and refreshes the taskbar (`alerts::window_gone`).
  8. Log followers could outlive their window. Each follow belongs to its window; a new follow, a closed window or a gone window stops it, and it leaves the table when it ends (`logs.rs`).
  9. Lines written between the first read and the follow were lost. `logs_tail` returns `end` and the follow starts there.
  10. Moving in the theme list while Apply saved could leave a preview showing for good. The list ignores moves and previews while Apply runs.
  11. After editing or creating a theme the list and its cached colors were stale. The editor and New Theme now call back and the list is read again.
  12. Settings fields that were only focused could save an old value over a change made elsewhere. Fields follow the config while not being typed in.
  13. Enter on the launcher password's Cancel button saved the password. A focused button now presses itself.
  14. Esc in the Skills sheet did not stop an install or search. Those calls now carry a call ID and are aborted when the sheet closes.
  15. Permission and question alerts never fired for a chat no window shows (the engine sends those prompts only to windows showing the chat). Alerts now also follow `prompt_pending`, which every window gets. Their texts differ from the design: "Permission required in \"<chat>\"" and "Questions in \"<chat>\" need your input" (the mark has no tool name or count).
  16. Two windows could claim one crash report, and a window could take another window's web view crash. `crash_pending` now holds one lock for the whole claim and takes the caller's own web view report.
- **Fourth review (t94, Step 5 Preview through OpenCode Go, read-only):** all 16 findings of t90 confirmed fixed, 17 new ones. The first try (t93) ran out on DeepInfra's hidden reasoning; a parallel MiMo V2.6 Pro review (t95) was asked for as well. Not real or not changed:
  1. `crush remote password --stdin` and `crush self-update run --tag --json` are missing: true only of the `crush` installed today (a main build). This branch's `crush` has both; the install step replaces it. Until then the desktop app run against an older `crush` cannot set the launcher password or run updates.
  7. The picture picker reopens where it was left, not the project folder: design 6.6 chose that on purpose, though design 2.10 and the terminal start in the project folder. Left as design 6.6 says.
  8. `has_checkout` on a skipped check: the engine always sets it, and only the AI tools check is skipped within the hour.
  13. `alerts.rs` sends its players' output nowhere, so only `remote.rs` needed the fix.
- Fixed, without tests (the wave's rule):
  3. Toggle skills now caps its list at 12 rows (FilterList's new `maxRows`) and counts the filtered rows below the view for "↓ N more" (FilterList's new bindable `below`).
  4. In Global scope a server this repository turned off shows "enabled", as the terminal.
  5. The memory warning beeps with style Bell instead of a pop-up.
  6. Settings' last line names the config folders from `crush dirs`.
  9. The automatic update checks run in whichever window connects first (new `updates_claim` command), not only the window that opened first.
  10. MCP error texts lose any password in the URLs they name (`scrub_errors` on `GET mcp/states` and on every event), with a Rust test.
  11. The taskbar asks for attention on the blocked window only.
  12. The update notice joins names with " and " and shows nothing when nothing was updated, as the terminal.
  13. `remote.rs` reads crush's output while it runs, so a full pipe cannot stall it.
  14. Keys wait while a theme is being applied; the sheet closes when the save is done.
  15. Log lines that arrive while following is off are kept; only the scrolling stops.
  16. Check-in minutes accepts 0 (off).
  17. "Memory Limit" in the command palette is Linux only, as the Settings row.

## Phases 5-6, wave 1 (KIT, TERM, LST, LIVE), 2026-10-19

The owner stopped the four builders (t82 to t85) once their app code was written and they were only working on tests. Their work was committed as work in progress and merged into `desktop` without the usual checks: KIT `e5a7ef50`, TERM `831a0b98`, LST `658b508d`, LIVE `bc8d07c5`.

Never done for any of the four:

- **Builder reports.** No list of rows as pass / runs-after-merge / not done, no interpretations or places the design didn't match the code, no "Look picks", no refused commands. The coordinator does not know which rows each piece actually covers.
- **Done-checks.** None of these were run by the builders: `pnpm test`, `pnpm build` and `cargo test` on the finished piece; the window driver passing twice in a row (fixture mode first); the outside check finding nothing changed; `git diff --name-only 925a538e..` listing only the piece's own files.
- **Window drivers.** Not finished and never run. Required window checks per the design: KIT 20 (`w:kit.*`), TERM 17 (`w:term.*`), LST 31 (`w:lst.*`), LIVE 28 (`w:live.*`).
  - KIT: `tests/driver/p5_kit.py` was never started (only its fixture `tests/fixtures/kit-sheets.json` exists).
  - TERM: `tests/driver/p6_terminal.py` written, not run (the builder was rebuilding to run it).
  - LST: `tests/driver/p6_leftlist.py` written, its engine steps were being fixed, not run.
  - LIVE: `tests/driver/p6_live.py` fixture part only; engine part not written.
- **Deliberate breaks for the drivers.** None (none of the drivers ran).
- **Look checks.** No `look_real` pictures of any new screen; no alignment measurements (see `align_real.py` below).

Unit and Rust tests the design names, and their state:

| Check | File | State |
|---|---|---|
| `u:kit.fuzzy_parity` | `src/lib/kit/fuzzy.test.js` | written; break shown (builder status) |
| `u:kit.help_truncate` | `src/lib/kit/helpfit.test.js` | written; break not confirmed |
| `u:kit.grace_repeat` | `src/lib/kit/sheetkeys.test.js` | written; break not confirmed |
| `r:term.env_clean` | `src-tauri/src/pty.rs` | written; break not confirmed |
| `r:term.editor_args` | `src-tauri/src/editor.rs` | written; break not confirmed |
| `u:lst.picker_match` | `src/lib/leftlist/picker.test.js` | written; break shown (2 of 6 failed, then passed) |
| `u:lst.spinner_clock` | none | **missing** |
| LIVE | none named by the design | — |

Other open items from wave 1:

- **Strip width fix unproven.** `ccc760e7` (left column 64 wide and divider fixed while the strip shows) passed unit tests and the build only; LST's `w:lst.collapse_strip` was meant to prove it in the window and never ran.
- **S5 focus bug fixed only on TERM's side.** S5's `closeMain()` focuses the typing box while it is still hidden; TERM refocuses after the redraw in `src/lib/terminal/tabs.svelte.js`. Any other piece that closes the main view (LIVE's helper view) still hits the S5 bug. Proper fix belongs in `src/lib/mainview.js` (S5).
- **SHELL / PATH note unchecked.** If a driver sets `SHELL`, `login_env.rs` may rebuild PATH so plain `crush` no longer finds the test build (S5 re-review note e). TERM and LIVE were asked to check it; neither reported.
- **Merged before review.** Review t86 (Sol 6.1 high) started after the merge, not before. Full checks on the merged tree: job 00D.
- **Review t86 findings, fixed by the coordinator without tests or re-review** (owner: "you make the fixes"). None of these fixes has a test, a deliberate break, a window check or a second review:
  1. (high) The external editor's text could land in another chat's draft after a chat switch. Now the chat it came from gets it (`editor.js`, new `setDraftOf` in `composer.svelte.js`).
  2. (high) A terminal still starting when its window closed could outlive the window. `pty_close_all` now marks the window closed and a later start is killed and refused (`pty.rs`). Window labels are never reused, so the mark stays.
  3. (medium) In a button row, ← / → moved the look of the selection but not the focus, so Enter could press the other button. Focus now follows (`kit/Buttons.svelte`).
  4. (medium) The left list's 1 s state checks restarted on every chat read and could be put off forever. The timer now runs while connected (`plugins/leftlist.js`; `anyWorking` removed).
  5. (medium) After a terminal row was dragged, closing a tab showed the wrong neighbour. The list and TERM now share one order: `terminals` itself, moved by TERM's new `moveTab` (`leftlist/numbers.js`, `tabs.svelte.js`; `leftList.terminalOrder` removed).
  6. (medium) OSC 7 folder reports naming this computer were ignored (no host name). `app_info` now gives `host` (gethostname) and TERM uses it.
  7. (medium) A terminal that failed to start never showed "Couldn't start the terminal." `openTerminal` now also returns `started`, and the folder picker waits for it.
  8. (medium) Ctrl+Shift+W did nothing while a terminal had the focus. Now bound in the `main` scope too.
  9. (medium) The background/helper sheet hid its scrollbar, so KIT's bar never showed. The hiding CSS is removed (`live/Background.svelte`).
  10. (medium) Picking another helper while one was shown kept the first helper's chat. `showMain` takes a `key` and the host mounts the view anew when it changes; the helper view passes `helper:<id>`.
  11. (medium) The left list's `app_info` call used up the one-time launch ID that SHARE needs. `commands.js` keeps the first launch ID seen; SHARE must use `takeLaunchId()`. `probe.js` now goes through the same wrapper.
  - Interfaces changed for later pieces: `openTerminal` returns `{id, exited, started}`; `showMain` takes `key`; `app_info` has `host`; `takeLaunchId()` is new. Design 6.4 / 7.4 were not updated.
- **Full checks found a hang.** `s5_commands` failed on the merged tree (job 00D): LST's `folder_pick` opened a real folder dialog in test runs when `CRUSH_DESKTOP_TEST_PICK_FOLDER` was unset, and every probe command waited on it. Test runs now never open the dialog (unset means cancel). Rerun pending.
- **Coordinator statement corrected.** PLAN.md (stop entry) says KIT's fuzzy and help-fit tests had their breaks shown; only the fuzzy break was seen in the builder's status.

## S5 (phase 5-6 scaffold)

- `tests/look/align_real.py` broken since phase 3 ("mark: nothing matches .day"), so S5 and every later piece have no alignment scores; only pixel comparisons (`look_real`). Fix is set for phase 7's look review.
- Re-review t81 (Sonnet) instead of the planned reviewer (Argon was too slow); its lows a to d fixed, e (SHELL/PATH) left as a note (above).

## Phase 4

- **F5:** a closed chat with no branch link cannot be found through the list search (`listmodel.js:166-172` skips closed chats). Left for SES (wave 2).
- **P4.4** (tree and restore dialogs) not built; waits for the command palette (PAL, wave 2).
- **P4.3 live fork check:** OpenCode not checked live (its key had expired); AGY has no fork.

## Phase 3

- Coverage limits the reviewer noted and left open: the queue recall test does not check that the abort happens; START-018 does not force two settings saves to overlap.
- Round 3 fix (reconnect notice must show on screen) closed with the builder's break evidence and no further review (3-round cap reached).

## Engine pieces (phase 1-2) and G56

- **Piece B known limit, not fixed:** helpers, jobs or `crush ask` started by a desktop turn while a `crush run` is open in the same chat go with the `crush run`, because those requests name only the chat. Also unchanged: `crush run` uses the joined app's auto-approve; Ctrl+C in `crush run` cancels the whole chat.
- **G56:** the config lock test is one-sided (300 ms window); the reviewer judged it non-blocking.
- **Two-app recovery test:** checking the `helper` event was left for "once B is merged"; not confirmed done.

## Look and design tool

- The first-screen review (`.impeccable/review/hero.json`, session `959ce8fa`) still waits for the owner. The design tool's later steps (other screens, motion, window sizes, finish review, DESIGN.md) wait on it; phases 3-6 were built in the same style meanwhile.

## Later by plan (not skipped, listed so nothing is lost)

- Phase 7: checklist sweep, look review (with a fixed `align_real.py`), DESIGN.md, Mac build path (never built or run on a Mac), docs, OpenCode live fork check.
- Final Fable review of the whole app; then merge to main, push, install and the app-menu entry.
