"""LIVE's window checks (desktop-design-5-6 2.15 to 2.17, 7.3, 8.4, 8.5): the
tray entries, the Background Processes and Sub-agents sheets, Ctrl+B and its
hint, the helper view and the memory notice, plus the phase 3 window steps
CHAT-009, CHAT-061 and CHAT-062 (8.7).

Two runs (the driver starts the app itself, so both use --no-app):
  tests/check.sh --no-app --fixture tests/fixtures/live-tray.json p6_live.py
      the fixture steps; live-memory.json and live-view.json are read from the
      same folder and passed to their own app runs (a later --fixture wins)
  tests/check.sh --no-app p6_live.py
      the engine steps (tests/out/bin/crush built with -tags crushtest)
Names after the driver run only those checks. A leg that needs a piece not
merged yet prints "SKIP <leg>: runs after <PIECE> merges"; --strict turns
those into failures (for the run on desktop after the merges).

The plan: each step, the rows it proves, and what makes it fail.

Fixture steps
  memory_warning   LIVE-018 (notice leg)  live-memory.json: GET /v1/memory is
                   high; then memory_status turns it low, then high again.
                   Fails when the notice line does not show the message as a
                   warning while high, still shows it after high turns false,
                   or does not show it again when high returns. (The one
                   pop-up per episode is SYS's listener: its legs SKIP until
                   SYS merges.)
  tray_counts      LIVE-001, LIVE-008 (count)  Fails when the tray does not
                   show "2 subagents" then "2 background processes" after the
                   to-dos and queue (a done helper, a helper of another chat
                   and a server job (G-18) counted), the helpers mark does not
                   turn with the wall-clock second, or the hint is not
                   "↓ manage".
  tray_keys        LIVE-002, CMD-040/041's sheets by key  Fails when ↓ from
                   the end of the field does not focus the helpers entry, the
                   hint does not change to "enter list · ←/→ select · ↑ back",
                   ← and → do not move and wrap, ↑ or Esc do not give the field
                   back, Tab does not leave, a typed letter is not typed into
                   the field, or Enter, Space, x, Delete, Backspace and Ctrl+X
                   do not open the selected entry's sheet (Sub-agents for
                   helpers, Background Processes for jobs); a click on an
                   entry must open its sheet too; Ctrl+X with the tray not
                   focused opens Background Processes.
  sheets           DLG-223, DLG-224, DLG-225, DLG-226, LIVE-010, LIVE-015 and
                   LIVE-016 (fixture rows)  Fails when a title, row text, info
                   text, row order, elapsed format ("0m41s", "1h05m") or help
                   pair differs from 2.15, a non-running helper or a server job
                   is listed, or the elapsed time is not redrawn each second.
  stop_kill        DLG-229  Fails when a click does not select, the Stop/Kill
                   button does not show on the selected row, the button (and,
                   with KIT merged, x/Delete/Backspace) does not post
                   helpers/{hid}/stop or jobs/kill with {"job_id"} or {"pid"},
                   the row does not leave at once with the selection kept in
                   place, or the info is not "Stopped sub-agent <name>" /
                   "Killed <command>".
  dialogs_empty    DLG-230  Fails when Background Processes on a chat with no
                   jobs does not say "No background processes are running.",
                   or Sub-agents does not say "No sub-agents are running." and
                   stay open when its last row goes.
  helper_view      DLG-228, LIVE-005  live-view.json. Fails when a double
                   click (and Enter, with KIT) on a helper row does not close
                   the sheet and show the helper's chat read-only in the middle
                   column ("Opening conversation…" first), the head is not the
                   helper's name, the band is not "<name> · <status> · read
                   only" (the model only while the right panel shows), the
                   footer, tray line (no key hint) and "← Return to parent" with
                   "esc" are missing, or Esc, the button, the shown chat
                   changing or a Q8 form opening do not close it.
  helper_statuses  LIVE-008 (band)  live-view.json's helper events: fails when
                   the band's status word and mark do not follow running,
                   done, failed and stopped.
  helper_view_keys LIVE-006  Fails when Tab/Shift+Tab/←/→ do not move focus
                   onto and off the button, Enter on it does not close, ↑/k
                   and ↓/j do not scroll a line (↓ at the bottom focusing the
                   button), PgUp/Ctrl+U and PgDn/Ctrl+D half a page, Home/End
                   the ends, Ctrl+C or y with a selection do not copy it with
                   "Selected text copied to clipboard", or Ctrl+C without one
                   does anything (no quit sheet, no notice).
  helper_view_mouse LIVE-007  Fails when the wheel does not scroll the view
                   (or scrolls the hidden parent), a drag does not select, the
                   scrollbar does not drag, a link does not open through the
                   opener, or a click on the button does not close the view.
  helper_view_paste LIVE-021  Fails when Ctrl+V, Shift+Insert or a middle
                   click puts the clipboard anywhere (the parent's draft
                   included), or releasing a drag selection copies it.

Engine steps (scripts tests/fakes/scripts/live-*.json, seams/live.json)
  spawn_and_jobs   LIVE-008, LIVE-009 (CHAT-009), LIVE-011, LIVE-015, LIVE-016,
                   LIVE-017, CHAT-062, DLG-227  A script runs crush spawn with
                   every flag, crush bg, the bash tool's run_in_background and
                   a listening crush bg --service. Fails when the tray and the
                   sheets do not show them as 2.15 says (flags on the helper
                   row, "job <id>" rows, the server job left out), an open
                   sheet does not follow them (selection kept by identity), a
                   finished helper stays counted, its result does not reach
                   the parent chat, a permission form opens for the API
                   helper, or the shells do not carry CRUSH_TASKS_DIR and
                   CRUSH_SESSION_ID as LIVE-017 says.
  ctrl_b           LIVE-003, CHAT-061  Fails when Ctrl+B during a running
                   command does not post background, the command does not move
                   to the background in both apps within 2 s, or Ctrl+B with
                   nothing running does not say "No running command can move
                   to the background".
  ctrl_b_hint      LIVE-004  SKIPs until MOD merges (capabilities). Fails when
                   the hint does not show under a command that ran 60 s while
                   can_background is true, or stays after Ctrl+B moved it.
  stop_parity      DLG-231, DLG-227 (x)  Fails when a helper stopped or a job
                   killed in one app still shows as running in the other after
                   2 s.
  resume           LIVE-012  Fails when a helper and a job interrupted by an
                   engine restart are not back in both apps' trays.
  check_in         LIVE-013  Fails when no check-in message reaches the chat
                   after options.check_in_minutes.
  stray            LIVE-014  Fails when a stray background process is not
                   reported in the chat when it ends.
  memory_engine    LIVE-018 (engine), LIVE-019, LIVE-020  With seam memory:
                   fails when the notice lacks the seam's message, the next
                   agent turn's request to the fake AI server lacks
                   <crush_memory_warning>, or any error notice appears while
                   the memory box cannot be joined (the sandbox has no systemd
                   user manager).
"""
