# Skipped and partly done steps

What the build process (PLAN.md, the ultrabuild review and test rules) called for but was skipped, cut short or left open. Newest first. Each item says what is missing and where it would be done. Details and decisions stay in PLAN.md; this file is the to-do list of gaps.

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
- **Merged before review.** Review t86 (Sol 6.1 high) started after the merge, not before. Full checks on the merged tree: job 00D. Early findings from t86: a terminal that fails to start never reaches the left list's failure line; the external editor can put its text into another chat's draft after a chat switch.
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
