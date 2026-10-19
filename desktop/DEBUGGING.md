# Skipped and partly done steps

What the build process (PLAN.md, the ultrabuild review and test rules) called for but was skipped, cut short or left open. Newest first. Each item says what is missing and where it would be done. Details and decisions stay in PLAN.md; this file is the to-do list of gaps.

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

- Phase 7: shared-engine switch-over, checklist sweep, look review (with a fixed `align_real.py`), DESIGN.md, Mac build path (never built or run on a Mac), docs, OpenCode live fork check.
- Final Fable review of the whole app; then merge to main, push, install and the app-menu entry.
