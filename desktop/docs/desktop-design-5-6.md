# Crush Desktop: desktop design for phases 5 and 6

Status: final (pass 2 with the review fixes), written by t32 on 2026-10-18; pass 1 was committed as `4a35cd5d`, pass 2 as `a482b00e`. It covers the desktop app's side of phase 5 (dialogs and settings: CMD, DLG, SET, THEME) and phase 6 (live work: LIVE, NOTE, REM, UPD, LIST, MISC).

Pass 1 was written without the phases 3-4 design, on purpose. Pass 2 maps every shared name onto that design (`desktop/docs/desktop-design.md` at the `desktop` branch's `cc785a12`, which carries the S3 review decisions; cited "3-4 design §n"), adds the scaffold S5 for everything phases 5 and 6 need in files that S3 or a phase 3-4 piece owns (7.1), settles file ownership (7.3, 7.5), and settles the rows both designs touch (section 1). Section 10 lists what pass 2 changed, and section 11 what the review fixes changed.

Several builders work from this file at the same time, each in its own Git worktree. A builder never invents a shared name. If something here is missing or wrong, the builder asks the coordinator.

## 0. How to read this

- **Code citations.** `path:line` is relative to the worktree root `/home/braxtenchenay/dev/crush-desktop`, at the `desktop` branch's commit `cc785a12`: engine pieces A to D with B's follow-ups, and scaffold S3 (merged as `736c5698`); the code is the same at `736c5698`, `efc88faf` and `773344e0`, since the three commits after the merge change only `desktop/PLAN.md` and the phases 3-4 design. Pass 2 read the code at `40405abf` (scaffold S and pieces A to D merged, `desktop/PLAN.md:346`) and `65e4250d`. The review fixes re-cited every `desktop/` citation at `773344e0`, where S3 had moved lines, and every Go citation into a file that B's follow-ups changed (`internal/agent`, `internal/backend`); `internal/rpc/` and `internal/proto/` have not changed since `40405abf`. Plan citations use `desktop/PLAN.md` at `cc785a12`. Terminal facts were read in the code, either by me or by the six read-only research helpers that worked for this pass; their own "inferred" marks are kept. Anything not read in code is marked **inferred**.
- **The engine design** is `desktop/docs/engine-design.md`. "Engine design 5.3" means its section 5.3; a bare "5.3", "5.3 Rules" or "5.9 c" inside a paragraph that reads engine data means the same. This file's own look proposals are written "look 5.N" (section 5); only inside section 5 itself does a bare "5.N" mean look 5.N. Its routes, events and JSON field names are used here exactly as written there. Routes that exist today are in `internal/rpc/routes.go`.
- **Shared names.** Every Tauri command, window event, store, registry, module path and wire type named here is pinned: either the phases 3-4 design's, used as it defines them, or one that S5 or a piece of section 7 adds. Section 6 lists them all.
- **MUST** and **MUST NOT** bind the builders.
- **Exact texts.** Text in double quotes is the exact user-facing text, copied from the terminal at the cited line. The desktop shows it unchanged, including the terminal's three-dot "..." where the terminal uses it. Where the desktop changes a text, the change is listed in 9.4.
- **Sizes** are picture pixels, written `calc(N * var(--px))` (app.css). The window zooms in twelfths; Compact (8/12) is the default. Small functional text never drops below 11 real px: the type roles `--t-meta`, `--t-small`, `--t-label`, `--t-path`, `--t-stamp` and `--t-agent-mono` already use `max(11px, …)` (`desktop/src/app.css:60-70`). A new role for small text MUST use the same floor. Hairlines stay one real pixel (`1px`).
- **Check IDs.** Every row of mine gets a check that can fail.
  - `w:<piece>.<name>`: real window, desktop app only. The driver is the piece's `desktop/tests/driver/p5_<folder>.py` or `p6_<folder>.py` (7.3), function `check_<name>`, which calls `harness.step("<row>")`. It drives a real mouse and keyboard with xdotool, reads the probe file and saves one screenshot per checklist row in `desktop/tests/out/<driver>/` (section 8).
  - `x:<piece>.<name>`: as `w:`, plus the terminal app running in a hidden tmux on the same throwaway data (PLAN section 8, "Both apps").
  - `u:<piece>.<name>`: Vitest unit test in the piece's `desktop/src/lib/<folder>/*.test.js` (7.3), test named `<name>`.
  - `r:<piece>.<name>`: `cargo test` in one of the piece's Rust files of 7.3 (for example `desktop/src-tauri/src/pty.rs` for TERM), test named `<name>`.
  - A row may list a `u:` or `r:` check beside its real-window check. The real-window check is the one that proves the row.
- **Pieces** are named in section 7: `S5` (scaffold), `KIT`, `PAL`, `MOD`, `AUTH`, `SES`, `ASK`, `EXT`, `SET`, `THM`, `VIEW`, `LIVE`, `LST`, `TERM`, `SHARE`, `SYS`, and `G56` (the Go additions of section 9.1, not designed here). Check IDs use the piece name in lower case.
- **Shared terms.**
  - *Sheet*: the shared dialog frame of 2.1 and look 5.1.
  - *Q8 slot*: the typing box's place, which asks and question forms take until answered (PLAN Q8): S3's `ui.openSlot({kind, component, props})` and `ui.closeSlot(kind)`, drawn by P3.7 (3-4 design §1.10, §4.9).
  - *Q7 tray*: the tray above the typing box for to-dos, the queue, helpers and jobs, shown only when something is in it (PLAN Q7): S3's `lib/tray.js`, drawn by P3.7 (3-4 design §1.10).
  - *Notice line*: where the terminal's short notices ("info", "warn", "error" messages with a time to live) appear: S3's `ui.notify(kind, text, ttlMs)`, drawn by P3.8's `NoticeLine.svelte` (SIDE-028; 3-4 design §4.14). Earlier drafts called it the status line.
  - *Window*: one desktop window. It shows one project, has its own `client_id` (engine design "What the desktop app (Rust side) calls" 2), and counts as one "app" for Q2.

## 1. Row map

Every row of the ten sections appears exactly once, and so do the two rows the phases 3-4 design moved to phase 6 (last block). The **Piece** column names the build piece; the last column is the check, or the row's status when it is not mine. A row the phases 3-4 design owns points to its section and piece there.

Counts: 634 rows (632 in the ten sections, plus TYPE-015 and START-020). **Mine: 547.** Phases 3-4 design, phase 3: 46 (DLG-119 to DLG-140; DLG-294 to DLG-311; LIST-012, LIST-033 to LIST-037). Phase 4: 25 (DLG-089 to DLG-112, and LIST-016). Left out (PLAN section 12): 10. Command line only (PLAN section 12): 6.

Four LIST rows describe closing both chats and terminal tabs (LIST-014, -015, -043, -050). Their terminal-tab parts are TERM's. Their chat parts differ by design: closing a desktop chat only hides it, at once and with no box, and stops nothing (PLAN.md:103; phases 3-4 design D39 and C19, which leaves these rows' text to this design).

### LIST (56)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| LIST-001 | Left list in each window: the project's chats (not closed), then terminal tabs, then server rows (2.22) | LST | `w:lst.list_shows_project_chats` |
| LIST-002 | Left list collapses to the narrow strip; forced to the strip below the minimum width | LST | `w:lst.collapse_strip` |
| LIST-003 | Collapsed state remembered in the desktop settings file | LST | `w:lst.collapse_remembered` |
| LIST-004 | Alt+N: folder picker "New session in…" | LST | `w:lst.new_session_picker` |
| LIST-005 | Alt+T and the Terminals "+": folder picker "New terminal in…", then a terminal tab | TERM | `w:term.new_terminal` |
| LIST-006 | Folder picker sheet: title, path box, 5 matches, Recent, error line, hint | LST | `w:lst.picker_layout` |
| LIST-007 | Folder picker text box keys | LST | `w:lst.picker_keys` |
| LIST-008 | Alt+1…Alt+9 and click switch rows; switching marks a chat read | LST | `w:lst.switch_marks_read` |
| LIST-009 | Alt+Shift+↑/↓ and drag reorder within a group | LST | `w:lst.reorder` |
| LIST-010 | Chat groups by day, then the Terminals group with its count | LST | `w:lst.groups` |
| LIST-011 | Chat row content; title falls back to "New session" (the row's × is phase 4's close) | LST | `w:lst.row_content` |
| LIST-012 | Row state marks: working, background, waiting, unread, ready, unknown; terminal mark | — | phases 3-4 design §2.6 (P3.9), which leaves the phase 6 marks to this design: unread ✓ "Done", ready, unknown and the terminal mark, drawn by LST through S5's row states (2.20) and checked by `w:lst.state_marks` |
| LIST-013 | Working mark: 8 frames, 100 ms, wall-clock aligned, only while a visible row works (S5's `ticker` and `Icon` change) | LST | `w:lst.spinner` |
| LIST-014 | Terminal tab close box "Close “Terminal”?"; chat part differs by design: closing a desktop chat only hides it, at once, with no box (phases 3-4 design D39, C19) | TERM | `w:term.close_confirm` |
| LIST-015 | Close box keys (terminal part; the chat part differs by design, as LIST-014) | TERM | `w:term.close_keys` |
| LIST-016 | Closing a chat stops it gently | — | phase 4: phases 3-4 design §3.3 (P4.1); differs by design: closing a desktop chat only hides it and stops nothing (its D39, C19) |
| LIST-017 | The app quits when its last window closes | LST | `w:lst.last_window_quits` |
| LIST-018 | Row title, folder, model, state and servers come from engine records and events | LST | `x:lst.status_from_engine` |
| LIST-019 | Terminal tabs use the active theme's colors and follow theme changes | TERM | `w:term.theme_follow` |
| LIST-020 | Terminal working folder from OSC 7, and `/proc/<pid>/cwd` on Linux | TERM | `w:term.cwd_tracking` |
| LIST-021 | Terminal scrollback: Shift+PgUp/PgDn, wheel 3 lines, alternate-screen arrows | TERM | `w:term.scrollback` |
| LIST-022 | Terminal selection, copy on release | TERM | `w:term.select_copy` |
| LIST-023 | Terminal paste, clipboard, focus reports, pictures | TERM | `w:term.paste_focus` |
| LIST-024 | Shift+Enter: a newline in chats, Enter in terminal tabs | TERM | `w:term.shift_enter` |
| LIST-025 | Terminal tab environment cleaned exactly as the session host does | TERM | `r:term.env_clean`, `w:term.env_shown` |
| LIST-026 | Window close handler follows Q2 | LST | `x:lst.window_close_q2` |
| LIST-027 | Background-color probe | — | left out |
| LIST-028 | Server rows at the bottom of the left list | LST | `w:lst.server_rows` |
| LIST-029 | Folded program rows "▸/▾ name · N ports" | LST | `w:lst.server_fold` |
| LIST-030 | Pick, open and stop a server row with keys and clicks | LST | `w:lst.server_keys` |
| LIST-031 | Open `http://localhost:<port>` in the browser | LST | `w:lst.server_open` |
| LIST-032 | Stop a server: a direct job kill through the engine | LST | `w:lst.server_stop` |
| LIST-033 | New chat: the New chat button and Ctrl+N | — | phases 3-4 design §2.6 (P3.9) |
| LIST-034 | "Opening <title>…" screen with elapsed seconds | — | phases 3-4 design §2.6 (P3.9) |
| LIST-035 | Opening a chat brings back its last model if still offered | — | phases 3-4 design §2.6 (P3.3) |
| LIST-036 | Events that arrive while a chat loads are replayed on top | — | phases 3-4 design §2.6 (P3.3) |
| LIST-037 | Chat deleted elsewhere: the window moves to a new chat | — | phases 3-4 design §2.6 (P3.3) |
| LIST-038 | Folder picker starts with the shown chat's or terminal's folder | LST | `w:lst.picker_prefill` |
| LIST-039 | Folder matching and the Recent list | LST | `u:lst.picker_match`, `w:lst.picker_matches` |
| LIST-040 | Folder picker errors "Type a folder." and "No folder at <path>" | LST | `w:lst.picker_errors` |
| LIST-041 | Folder picker mouse | LST | `w:lst.picker_mouse` |
| LIST-042 | Folder picker editing details | LST | `w:lst.picker_editing` |
| LIST-043 | Close box buttons clickable (terminal part; the chat part differs by design, as LIST-014) | TERM | `w:term.close_buttons` |
| LIST-044 | Narrow strip: open, "+", ">_", marks and numbers | LST | `w:lst.strip` |
| LIST-045 | The list scrolls normally and keeps the active row in view (the row's own GUI note) | LST | `w:lst.scroll_keeps_active` |
| LIST-046 | A terminal tab closes when its shell exits; engine start failure screen | TERM | `w:term.exit_closes_tab` |
| LIST-047 | Server row look | LST | `w:lst.server_look` |
| LIST-048 | "Couldn't start a session." / "Couldn't start the terminal." on the list's bottom row | LST | `w:lst.start_failures` |
| LIST-049 | Start flags apply to the first window; later windows get folder, YOLO and debug only | LST | `w:lst.flags_first_window` |
| LIST-050 | Closing by kind (terminal part; the chat part differs by design, as LIST-014); app quit stops terminal tabs | TERM | `w:term.close_kinds` |
| LIST-051 | Typing jumps a terminal to its newest output | TERM | `w:term.typing_jumps` |
| LIST-052 | Each chat knows whether it is shown and focused; hidden chats still notify | SYS | `w:sys.hidden_chat_notifies` |
| LIST-053 | Pictures under the folder picker and close box are covered and come back | LST | `w:lst.cover_images` |
| LIST-054 | The window draws in the desktop's theme from the first frame (3.3) | THM | `w:thm.first_frame` |
| LIST-055 | Pointer capture for drags | LST | `w:lst.pointer_capture` |
| LIST-056 | Terminal cursor shape and blink; text-box caret in the picker | TERM | `w:term.cursor` |

### LIVE (21)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| LIVE-001 | Q7 tray entries "N subagents" and "N background processes" | LIVE | `w:live.tray_counts` |
| LIVE-002 | Tray keys | LIVE | `w:live.tray_keys` |
| LIVE-003 | Ctrl+B moves the running command to the background | LIVE | `x:live.ctrl_b` |
| LIVE-004 | "ctrl+b to run in background" hint after 60 s | LIVE | `w:live.ctrl_b_hint` |
| LIVE-005 | Helper view: read-only transcript in the middle column | LIVE | `w:live.helper_view` |
| LIVE-006 | Helper view keys | LIVE | `w:live.helper_view_keys` |
| LIVE-007 | Helper view mouse; the wheel scrolls the view (PLAN section 12 fix) | LIVE | `w:live.helper_view_mouse` |
| LIVE-008 | Helper statuses running, done, failed, stopped | LIVE | `w:live.helper_statuses` |
| LIVE-009 | Helper result shown in the parent chat | LIVE | `w:live.task_result_message` |
| LIVE-010 | Read-only helper marked "read only" | LIVE | `w:live.read_only_marker` |
| LIVE-011 | API-provider helpers run without permission asks | LIVE | `w:live.api_helper_no_prompt` |
| LIVE-012 | Interrupted helpers and jobs come back after a restart | LIVE | `x:live.resume_after_restart` |
| LIVE-013 | Check-in messages every `check_in_minutes` | LIVE | `w:live.check_in_message` |
| LIVE-014 | Stray `nohup`/`&` processes reported when they end | LIVE | `w:live.stray_process_report` |
| LIVE-015 | `crush spawn` flags shown on the helper row | LIVE | `w:live.spawn_flags` |
| LIVE-016 | `crush bg` jobs shown on the job row | LIVE | `w:live.bg_job_row` |
| LIVE-017 | Task commands work in desktop chats | LIVE | `w:live.task_env` |
| LIVE-018 | Memory warning in the notice line plus one pop-up per 10 min | LIVE | `w:live.memory_warning` |
| LIVE-019 | Memory note added to agent turns while memory is high | LIVE | `w:live.memory_note` |
| LIVE-020 | Memory box: no error when it is unavailable | LIVE | `w:live.memory_box_quiet` |
| LIVE-021 | Helper view ignores pastes; copy only with Ctrl+C or y | LIVE | `w:live.helper_view_paste` |

### CMD (64)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| CMD-001 | Palette: Ctrl+P, and "/" on an empty typing box | PAL | `w:pal.open` |
| CMD-002 | Palette filter by name, aliases, description | PAL | `w:pal.filter` |
| CMD-003 | Palette selection wraps | PAL | `w:pal.wrap` |
| CMD-004 | Run the selected entry: Enter, Ctrl+Y | PAL | `w:pal.run` |
| CMD-005 | An entry's shortcut runs it while the palette is open | PAL | `w:pal.shortcut_inside` |
| CMD-006 | System / User / MCP categories | PAL | `w:pal.categories` |
| CMD-007 | Category markers beside the title | PAL | `w:pal.category_markers` |
| CMD-008 | Shortcut column, hidden past 25% of the row | PAL | `w:pal.shortcut_column` |
| CMD-009 | Skill description line under a user entry | PAL | `w:pal.skill_description` |
| CMD-010 | "Generating Prompt..." loading line | PAL | `w:pal.generating` |
| CMD-011 | Live refresh keeps filter and selection | PAL | `w:pal.live_refresh` |
| CMD-012 | Close the palette: Esc, Alt+Esc | PAL | `w:pal.close` |
| CMD-013 | Palette mouse: hover, click, wheel | PAL | `w:pal.mouse` |
| CMD-014 | "New Session" | LST | `w:lst.entry_new_session` |
| CMD-015 | "Sessions" | SES | `w:ses.entry_sessions` |
| CMD-016 | "Switch Model" | MOD | `w:mod.entry_switch_model` |
| CMD-017 | "Usage" | MOD | `w:mod.entry_usage` |
| CMD-018 | "Fallback compaction" | SET | `w:set.entry_autocompact` |
| CMD-019 | "Undo Last File Restore" (opens phase 4's undo preview) | PAL | `w:pal.entry_undo_restore` |
| CMD-020 | "Session Tree" (opens phase 4's tree) | PAL | `w:pal.entry_tree` |
| CMD-021 | "Fork from Prompt" (opens phase 4's tree in fork mode) | PAL | `w:pal.entry_fork` |
| CMD-022 | "Clone Current Branch" (calls phase 4's clone) | PAL | `w:pal.entry_clone` |
| CMD-023 | "Summarize Session" | PAL | `w:pal.summarize` |
| CMD-024 | "Enable/Disable FAST Mode" | MOD | `x:mod.fast_toggle` |
| CMD-025 | "Turn On/Off Ultracode" | MOD | `x:mod.ultracode_toggle` |
| CMD-026 | "Enable/Disable Thinking Mode" | MOD | `x:mod.thinking_toggle` |
| CMD-027 | "Select Reasoning Effort" | MOD | `w:mod.entry_effort` |
| CMD-028 | "Toggle Sidebar" (`compact_mode`) | SET | `x:set.toggle_sidebar` |
| CMD-029 | "Open File Picker" | VIEW | `w:view.entry_file_picker` |
| CMD-030 | "Open External Editor": `$VISUAL`/`$EDITOR` in a terminal tab | TERM | `w:term.external_editor` |
| CMD-031 | "Enable / Disable Docker MCP Catalog" | EXT | `w:ext.docker_mcp` |
| CMD-032 | "Toggle MCPs" | EXT | `w:ext.entry_toggle_mcps` |
| CMD-033 | "Toggle To-Dos/Queue" (calls phase 3's panel toggle) | PAL | `w:pal.toggle_pills` |
| CMD-034 | "Notification Style" | SET | `w:set.entry_notifications` |
| CMD-035 | "Toggle Yolo Mode" | PAL | `x:pal.toggle_yolo` |
| CMD-036 | "Toggle Help" (calls phase 3's help bar) | PAL | `w:pal.toggle_help` |
| CMD-037 | "Initialize Project" | PAL | `w:pal.init_project` |
| CMD-038 | "Save Project" | SES | `w:ses.save_project` |
| CMD-039 | "Open Project" | SES | `w:ses.entry_open_project` |
| CMD-040 | "Background Processes" | LIVE | `w:live.entry_background` |
| CMD-041 | "Sub-agents" | LIVE | `w:live.entry_subagents` |
| CMD-042 | "Remote Control" | SHARE | `w:share.entry_remote` |
| CMD-043 | "Update Agent CLIs" | SYS | `w:sys.entry_update_clis` |
| CMD-044 | "Update Crush" | SYS | `w:sys.entry_update_crush` |
| CMD-045 | "Skills" | EXT | `w:ext.entry_skills` |
| CMD-046 | "Toggle skills" | EXT | `w:ext.entry_kept_skills` |
| CMD-047 | "Use Terminal Background" | — | left out |
| CMD-048 | "Keep Focus on Composer" / "Let Tab Move Focus" | SET | `x:set.toggle_composer_focus` |
| CMD-049 | "Hide Thought Line" / "Show Thought Line" | SET | `x:set.toggle_thought_line` |
| CMD-050 | "Customize Composer" (opens phase 3's form) | PAL | `w:pal.entry_customize_composer` |
| CMD-051 | "Sub-agent Instructions" | SET | `w:set.entry_subagent_instructions` |
| CMD-052 | "Memory Limit" | SET | `w:set.entry_memory_limit` |
| CMD-053 | "Themes" | THM | `w:thm.entry_themes` |
| CMD-054 | "Disable Mouse" / "Enable Mouse" | — | left out |
| CMD-055 | "Run AI Work on CPU Half 2" / "Let AI Work Use Whole CPU" | SET | `w:set.toggle_agent_cpus` |
| CMD-056 | "Quit", always last; closes the window at once | PAL | `w:pal.entry_quit` |
| CMD-057 | Custom commands from Markdown files, with arguments | PAL | `w:pal.custom_command` |
| CMD-058 | A user skill entry attaches a skill chip | PAL | `w:pal.skill_chip` |
| CMD-059 | MCP prompts, with arguments | PAL | `w:pal.mcp_prompt` |
| CMD-060 | Global dialog keys Ctrl+P, Ctrl+L/Ctrl+M, Ctrl+S, Ctrl+G | PAL | `w:pal.global_keys` |
| CMD-061 | One owner per key, in the terminal's order | PAL | `w:pal.key_owner_order` |
| CMD-062 | Click focus; `composer_focus_only` blocks it | PAL | `w:pal.click_focus` |
| CMD-063 | One focus function keeps the focus look in step | PAL | `w:pal.focus_function` |
| CMD-064 | Ctrl+Z outside text boxes minimizes the window | PAL | `w:pal.suspend_minimizes` |

### DLG (311)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| DLG-001 | Sheets stack; only the front one takes input | KIT | `w:kit.stack` |
| DLG-002 | Reopening an open sheet brings it forward | KIT | `w:kit.reopen_front` |
| DLG-003 | Esc and Alt+Esc close; sheets may override | KIT | `w:kit.close_keys` |
| DLG-004 | Keys in flight are absorbed when an ask appears (425 ms quiet, 1.5 s cap) | KIT | `w:kit.key_grace` |
| DLG-005 | The same sheet reopened within 500 ms does not absorb again | KIT | `u:kit.grace_repeat`, `w:kit.key_grace_repeat` |
| DLG-006 | Mouse and results pass during the grace | KIT | `w:kit.grace_mouse` |
| DLG-007 | Sheets fit the window and follow resizes | KIT | `w:kit.fit_resize` |
| DLG-008 | Sheet parts: title, title-side controls, body, help footer | KIT | `w:kit.frame_parts` |
| DLG-009 | Help footer on one line; trailing hints drop with "…" | KIT | `w:kit.help_ellipsis` |
| DLG-010 | Filterable lists: fuzzy match, matched letters underlined, ellipsis | KIT | `w:kit.filter_underline` |
| DLG-011 | Right-side info keeps room for the name (half the row at most) | KIT | `w:kit.info_column` |
| DLG-012 | Scrollbar only when the list overflows | KIT | `w:kit.scrollbar_overflow` |
| DLG-013 | One-line fields: character movement | KIT | `w:kit.field_char_keys` |
| DLG-014 | One-line fields: word movement | KIT | `w:kit.field_word_keys` |
| DLG-015 | Delete character or word | KIT | `w:kit.field_delete_keys` |
| DLG-016 | Clear before/after the caret; line ends | KIT | `w:kit.field_line_keys` |
| DLG-017 | Paste into fields | KIT | `w:kit.field_paste` |
| DLG-018 | Multiline editors: line, page and text movement | KIT | `w:kit.area_move_keys` |
| DLG-019 | Multiline selection and extras | KIT | `w:kit.area_select_keys` |
| DLG-020 | Real buttons with an underlined shortcut letter; real rules | KIT | `w:kit.buttons_rules` |
| DLG-021 | Model picker opens: Ctrl+L, Ctrl+M, Switch Model, onboarding | MOD | `w:mod.open_keys` |
| DLG-022 | Large Task / Small Task | MOD | `w:mod.task_type` |
| DLG-023 | Model search | MOD | `w:mod.search` |
| DLG-024 | Provider groups and "Configured" badge | MOD | `w:mod.groups` |
| DLG-025 | "Recently used" group | MOD | `w:mod.recent` |
| DLG-026 | Custom and configured model names | MOD | `w:mod.custom_names` |
| DLG-027 | Disabled providers hidden | MOD | `w:mod.disabled_hidden` |
| DLG-028 | ChatGPT / Grok subscription models | MOD | `w:mod.subscription_catalog` |
| DLG-029 | Stale catalog fallback | MOD | `w:mod.stale_catalog` |
| DLG-030 | Move between models, wrapping | MOD | `w:mod.move_wrap` |
| DLG-031 | Choose a model, with every provider path and notice | MOD | `x:mod.choose` |
| DLG-032 | Ctrl+E edits a configured provider's sign-in | MOD | `w:mod.edit_login` |
| DLG-033 | Close; mouse added | MOD | `w:mod.close_mouse` |
| DLG-034 | Subscription catalog rejects the old choice | MOD | `w:mod.subscription_reject` |
| DLG-035 | Effort rows with "current" | MOD | `w:mod.effort_rows` |
| DLG-036 | Filter effort levels | MOD | `w:mod.effort_filter` |
| DLG-037 | Confirm an effort level | MOD | `x:mod.effort_confirm` |
| DLG-038 | Ultracode switch in the effort sheet | MOD | `w:mod.effort_ultracode` |
| DLG-039 | Cancel effort and Ultracode | MOD | `w:mod.effort_cancel` |
| DLG-040 | Effort opening errors | MOD | `w:mod.effort_errors` |
| DLG-041 | Sign-in method choice | AUTH | `w:auth.method_choice` |
| DLG-042 | Switch method with arrows, Tab, H/L | AUTH | `w:auth.method_keys` |
| DLG-043 | Confirm or go back | AUTH | `w:auth.method_confirm` |
| DLG-044 | Method mouse: click selects, second click within 400 ms confirms | AUTH | `w:auth.method_mouse` |
| DLG-045 | Methods side by side or stacked | AUTH | `w:auth.method_layout` |
| DLG-046 | API key field and destination notice | AUTH | `w:auth.key_field` |
| DLG-047 | Verify key, spinner at least 750 ms | AUTH | `w:auth.key_verify_min` |
| DLG-048 | Verification locks keys | AUTH | `w:auth.key_verify_locks` |
| DLG-049 | Invalid key | AUTH | `w:auth.key_invalid` |
| DLG-050 | Validated key: Enter or Esc saves (kept, PLAN section 12) | AUTH | `w:auth.key_valid_esc_saves` |
| DLG-051 | Key saved once; the original model is selected | AUTH | `x:auth.key_save_once` |
| DLG-052 | Cancel an unvalidated key | AUTH | `w:auth.key_cancel` |
| DLG-053 | Paste a key | AUTH | `w:auth.key_paste` |
| DLG-054 | "Initializing..." | AUTH | `w:auth.oauth_init` |
| DLG-055 | Device sign-in screen with the code box | AUTH | `w:auth.device_screen` |
| DLG-056 | Browser sign-in screen | AUTH | `w:auth.browser_screen` |
| DLG-057 | C copies the code | AUTH | `w:auth.copy_code` |
| DLG-058 | U copies the URL | AUTH | `w:auth.copy_url` |
| DLG-059 | Enter opens the browser | AUTH | `w:auth.open_browser` |
| DLG-060 | Grok manual code field | AUTH | `w:auth.grok_code_field` |
| DLG-061 | Submit or leave the manual code | AUTH | `w:auth.grok_code_keys` |
| DLG-062 | Credential saved; "Fetching models..." | AUTH | `x:auth.oauth_saved` |
| DLG-063 | Success resumes model selection | AUTH | `w:auth.oauth_success` |
| DLG-064 | Failure screen | AUTH | `w:auth.oauth_failure` |
| DLG-065 | Esc cancels the flow's background work | AUTH | `w:auth.oauth_cancel` |
| DLG-066 | Hyper device flow | AUTH | `w:auth.hyper_flow` |
| DLG-067 | Copilot device flow | AUTH | `w:auth.copilot_flow` |
| DLG-068 | ChatGPT browser flow | AUTH | `w:auth.chatgpt_flow` |
| DLG-069 | Grok device-flow fallback | AUTH | `w:auth.grok_fallback` |
| DLG-070 | Real links | AUTH | `w:auth.links_clickable` |
| DLG-071 | AWS SSO progress sheet | AUTH | `w:auth.aws_progress` |
| DLG-072 | Open the AWS URL | AUTH | `w:auth.aws_open` |
| DLG-073 | Close the AWS sheet; the refresh goes on | AUTH | `w:auth.aws_close` |
| DLG-074 | AWS success closes; errors shown | AUTH | `w:auth.aws_result` |
| DLG-075 | Provider re-sign-in sheet | AUTH | `w:auth.reauth_opens` |
| DLG-076 | Sessions sheet lists saved chats; current selected | SES | `w:ses.list` |
| DLG-077 | Columns: title, short folder, updated time | SES | `w:ses.columns` |
| DLG-078 | Info column hides past 35% | SES | `w:ses.info_hides` |
| DLG-079 | Filter "Enter session name" | SES | `w:ses.filter` |
| DLG-080 | Move and open | SES | `w:ses.move_open` |
| DLG-081 | Rename inline "Rename this session?" | SES | `w:ses.rename_inline` |
| DLG-082 | Confirm or cancel rename | SES | `x:ses.rename_confirm` |
| DLG-083 | "Delete this session?" | SES | `w:ses.delete_prompt` |
| DLG-084 | Confirm or cancel delete | SES | `x:ses.delete_confirm` |
| DLG-085 | Mouse: click selects, double-click opens | SES | `w:ses.mouse` |
| DLG-086 | Wheel scrolls without snapping back | SES | `w:ses.wheel` |
| DLG-087 | Close | SES | `w:ses.close` |
| DLG-088 | A chat from another folder | SES | `w:ses.other_folder` |
| DLG-089 to DLG-103 | Session tree, fork, clone | — | phase 4 (15 rows) |
| DLG-104 to DLG-112 | File restore preview | — | phase 4 (9 rows) |
| DLG-113 | Open Project sheet; empty notice | SES | `w:ses.projects_open` |
| DLG-114 | Project rows, "current" badge, filter | SES | `w:ses.projects_rows` |
| DLG-115 | Move and open; "Already in this project" | SES | `w:ses.projects_open_keys` |
| DLG-116 | Remove a saved project | SES | `w:ses.projects_remove` |
| DLG-117 | Switch project in the same window, blocked while busy | SES | `x:ses.switch_project` |
| DLG-118 | Close | SES | `w:ses.projects_close` |
| DLG-119 to DLG-140 | Permissions | — | phase 3 (22 rows) |
| DLG-141 | Question form takes the Q8 slot; sound and notice; closes when answered elsewhere | ASK | `x:ask.form_replaces_composer` |
| DLG-142 | Question types | ASK | `w:ask.types` |
| DLG-143 | Question limits | ASK | `w:ask.limits` |
| DLG-144 | Heading and description | ASK | `w:ask.heading` |
| DLG-145 | Tab labels and Confirm | ASK | `w:ask.tab_labels` |
| DLG-146 | `[`/`]`, Ctrl+←/→ switch questions | ASK | `w:ask.tab_switch` |
| DLG-147 | Tab moves focus between form and chat | ASK | `w:ask.tab_focus_chat` |
| DLG-148 | Click and hover tabs | ASK | `w:ask.tab_mouse` |
| DLG-149 | Narrow tab bar | ASK | `w:ask.narrow_tabs` |
| DLG-150 | Stable form height | ASK | `w:ask.stable_height` |
| DLG-151 | Collapsed summary | ASK | `w:ask.collapsed_summary` |
| DLG-152 | Enter answers and advances | ASK | `w:ask.enter_advances` |
| DLG-153 | Esc cancels the batch; Esc in a note closes only the note (PLAN section 12 fix) | ASK | `w:ask.esc_batch_cancel` |
| DLG-154 | Live answers survive tab changes | ASK | `w:ask.live_answers` |
| DLG-155 | Yes/no defaults to No; note | ASK | `w:ask.yesno_default` |
| DLG-156 | Yes/no keys | ASK | `w:ask.yesno_keys` |
| DLG-157 | Yes/no mouse | ASK | `w:ask.yesno_mouse` |
| DLG-158 | Single-choice rows and "Something else?" | ASK | `w:ask.single_rows` |
| DLG-159 | Choice navigation | ASK | `w:ask.choice_nav` |
| DLG-160 | Number keys 1–9 move | ASK | `w:ask.number_keys` |
| DLG-161 | Single-choice confirm | ASK | `w:ask.single_confirm` |
| DLG-162 | Single-choice click selects without advancing | ASK | `w:ask.single_click` |
| DLG-163 | Multi-choice preselection and checks | ASK | `w:ask.multi_preselect` |
| DLG-164 | Toggle a multi-choice answer | ASK | `w:ask.multi_toggle` |
| DLG-165 | Finish multi-choice | ASK | `w:ask.multi_finish` |
| DLG-166 | Custom answer field | ASK | `w:ask.custom_answer` |
| DLG-167 | Leave the custom field | ASK | `w:ask.custom_leave` |
| DLG-168 | Notes (Alt+N) | ASK | `w:ask.notes` |
| DLG-169 | Save or remove a note | ASK | `w:ask.note_save_remove` |
| DLG-170 | Paste into notes and custom answers | ASK | `w:ask.paste_notes` |
| DLG-171 | Hover follows the mouse | ASK | `w:ask.hover_follow` |
| DLG-172 | Scroll long forms | ASK | `w:ask.scroll_long` |
| DLG-173 | Free-text answer | ASK | `w:ask.freetext` |
| DLG-174 | Free-text submit and newline | ASK | `w:ask.freetext_keys` |
| DLG-175 | Free-text scroll and paste | ASK | `w:ask.freetext_scroll` |
| DLG-176 | Confirm page "Ready to go?" | ASK | `w:ask.confirm_page` |
| DLG-177 | Answer summary | ASK | `w:ask.confirm_summary` |
| DLG-178 | Unanswered WARN tag | ASK | `w:ask.confirm_warn` |
| DLG-179 | "Yup!" / "Not yet" | ASK | `w:ask.confirm_choices` |
| DLG-180 | Back to the first unanswered question | ASK | `w:ask.confirm_back` |
| DLG-181 | Confirm mouse and scrolling | ASK | `w:ask.confirm_mouse` |
| DLG-182 | Select and copy form text | ASK | `w:ask.select_copy` |
| DLG-183 | Links in questions | ASK | `w:ask.links` |
| DLG-184 | Choice pictures follow cursor or hover | ASK | `w:ask.pictures` |
| DLG-185 | Picture states | ASK | `w:ask.picture_states` |
| DLG-186 | Clicking a picture opens the file | ASK | `w:ask.picture_click` |
| DLG-187 | Pictures are image elements, clipped by scrolling | ASK | `w:ask.picture_element` |
| DLG-188 | Form mouse: click, hover, drag-select, wheel, paste | ASK | `w:ask.form_mouse` |
| DLG-189 | `crush secure-entry` asks reach the window | ASK | `w:ask.secure_cli` |
| DLG-190 | Secure entry stays local; "Unavailable" text | ASK | `w:ask.secure_local_only` |
| DLG-191 | Masked value | ASK | `w:ask.secure_masked` |
| DLG-192 | Secure editing keys | ASK | `w:ask.secure_keys` |
| DLG-193 | Paste a secret | ASK | `w:ask.secure_paste` |
| DLG-194 | Ctrl+U clears | ASK | `w:ask.secure_clear` |
| DLG-195 | Secure validation | ASK | `w:ask.secure_validation` |
| DLG-196 | Secure status texts | ASK | `w:ask.secure_status_texts` |
| DLG-197 | Written only on submit | ASK | `w:ask.secure_write_on_submit` |
| DLG-198 | Save failure keeps the form on the failed field | ASK | `w:ask.secure_save_failure` |
| DLG-199 | Secure layout | ASK | `w:ask.secure_layout` |
| DLG-200 | Pastes dropped while the secure form is covered | ASK | `w:ask.secure_covered` |
| DLG-201 | Secure file safety (engine side) | ASK | `w:ask.secure_file_mode` |
| DLG-202 | Skills · Installed tab | EXT | `w:ext.skills_installed` |
| DLG-203 | Filter installed skills | EXT | `w:ext.skills_filter` |
| DLG-204 | Enable or disable a skill | EXT | `x:ext.skills_toggle` |
| DLG-205 | Browse skills.sh tab | EXT | `w:ext.skills_browse_tab` |
| DLG-206 | Search skills.sh | EXT | `w:ext.skills_search` |
| DLG-207 | Result rows | EXT | `w:ext.skills_results` |
| DLG-208 | Install a skill | EXT | `w:ext.skills_install` |
| DLG-209 | Ctrl+R refreshes | EXT | `w:ext.skills_refresh` |
| DLG-210 | Status and error line | EXT | `w:ext.skills_status` |
| DLG-211 | Empty states | EXT | `w:ext.skills_empty` |
| DLG-212 | Navigation and close | EXT | `w:ext.skills_nav` |
| DLG-213 | "Skill management is unavailable in this workspace." | EXT | `w:ext.skills_unavailable` |
| DLG-214 | Reload failure still refreshes the catalog | EXT | `w:ext.skills_reload_failure` |
| DLG-215 | Labels `system:`, `user:`, `project:` | EXT | `w:ext.skills_labels` |
| DLG-216 | Toggle skills list for this chat | EXT | `x:ext.kept_list` |
| DLG-217 | Off → Toggleable → On cycle | EXT | `w:ext.kept_cycle` |
| DLG-218 | Groups On / Toggleable / Off | EXT | `w:ext.kept_groups` |
| DLG-219 | Explanation line | EXT | `w:ext.kept_note` |
| DLG-220 | Filter and move; 12 rows and "↓ N more" | EXT | `w:ext.kept_filter` |
| DLG-221 | Empty states and close | EXT | `w:ext.kept_empty` |
| DLG-222 | Agent turns skills on or off: notice | EXT | `w:ext.kept_agent_notice` |
| DLG-223 | Background Processes sheet | LIVE | `w:live.bg_dialog` |
| DLG-224 | Sub-agents sheet (running only) | LIVE | `w:live.helpers_dialog` |
| DLG-225 | Job row | LIVE | `w:live.bg_row` |
| DLG-226 | Helper row | LIVE | `w:live.helper_row` |
| DLG-227 | Lists refresh live, keeping selection | LIVE | `x:live.lists_refresh` |
| DLG-228 | Enter opens a helper's view | LIVE | `w:live.helper_open_view` |
| DLG-229 | Stop a helper, kill a job, no confirmation | LIVE | `w:live.stop_kill` |
| DLG-230 | Empty states and close | LIVE | `w:live.dialogs_empty` |
| DLG-231 | Stop and kill through the engine, seen by the other app | LIVE | `x:live.stop_kill_client_parity` |
| DLG-232 | Toggle MCPs list with Local / Global | EXT | `w:ext.mcp_list` |
| DLG-233 | Switch scope | EXT | `w:ext.mcp_scope` |
| DLG-234 | Choose a row | EXT | `w:ext.mcp_rows` |
| DLG-235 | Toggle a server; notices | EXT | `x:ext.mcp_toggle` |
| DLG-236 | Local override status | EXT | `w:ext.mcp_override` |
| DLG-237 | Status dots | EXT | `w:ext.mcp_dots` |
| DLG-238 | Empty state; close keeps changes | EXT | `w:ext.mcp_empty` |
| DLG-239 | MCP sign-in opens by itself | EXT | `w:ext.mcp_auth_opens` |
| DLG-240 | Start or reopen the browser | EXT | `w:ext.mcp_auth_browser` |
| DLG-241 | Copy the URL | EXT | `w:ext.mcp_auth_copy` |
| DLG-242 | Skip a server | EXT | `w:ext.mcp_auth_skip` |
| DLG-243 | Sign-in states | EXT | `w:ext.mcp_auth_states` |
| DLG-244 | Finish or cancel | EXT | `w:ext.mcp_auth_finish` |
| DLG-245 | Add Image picker opens in the project folder | VIEW | `w:view.picker_opens` |
| DLG-246 | JPG/JPEG/PNG only; folders navigable | VIEW | `w:view.picker_types` |
| DLG-247 | Folders first; hidden files excluded | VIEW | `w:view.picker_order` |
| DLG-248 | Move | VIEW | `w:view.picker_move` |
| DLG-249 | Page, first, last | VIEW | `w:view.picker_page` |
| DLG-250 | Open a folder, go back | VIEW | `w:view.picker_folders` |
| DLG-251 | Confirm a file, close | VIEW | `w:view.picker_confirm` |
| DLG-252 | Preview of the highlighted image | VIEW | `w:view.picker_preview` |
| DLG-253 | Preview is a normal thumbnail | VIEW | `w:view.picker_thumbnail` |
| DLG-254 | Empty folder, links, unreadable files | VIEW | `w:view.picker_edges` |
| DLG-255 | 5 MB limit and read errors | VIEW | `w:view.picker_limits` |
| DLG-256 | Uppercase `.PNG`/`.JPG` selectable (PLAN section 12 fix) | VIEW | `w:view.picker_uppercase` |
| DLG-257 | Mouse added: click, double-click, wheel | VIEW | `w:view.picker_mouse` |
| DLG-258 | Arguments form | PAL | `w:pal.args_form` |
| DLG-259 | Field navigation | PAL | `w:pal.args_nav` |
| DLG-260 | Enter advances, then submits | PAL | `w:pal.args_enter` |
| DLG-261 | Required-argument warning | PAL | `w:pal.args_required` |
| DLG-262 | Scroll and paste | PAL | `w:pal.args_scroll_paste` |
| DLG-263 | "Generating Prompt..." and cancel | PAL | `w:pal.args_generating` |
| DLG-264 | Fallback compaction field "Tokens:" | SET | `w:set.autocompact_field` |
| DLG-265 | Validate and save | SET | `x:set.autocompact_save` |
| DLG-266 | Cancel | SET | `w:set.autocompact_cancel` |
| DLG-267 | Memory limit field "GB:" with hint | SET | `w:set.memory_field` |
| DLG-268 | Usage line | SET | `w:set.memory_usage_line` |
| DLG-269 | Memory validation | SET | `w:set.memory_validate` |
| DLG-270 | Memory outcome notices | SET | `w:set.memory_outcome` |
| DLG-271 | Cancel | SET | `w:set.memory_cancel` |
| DLG-272 | Sub-agent instructions editor and file | SET | `x:set.subagent_instructions_save` |
| DLG-273 | Ctrl+S saves, Esc cancels, Enter is a newline | SET | `w:set.subagent_instructions_keys` |
| DLG-274 | Notification style rows with "current" | SET | `w:set.notify_choices` |
| DLG-275 | Auto | SET | `w:set.notify_auto` |
| DLG-276 | Native | SET | `w:set.notify_native` |
| DLG-277 | OSC: a system notification | SET | `w:set.notify_osc` |
| DLG-278 | Bell: the alert sound | SET | `w:set.notify_bell` |
| DLG-279 | Disabled (sounds too) | SET | `w:set.notify_disabled` |
| DLG-280 | Filter, move, save; "Notifications set to: X" | SET | `x:set.notify_save` |
| DLG-281 | Usage header and unsupported notice | MOD | `w:mod.usage_header` |
| DLG-282 | Window details | MOD | `w:mod.usage_windows` |
| DLG-283 | Reset details | MOD | `w:mod.usage_resets` |
| DLG-284 | Context lines | MOD | `w:mod.usage_context` |
| DLG-285 | Live refresh; R refreshes | MOD | `w:mod.usage_refresh` |
| DLG-286 | Scroll and close | MOD | `w:mod.usage_scroll` |
| DLG-287 | The wheel scrolls Usage (PLAN section 12 fix) | MOD | `w:mod.usage_wheel` |
| DLG-288 | Quit sheet over any sheet or form; typed `exit`/`quit` | PAL | `w:pal.quit_over_dialogs` |
| DLG-289 | Quit texts, "Nope" default | PAL | `w:pal.quit_texts` |
| DLG-290 | Switch and confirm | PAL | `w:pal.quit_keys` |
| DLG-291 | Direct keys | PAL | `w:pal.quit_direct_keys` |
| DLG-292 | Quit mouse | PAL | `w:pal.quit_mouse` |
| DLG-293 | Small window keeps buttons in a row | PAL | `w:pal.quit_small` |
| DLG-294 to DLG-311 | Changes sheet | — | phases 3-4 design §2.8 (P3.6; 18 rows) |

### SET (37)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| SET-001 | Mouse on/off | — | left out |
| SET-002 | Transparent background | — | left out |
| SET-003 | `composer_focus_only`: Settings row and palette toggle | SET | `x:set.composer_focus_only` |
| SET-004 | `agent_cpus_enabled`, `agent_cpus`: Settings rows and palette toggle (Linux) | SET | `w:set.agent_cpus` |
| SET-005 | `thought_line`: Settings row and palette toggle | SET | `x:set.thought_line` |
| SET-006 | `scrollbar`: honored by every scrolling surface | SET | `w:set.scrollbar_mode` |
| SET-007 | `compact_mode`: Settings row and "Toggle Sidebar" | SET | `x:set.compact_mode` |
| SET-008 | `diff_mode`: honored by phase 3's permission sheet | SET | `w:set.diff_mode` |
| SET-009 | Theme setting: the desktop keeps its own `theme`, not shared with the terminal (3.3) | THM | `x:thm.theme_not_shared` |
| SET-010 | `completions.max_depth`, `max_items` honored (JSON only, as in the terminal) | SET | `w:set.completions_limits` |
| SET-011 | `composer_footer`: Settings row opens phase 3's form | SET | `w:set.composer_footer` |
| SET-012 | `auto_compact_token_limit`: Settings row | SET | `w:set.autocompact_row` |
| SET-013 | `disable_auto_summarize` honored (JSON only) | SET | `w:set.auto_summarize` |
| SET-014 | `memory_limit_gb`: Settings row (Linux) | SET | `w:set.memory_limit_row` |
| SET-015 | `notifications`: Settings row; desktop backends | SET | `x:set.notifications_style` |
| SET-016 | `request_timeout` shown as 2 minutes (PLAN section 12 fix) | SET | `w:set.request_timeout` |
| SET-017 | Context file options honored (JSON only) | SET | `w:set.context_paths` |
| SET-018 | Skill paths and disabled skills honored | SET | `w:set.skill_paths` |
| SET-019 | `disabled_tools`, `data_directory`, `tools` honored | SET | `w:set.tools_options` |
| SET-020 | Provider options honored | SET | `w:set.providers_section` |
| SET-021 | Agent CLI providers detected on PATH | SET | `w:set.cli_providers` |
| SET-022 | Abacus provider | SET | `w:set.abacus_provider` |
| SET-023 | Model selection keys | SET | `w:set.model_keys` |
| SET-024 | MCP server config honored | SET | `w:set.mcp_section` |
| SET-025 | LSP options honored | SET | `w:set.lsp_section` |
| SET-026 | Attribution options honored | SET | `w:set.attribution` |
| SET-027 | Default agents | SET | `w:set.agents_info` |
| SET-028 | Other options honored | SET | `w:set.other_options` |
| SET-029 | crushrc options honored | SET | `w:set.crushrc_shown` |
| SET-030 | crushrc UI options honored | SET | `w:set.crushrc_ui_shown` |
| SET-031 | The eight options crushrc cannot set are Settings rows | SET | `w:set.json_only_rows` |
| SET-032 | Metrics opt-out | SET | `w:set.metrics` |
| SET-033 | `permissions.allowed_tools` honored | SET | `w:set.allowed_tools` |
| SET-034 | PreToolUse hooks honored | SET | `w:set.hooks` |
| SET-035 | Keys and tokens never shown or logged by the desktop | SET | `w:set.secrets_hidden` |
| SET-036 | Skill discovery order | SET | `w:set.skill_order` |
| SET-037 | Skill validation | SET | `w:set.skill_validation` |

### THEME (82)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| THEME-001 | Built-in themes, plus the Scholar's edition (default) and charcoal-amber | THM | `w:thm.builtins` |
| THEME-002 | 55 palette roles mapped to app tokens | THM | `u:thm.token_map`, `u:thm.contrast_builtin`, `w:thm.palette_roles` |
| THEME-003 | What the colors mean on every surface | THM | `w:thm.color_meanings` |
| THEME-004 | User theme files, read and written in the terminal's folder and format | THM | `x:thm.user_files` |
| THEME-005 | A theme switch recolors everything | THM | `w:thm.switch_redraw` |
| THEME-006 | Light and dark themes | THM | `w:thm.light_dark` |
| THEME-007 | Code colors follow the theme | THM | `w:thm.syntax_colors` |
| THEME-008 | Themes sheet list | THM | `w:thm.picker_list` |
| THEME-009 | Filter theme names | THM | `w:thm.picker_filter` |
| THEME-010 | Move with live preview | THM | `w:thm.picker_preview` |
| THEME-011 | Apply or undo the preview | THM | `x:thm.apply_sync` |
| THEME-012 | Ctrl+E opens the editor | THM | `w:thm.editor_open` |
| THEME-013 | Ctrl+R renames a user theme inline | THM | `w:thm.rename` |
| THEME-014 | Confirm or cancel rename | THM | `w:thm.rename_confirm` |
| THEME-015 | Ctrl+X deletes a user theme | THM | `w:thm.delete` |
| THEME-016 | Ctrl+D reverts a built-in override | THM | `w:thm.revert` |
| THEME-017 | Help shows allowed actions only | THM | `w:thm.help_allowed` |
| THEME-018 | New theme name "my-theme" | THM | `w:thm.new_theme` |
| THEME-019 | New theme validation | THM | `w:thm.new_validation` |
| THEME-020 | Cancel a new theme | THM | `w:thm.new_cancel` |
| THEME-021 | Editor rows | THM | `w:thm.editor_rows` |
| THEME-022 | Move among fields | THM | `w:thm.editor_move` |
| THEME-023 | Live color preview | THM | `w:thm.editor_preview` |
| THEME-024 | Invalid or empty input | THM | `w:thm.editor_invalid` |
| THEME-025 | Save or cancel the palette | THM | `x:thm.editor_save_sync` |
| THEME-026 | Editor falls back when a file or base is broken | THM | `w:thm.editor_fallback` |
| THEME-027 | Mouse added to the picker, new-theme form and editor | THM | `w:thm.mouse` |
| THEME-028 | Editor row `primary` | THM | `w:thm.field[primary]` |
| THEME-029 | Editor row `secondary` | THM | `w:thm.field[secondary]` |
| THEME-030 | Editor row `accent` | THM | `w:thm.field[accent]` |
| THEME-031 | Editor row `keyword` | THM | `w:thm.field[keyword]` |
| THEME-032 | Editor row `fg_base` | THM | `w:thm.field[fg_base]` |
| THEME-033 | Editor row `fg_subtle` | THM | `w:thm.field[fg_subtle]` |
| THEME-034 | Editor row `fg_more_subtle` | THM | `w:thm.field[fg_more_subtle]` |
| THEME-035 | Editor row `fg_most_subtle` | THM | `w:thm.field[fg_most_subtle]` |
| THEME-036 | Editor row `bg_base` | THM | `w:thm.field[bg_base]` |
| THEME-037 | Editor row `bg_most_visible` | THM | `w:thm.field[bg_most_visible]` |
| THEME-038 | Editor row `bg_less_visible` | THM | `w:thm.field[bg_less_visible]` |
| THEME-039 | Editor row `bg_least_visible` | THM | `w:thm.field[bg_least_visible]` |
| THEME-040 | Editor row `on_primary` | THM | `w:thm.field[on_primary]` |
| THEME-041 | Editor row `separator` | THM | `w:thm.field[separator]` |
| THEME-042 | Editor row `destructive` | THM | `w:thm.field[destructive]` |
| THEME-043 | Editor row `error` | THM | `w:thm.field[error]` |
| THEME-044 | Editor row `warning` | THM | `w:thm.field[warning]` |
| THEME-045 | Editor row `warning_subtle` | THM | `w:thm.field[warning_subtle]` |
| THEME-046 | Editor row `attention` | THM | `w:thm.field[attention]` |
| THEME-047 | Editor row `busy` | THM | `w:thm.field[busy]` |
| THEME-048 | Editor row `info` | THM | `w:thm.field[info]` |
| THEME-049 | Editor row `info_more_subtle` | THM | `w:thm.field[info_more_subtle]` |
| THEME-050 | Editor row `info_most_subtle` | THM | `w:thm.field[info_most_subtle]` |
| THEME-051 | Editor row `success` | THM | `w:thm.field[success]` |
| THEME-052 | Editor row `success_more_subtle` | THM | `w:thm.field[success_more_subtle]` |
| THEME-053 | Editor row `success_most_subtle` | THM | `w:thm.field[success_most_subtle]` |
| THEME-054 | Editor row `diff_insert_fg` | THM | `w:thm.field[diff_insert_fg]` |
| THEME-055 | Editor row `diff_insert_code_bg` | THM | `w:thm.field[diff_insert_code_bg]` |
| THEME-056 | Editor row `diff_insert_gutter_bg` | THM | `w:thm.field[diff_insert_gutter_bg]` |
| THEME-057 | Editor row `diff_delete_fg` | THM | `w:thm.field[diff_delete_fg]` |
| THEME-058 | Editor row `diff_delete_code_bg` | THM | `w:thm.field[diff_delete_code_bg]` |
| THEME-059 | Editor row `diff_delete_gutter_bg` | THM | `w:thm.field[diff_delete_gutter_bg]` |
| THEME-060 | Editor row `yolo` | THM | `w:thm.field[yolo]` |
| THEME-061 | Editor row `plan` | THM | `w:thm.field[plan]` |
| THEME-062 | Editor row `plan_more_subtle` | THM | `w:thm.field[plan_more_subtle]` |
| THEME-063 | Editor row `button` | THM | `w:thm.field[button]` |
| THEME-064 | Editor row `button_subtle` | THM | `w:thm.field[button_subtle]` |
| THEME-065 | Editor row `button_inactive` | THM | `w:thm.field[button_inactive]` |
| THEME-066 | Editor row `button_hovered` | THM | `w:thm.field[button_hovered]` |
| THEME-067 | Editor row `ansi_black` | THM | `w:thm.field[ansi_black]` |
| THEME-068 | Editor row `ansi_red` | THM | `w:thm.field[ansi_red]` |
| THEME-069 | Editor row `ansi_green` | THM | `w:thm.field[ansi_green]` |
| THEME-070 | Editor row `ansi_yellow` | THM | `w:thm.field[ansi_yellow]` |
| THEME-071 | Editor row `ansi_blue` | THM | `w:thm.field[ansi_blue]` |
| THEME-072 | Editor row `ansi_magenta` | THM | `w:thm.field[ansi_magenta]` |
| THEME-073 | Editor row `ansi_cyan` | THM | `w:thm.field[ansi_cyan]` |
| THEME-074 | Editor row `ansi_white` | THM | `w:thm.field[ansi_white]` |
| THEME-075 | Editor row `ansi_bright_black` | THM | `w:thm.field[ansi_bright_black]` |
| THEME-076 | Editor row `ansi_bright_red` | THM | `w:thm.field[ansi_bright_red]` |
| THEME-077 | Editor row `ansi_bright_green` | THM | `w:thm.field[ansi_bright_green]` |
| THEME-078 | Editor row `ansi_bright_yellow` | THM | `w:thm.field[ansi_bright_yellow]` |
| THEME-079 | Editor row `ansi_bright_blue` | THM | `w:thm.field[ansi_bright_blue]` |
| THEME-080 | Editor row `ansi_bright_magenta` | THM | `w:thm.field[ansi_bright_magenta]` |
| THEME-081 | Editor row `ansi_bright_cyan` | THM | `w:thm.field[ansi_bright_cyan]` |
| THEME-082 | Editor row `ansi_bright_white` | THM | `w:thm.field[ansi_bright_white]` |

### NOTE (9)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| NOTE-001 | Window title "Chat title · project" | SYS | `w:sys.window_title` |
| NOTE-002 | Turn-finished sound and notification | SYS | `w:sys.finished_alert` |
| NOTE-003 | Notifications only when not focused and shown; sounds also when focused | SYS | `w:sys.focus_rule` |
| NOTE-004 | Backends: system notification, alert sound, off | SYS | `w:sys.backends` |
| NOTE-005 | Sounds "message-new-instant" and "complete" through the terminal's player chain | SYS | `w:sys.sounds` |
| NOTE-006 | Notification kinds | SYS | `w:sys.notification_kinds` |
| NOTE-007 | Taskbar badge for blocked, done and error | SYS | `w:sys.badge_status` |
| NOTE-008 | Taskbar progress while busy | SYS | `w:sys.taskbar_progress` |
| NOTE-009 | Focus clears done and error | SYS | `w:sys.focus_clears` |

### REM (19)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| REM-001 | Start sharing: setup sheet, 2 s re-checks, start errors | SHARE | `w:share.start_setup` |
| REM-002 | Guided Tailscale setup states | SHARE | `w:share.setup_states` |
| REM-003 | Setup buttons | SHARE | `w:share.setup_choices` |
| REM-004 | Setup mouse and cancel | SHARE | `w:share.setup_mouse` |
| REM-005 | Setup step runs `crush remote setup <action>` in a terminal tab | SHARE | `w:share.setup_terminal_tab` |
| REM-006 | Sharing status sheet; "REMOTE" badge; phone connected notice | SHARE | `w:share.status_dialog` |
| REM-007 | "Turn off" / "Keep on" | SHARE | `w:share.turn_off` |
| REM-008 | Status stays current | SHARE | `w:share.status_live` |
| REM-009 | A launcher-opened window shares quietly | SHARE | `w:share.quiet_start` |
| REM-010 | Phone actions and their refusal texts | SHARE | `w:share.phone_actions` |
| REM-011 | The window tells the phone what it shows | SHARE | `w:share.presence` |
| REM-012 | Tailscale-only listening (engine side) | SHARE | `w:share.tailnet_only` |
| REM-013 | Phone event stream (engine side) | SHARE | `w:share.event_stream` |
| REM-014 | Share safety (engine side) | SHARE | `w:share.safety` |
| REM-015 | Settings: "Phone opens new chats in: Terminal / Desktop app" | SHARE | `w:share.launcher_setting` |
| REM-016 | Settings: launcher password | SHARE | `w:share.launcher_password` |
| REM-017 | The launcher opens a desktop window that reports its launch ID | SHARE | `w:share.launcher_desktop_window` |
| REM-018 | Phone step details (engine side) | SHARE | `w:share.step_details` |
| REM-019 | Phone step labels (engine side) | SHARE | `w:share.step_labels` |

### UPD (8)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| UPD-001 | Upstream update notice | SYS | `w:sys.upstream_notice` |
| UPD-002 | Agent CLI check at most hourly | SYS | `w:sys.cli_check_hourly` |
| UPD-003 | CLI update offer in the Q8 slot | SYS | `w:sys.cli_offer` |
| UPD-004 | Crush update check and first offer | SYS | `w:sys.crush_offer` |
| UPD-005 | Background merge, build and test | SYS | `w:sys.crush_build_background` |
| UPD-006 | AI fix step and log | SYS | `w:sys.crush_ai_fix` |
| UPD-007 | Install offer | SYS | `w:sys.crush_install_offer` |
| UPD-008 | Offers become update notices when another form holds the slot | SYS | `w:sys.offer_to_status` |

### MISC (25)

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| MISC-001 | Full-screen terminal mode | — | left out |
| MISC-002 | Debug overlay with `CRUSH_UI_DEBUG=true` | SYS | `w:sys.debug_overlay` |
| MISC-003 | Background-color resend | — | left out |
| MISC-004 | Terminal capability probes | — | left out |
| MISC-005 | Konsole scroll marker | — | left out |
| MISC-006 | Exit banner | — | left out |
| MISC-007 | Pictures are image elements | VIEW | `w:view.images_are_img` |
| MISC-008 | `crush prune-diffs` | — | command line only |
| MISC-009 | `crush run` | — | command line only |
| MISC-010 | `crush run` flags | — | command line only |
| MISC-011 | `crush models`: the model picker lists the same models | MOD | `w:mod.models_list_matches_cli` |
| MISC-012 | Sign in: Settings rows for Hyper, Copilot, ChatGPT, Grok | AUTH | `w:auth.settings_sign_in` |
| MISC-013 | Sign out: Settings rows | AUTH | `x:auth.settings_sign_out` |
| MISC-014 | `crush session`: the left list and the Sessions sheet | SES | `w:ses.cli_parity` |
| MISC-015 | `crush projects`, `save-project`, `open-project`: the Open Project sheet | SES | `w:ses.projects_cli_parity` |
| MISC-016 | Log viewer | SYS | `w:sys.log_viewer` |
| MISC-017 | "Usage report" opens the stats page | SYS | `w:sys.usage_report` |
| MISC-018 | About panel with Crush's folders | SYS | `w:sys.about_folders` |
| MISC-019 | "Refresh provider list" | MOD | `w:mod.refresh_providers` |
| MISC-020 | `crush schema` | — | command line only |
| MISC-021 | `crush server` | — | command line only |
| MISC-022 | Secrets-guard hook | — | command line only |
| MISC-023 | About shows the version; `crush-desktop --version` | SYS | `w:sys.about_version` |
| MISC-024 | Closing a window or quitting discards an empty draft chat | LST | `x:lst.quit_discards_draft` |
| MISC-025 | Crash notice with "Open report" and "Restart" | SYS | `w:sys.crash_notice` |

### Rows moved in from the phases 3-4 design (2)

The phases 3-4 design moves these two to phase 6 (its §2.10; PLAN.md:305 for TYPE-015); this design owns them.

| Row | Desktop surface | Piece | Check or status |
|---|---|---|---|
| TYPE-015 | Ctrl+O in the typing box: edit the prompt in `$VISUAL`/`$EDITOR` in a terminal tab; the result replaces the typing box's text (2.26) | TERM | `w:term.editor_ctrl_o` |
| START-020 | Crash guard: a panic leaves a report and the next start shows the crash notice; empty reports older than 24 h are deleted at start (2.28) | SYS | `w:sys.crash_guard` |

## 2. Surfaces

Each surface lists: what the desktop does, the terminal facts it copies (file:line), the engine routes and events with their exact JSON names, its state, keys and focus, errors and empty states, and what happens when the terminal app changes the same thing at the same time. The checklist rows (CHECKLIST.md) hold the full per-row edge cases; this section pins what builders need beyond them. The checklist's evidence lines were made in `/home/braxtenchenay/dev/crush`; for `internal/ui` they match this worktree line for line (spot-checked: `dialog.go:46`, `:108`, `:209`, `model_select.go:120`, `commands.go:159`).

### 2.0 Rules every surface follows

1. **One sheet stack per window** (2.1). Sheets are drawn inside the window. The only native dialog is the system folder dialog behind the folder picker's "Browse…" button (PLAN section 7: "plus the system folder dialog"); the folder picker itself (2.21) and the image picker (2.10) are sheets.
2. **Busy.** A window is busy when the chat it shows has `is_busy: true` in its newest `proto.Session` record, or the window has a `!` shell command running, or a prompt send is in flight. A window showing a new chat with no session is never busy. This is the terminal's rule: `internal/ui/model/send.go:23-31`, `internal/ui/model/workspace_cache.go:152-160` ("Work in another session must not block its model or effort selection"). Phase 3 owns this state (P3.3's `busy(id)`, P3.7's `composer.shells`); surfaces here read it through S5's `windowBusy()` (`src/lib/busy.js`, 6.2).
3. **Notices.** A text the terminal reports with `util.ReportInfo`/`NewInfoMsg` is an info notice, `ReportWarn`/`NewWarnMsg` a warning, `ReportError` an error. The desktop sends the same text, same kind, through `ui.notify("info" | "warn" | "error", text)` (S3) to phase 3's notice line. Go error strings arriving from the engine (`{"message": …}` bodies) are shown as received.
4. **Engine routes.** `…/path` means `/v1/workspaces/{id}/path`. Bodies use the exact JSON names of `internal/proto` or of engine design section 5. "id required" means the route needs the window's `client_id` (engine design, Conventions). Current routes are in `internal/rpc/routes.go:25-152`, `routes_lifecycle.go`, `routes_live.go`, `routes_share.go` and `routes_tree.go`; 6.5 lists every one this design calls, with its `api.js` function.
5. **Writes go through the engine.** The desktop writes only these files itself, each because the engine has no route and the terminal also writes it in-process: `projects.json` (2.5), `subagent-instructions.md` (2.12), and the desktop's own settings file (section 4). Every other change is an engine call, so the engine's `config_changed` event and its locks cover it.
6. **Mouse-first.** PLAN "Purpose" adds "mouse-first navigation". Every list row, button and tab is clickable, even where the terminal has no mouse handler (for example the model picker, `internal/ui/dialog/models/models.go:172`). A click selects; a double-click within 400 ms or a click on a selected row's action opens or confirms, matching the terminal's own double-click rule where it has one (`internal/ui/dialog/auth/auth_method.go:36`, `internal/ui/dialog/sessions/sessions.go:247`). The wheel scrolls every scrolling area.
7. **Paste.** DLG-017's entry point is Ctrl+V, so every desktop text field takes Ctrl+V, Shift+Insert and middle-click paste. The terminal's split between key messages and terminal paste messages has no desktop meaning (9.4, D1). Exception: the helper view ignores pastes (LIVE-021), and so does a covered secure form (DLG-200).
8. **Text selection.** Static sheet text (question bodies, descriptions, notices in sheets) is selectable with the mouse and copied with Ctrl+C or Ctrl+Shift+C, the system way (DLG-182 asks for copy; 9.4, D2).
9. **Focus.** Opening a sheet moves focus to its first field or its list. Closing a sheet returns focus to where it was before the sheet opened (the typing box by default). With `composer_focus_only` on, clicks outside sheets never move focus out of the typing box (SET-003, PLAN section 7).
10. **Event names.** SSE events are named by their payload type (`internal/pubsub/events.go:19-46`), each as `{type: <payload type>, payload: {type: "created"|"updated"|"deleted", payload: <record>}}` (`events.go:51-52`, `:66-67`): `lsp_event`, `mcp_event`, `permission_request`, `permission_notification`, `message`, `session`, `file`, `agent_event`, `config_changed` (`{workspace_id}`, `internal/proto/proto.go:46-48`), `skills_event`, `run_complete`, `update_available`, `question_batch_request`, `question_batch_notification`, and the engine's new `helper`, `shell_output`, `limits`, `memory_status`, `cli_updates_installed`, `tree_changed`, `chat_link`, `remote_action`, `remote_status`, `secure_form`, `secure_form_closed`, `prompt_pending`. Question and permission events reach only the windows that show the asking chat (`internal/server/events.go:120-150`).

### 2.1 Shared sheet frame and kit (KIT; DLG-001 to DLG-020)

**What the desktop does.** One stack of sheets per window, drawn over the window's content on OverlayHost's veil (`--scrim`). Only the front sheet takes keys; sheets underneath stay drawn but inert (`inert` attribute). The stack is S3's `lib/dialogs.js` (3-4 design §1.10), with exactly these calls, used by every piece:

| Call | Behavior | Terminal fact |
|---|---|---|
| `registerDialog(id, Component, {veil = true})` | Once per sheet, from its piece's plugin file; `veil: false` only for the docked Themes sheet and theme editor (S5) | — |
| `openDialog(id, props, {grace})` | Pushes a sheet. If a sheet with the same `id` is open, brings it to the front instead and returns `false`. | `dialog.go:108-113`, `:209`; reopen brings forward `internal/ui/model/dialogs.go:31` |
| `bringToFront(id)` | Moves the sheet to the top. | `dialog.go:209` |
| `closeDialog(id)` | Removes that sheet wherever it is. | `dialog.go:156` |
| `closeFront()` | Removes the top sheet. | `dialog.go:166` |
| `hasDialog(id)` | True if open anywhere in the stack. | `dialog.go:99` |
| `frontDialog()` | The top sheet's id or `null`. | — |

Sheet ids are the terminal's dialog ids where one exists (`internal/ui/dialog/ids.go`), so "has the Permissions sheet" means the same thing in both apps. One exception: the Sessions sheet is `sessions`, the id the phases 3-4 design already opens (3-4 design §1.10), where the terminal says `session` (9.4, D18). The ids, by piece: PAL `commands`, `arguments`, `quit`; MOD `models`, `reasoning`, `usage`; AUTH `auth_method`, `api_key_input`, `oauth`, `aws_sso`; SES `sessions`, `projects`; EXT `skills`, `kept_skills`, `mcp_toggles`, `mcp_auth`; SET `settings`, `autocompact`, `memory_limit`, `subagent_instructions`, `notifications`; THM `theme`, `theme_new`, `theme_editor`; VIEW `filepicker`; LIVE `background`, `sub-agents`; LST `folder_picker`; TERM `terminal_close`; SHARE `remote`, `remote_setup`, `launcher_password`; SYS `logs`, `about`, `crash`.

**Keys.** KIT's plugin binds one handler for every key in S3's `overlay` scope (`bindKey("overlay", "*", …)`, S5's catch-all) and hands the key to the front sheet's `onKey`. While a sheet is open, S3's router asks no other scope, except that Ctrl+C asks `global` first unless the quit sheet is open (3-4 design §1.8; `desktop/src/lib/keys.js:52-67`). The zoom keys keep their own listener.

**Key grace (DLG-004 to DLG-006).** A sheet opened with `{grace: true}` absorbs key presses until no key arrived for 425 ms, or 1.5 s after opening, whichever comes first (`dialog.go:49-55`, `:140-152`, `:226-229`). Each absorbed key restarts the quiet timer. Mouse events and engine results are never absorbed. If the same sheet id closed less than 500 ms ago, the grace is skipped (`dialog.go:125-137`, `:173-175`). The terminal opens these sheets with grace: Permissions (phase 3, `internal/ui/model/dialogs.go:270`), AWS SSO (`internal/ui/model/agent_events.go:226`), and the sign-in sheets opened from the model flow (`internal/ui/model/model_select.go:351`, `:383`). The grace is S3's `lib/grace.js`, used through `openDialog(id, props, {grace: true})`, the same code as phase 3's permission prompt (3-4 design §1.10).

**Close keys (DLG-003).** Esc and Alt+Esc close the front sheet (`dialog.go:13-16`) unless the sheet handles them itself (deny, back, save, cancel; each surface says so). Clicking the veil does nothing. (The phases 3-4 design's Changes sheet closes on a click outside, its DLG-308.)

**Fit (DLG-007).** A sheet is centered in its window and never larger than the window minus 24 picture px on each side; content scrolls inside. On resize the sheet re-centers and re-fits in the same frame. Onboarding sheets (model picker and sign-in during onboarding) sit at the bottom left of the main column, as in the terminal (`common.go:73-76`, `:151-153`).

**Frame parts (DLG-008).** Title, optional title-side controls on the title line (for example the palette's System/User/MCP selector), body, help footer (`internal/ui/dialog/common.go:99`). When the title-side controls cannot sit beside the title with a gap, they are hidden, not cut (`common.go:111-118`). Group headings inside a body are small-caps labels (look 5.1); a sheet adds no label text of its own.

**Help footer (DLG-009).** One line of `key description` pairs separated by " · ". Disabled bindings are left out. Pairs that do not fit are dropped from the end and replaced by "…", never ending on a separator (`internal/ui/dialog/internal/kit/help.go:26-65`). Each sheet passes its pairs in the terminal's order; the footer is `src/components/kit/HelpFooter.svelte`.

**Filter lists (DLG-010 to DLG-012).** `src/components/kit/FilterList.svelte` with `src/lib/kit/fuzzy.js` (`fuzzyFind`), a port of `github.com/sahilm/fuzzy` v0.1.3 `FindFrom`: same bonuses (first char 10, after separator 20, camel case 20, adjacent 5 growing as `current*2+5`), same leading penalty (−5 per unmatched leading char, floor −15), separators `/-_ .\`, stable sort by score (`/home/braxtenchenay/go/pkg/mod/github.com/sahilm/fuzzy@v0.1.3/fuzzy.go:29-37`, `:110-113`, `:272-280`). Each row's filter string is what the terminal filters on (per surface). An empty query shows all rows in their own order with no marks (`internal/ui/list/filterable.go:91-102`). Matched letters are underlined (1 px, `--ink`); the selected row has the selected look; long titles end with "…" (`kit/listitem.go:88-120`). A right-side info column (shortcut, "current", time) takes at most half the row and is cut with "…" first (`kit/listitem.go:75-77`). Lists with an info budget hide the whole column together when the widest info would pass the budget: 25% for the palette, 35% for Sessions (`kit/infocolumn.go:13-14`, `:30`). A scrollbar appears only when the list overflows; its space is reserved only then (`kit/layout.go:59`, `:83`). Setting a new query selects the first match and scrolls to the top (`commands.go:219-226`).

**One-line fields (DLG-013 to DLG-017).** `src/components/kit/Field.svelte` wraps an `<input>` and adds the terminal's keys on top of the browser's: Ctrl+B/Ctrl+F (char back/forward), Alt+B/Alt+F and Alt+←/→ (word), Ctrl+H (backspace), Ctrl+D (delete forward, unless the sheet reserves it), Ctrl+W and Alt+Backspace (delete word back), Alt+D and Alt+Delete (delete word forward), Ctrl+U (clear before caret), Ctrl+K (clear after caret), Ctrl+A/Home and Ctrl+E/End (line ends). Source: bubbles textinput key map (CHECKLIST DLG-013 to DLG-016 evidence). A sheet that binds one of these keys (for example Ctrl+E in the model picker, Ctrl+D in Themes, Ctrl+K nowhere) gets it first, as in the terminal ("Screen keys win").

**Multiline editors (DLG-018, DLG-019).** `src/components/kit/Area.svelte` wraps a `<textarea>` with: Ctrl+P/Ctrl+N (line up/down), PgUp/PgDn, Ctrl+Home/End and Alt+</Alt+> (start/end), Shift+arrows and Ctrl/Alt+Shift+←/→ (select), Ctrl+G (select all), Ctrl+Shift+C (copy), Alt+C/Alt+L/Alt+U (capitalize/lower/upper word), Ctrl+T (swap characters). Question forms may take ↑/↓ for choices first (DLG-018 note).

**Buttons and rules (DLG-020).** `src/components/kit/Buttons.svelte`: real `<button>`s in a row with one underlined shortcut letter each (focused, hovered, inactive looks), click areas the full button. Section headers are a small-caps label followed by a 1 px rule (`components/Rule.svelte`); the terminal's "Title ╱╱╱" gradient becomes the title plus a rule with + marks (look 5.1). The model info block (token %, "~" when estimated, "W" past 80%, cost, Hypercredits) is phase 3's sidebar; the kit only provides the rule and label parts.

**State.** Per window: the stack (S3's `dialogs`), the grace times and last close per id (S3's `lib/grace.js`), and each open sheet's key handler (`src/lib/kit/sheetkeys.js`). Nothing is persisted.

**Concurrent terminal changes.** None at the frame level; each surface handles its own.

### 2.2 Command palette and global keys (PAL; CMD-001 to CMD-013, CMD-019 to CMD-023, CMD-033, CMD-035 to CMD-037, CMD-050, CMD-057 to CMD-064; arguments DLG-258 to DLG-263)

**Opening (CMD-001).** Ctrl+P anywhere, or "/" typed into an empty typing box (the field, P3.7, calls `runAction("palette.open")`, which PAL registers; 3-4 design §1.10). The sheet id is `commands`; reopening brings it forward.

**Sheet.** Title "Commands"; filter placeholder "Type to filter"; help footer "tab switch selection · ↑/↓ choose · enter confirm · esc cancel" (`internal/ui/dialog/commands.go:104-136`, `:305-315`, `:325-332`). Width at most 70 terminal columns' worth (`kit/layout.go:16`): on the desktop 760 picture px; height at most 20 rows' worth: 640 picture px (look 5.3).

**Categories (CMD-006, CMD-007).** System always; User only when there are custom commands or user skills; MCP only when there are MCP prompts. The selector (radio marks with labels "System", "User", "MCP", `commands.go:29`, `:253-277`) sits on the title line. Tab and Shift+Tab cycle among the categories that exist, clearing the filter and selecting the first row (`commands.go:200-209`, `:342-396`).

**System entries.** Built in `src/lib/palette/entries.js` from the terminal's order and conditions (`commands.go:461-682`). Each entry runs an action (`runAction(id)`, `lib/actions.js`) or opens a sheet (`openDialog(id)`) that its owner registers; the targets are listed under the table. The order and labels below are fixed here, so no piece reorders them, and an entry whose action is not registered (yet) is hidden, not broken.

| # | ID | Label (state-dependent labels in /) | Shortcut | Aliases | Shown when | Action (piece) |
|---|---|---|---|---|---|---|
| 1 | `new_session` | New Session | ctrl+n | clear | always | new chat in this window (phase 3's `chat.new`) |
| 2 | `switch_session` | Sessions | ctrl+s | — | always | Sessions sheet (SES) |
| 3 | `switch_model` | Switch Model | ctrl+l | — | always | model picker (MOD) |
| 4 | `usage` | Usage | — | — | always | Usage sheet (MOD) |
| 5 | `autocompact` | Fallback compaction | — | — | always | sheet (SET) |
| 6 | `undo-file-restore` | Undo Last File Restore | — | — | chat has a session | phase 4's undo preview |
| 7 | `tree` | Session Tree | — | — | chat has a session | phase 4's tree |
| 8 | `fork` | Fork from Prompt | — | — | chat has a session | phase 4's tree, fork mode |
| 9 | `clone` | Clone Current Branch | — | — | chat has a session | phase 4's clone |
| 10 | `summarize` | Summarize Session | — | compact | chat has a session | 2.2 below (PAL) |
| 11 | `toggle_fast_mode` | Enable / Disable FAST Mode | — | fast, speed | coder model supports FAST | MOD |
| 12 | `toggle_ultracode` | Turn On / Turn Off Ultracode | — | ultracode, ultra, workflows | supports Ultracode | MOD |
| 13 | `toggle_thinking` | Enable / Disable Thinking Mode | — | — | model can reason and supports the thinking toggle | MOD |
| 14 | `select_reasoning_effort` | Select Reasoning Effort | — | effort | model can reason and has reasoning levels | MOD |
| 15 | `toggle_sidebar` | Toggle Sidebar | — | — | chat has a session and the window is at least the terminal's 120-column breakpoint (desktop: the three-column layout is showing) | SET |
| 16 | `file_picker` | Open File Picker | ctrl+f | — | chat has a session and the model takes images (`supports_attachments`) | VIEW |
| 17 | `open_external_editor` | Open External Editor | ctrl+o | — | `$EDITOR` set in the app's environment | TERM |
| 18 | `enable_docker_mcp` | Enable Docker MCP Catalog | — | — | Docker MCP not enabled and `docker mcp version` works | EXT |
| 19 | `disable_docker_mcp` | Disable Docker MCP Catalog | — | — | Docker MCP enabled | EXT |
| 20 | `toggle_mcps` | Toggle MCPs | — | — | at least one MCP server configured | EXT |
| 21 | `toggle_pills` | Toggle To-Dos/Queue, Toggle Queue, Toggle To-Dos | ctrl+t | — | chat has to-dos or a queue | phase 3's pills toggle |
| 22 | `select_notifications` | Notification Style | — | — | always | SET |
| 23 | `toggle_yolo` | Toggle Yolo Mode | ctrl+y | — | always | 2.2 below (PAL) |
| 24 | `toggle_help` | Toggle Help | ctrl+g | — | always | phase 3's help bar |
| 25 | `init` | Initialize Project | — | — | always | 2.2 below (PAL) |
| 26 | `save_project` | Save Project | — | — | always | SES |
| 27 | `open_project` | Open Project | — | projects | always | SES |
| 28 | `background` | Background Processes | ctrl+x | processes, kill | always | LIVE |
| 29 | `sub_agents` | Sub-agents | — | agents, tasks, stop | always | LIVE |
| 30 | `remote` | Remote Control | — | rc, phone | always | SHARE |
| 31 | `update_clis` | Update Agent CLIs | — | upgrade, version | always | SYS |
| 32 | `update_crush` | Update Crush | — | upgrade, upstream, release | always | SYS |
| 33 | `skills` | Skills | — | install skills, enable skills, disable skills | always | EXT |
| 34 | `kept_skills` | Toggle skills | — | keep skills on, remind skills every message, bring skills back after compression | always | EXT |
| 35 | `toggle_composer_focus` | Keep Focus on Composer / Let Tab Move Focus | — | tab, focus | always | SET |
| 36 | `toggle_thought_line` | Hide Thought Line / Show Thought Line | — | thinking, thoughts, status, working | always | SET |
| 37 | `customize_composer` | Customize Composer | — | footer, stats, status | always | phase 3's form |
| 38 | `subagent_instructions` | Sub-agent Instructions | — | subagents, sub-agents, spawn, agents | always | SET |
| 39 | `memory_limit` | Memory Limit | — | ram, memory, swap | always | SET |
| 40 | `switch_theme` | Themes | — | — | always | THM |
| 41 | `toggle_agent_cpus` | Run AI Work on CPU Half 2 / Let AI Work Use Whole CPU | — | cpu, cores, stutter | Linux (CPU affinity supported) | SET |
| 42 | `settings` | Settings | ctrl+, | preferences, options | always (desktop addition, 9.4 D3) | SET |
| 43 | `log_viewer` | Logs | — | log | always (desktop surface for MISC-016) | SYS |
| 44 | `usage_report` | Usage report | — | stats | always (MISC-017) | SYS |
| 45 | `refresh_providers` | Refresh provider list | — | providers, catalog | always (MISC-019) | MOD |
| 46 | `about` | About Crush | — | version, folders | always (MISC-018, MISC-023) | SYS |
| 47 | `quit` | Quit | ctrl+c | exit | always, last | PAL (2.24) |

**Targets** (7.4): 1 `runAction("chat.new")` (P3.9); 2 `openDialog("sessions")`; 3 `openDialog("models")`; 4 `openDialog("usage")`; 5 `openDialog("autocompact")`; 6 to 9 `runAction("restore.undo")`, `runAction("tree.open")`, `runAction("tree.fork")`, `runAction("tree.clone")` (P4.4; hidden until it registers them, D20 of the phases 3-4 design); 10 PAL's own (below); 11 to 13 `runAction("model.fast")`, `runAction("model.ultracode")`, `runAction("model.thinking")`; 14 `openDialog("reasoning")`; 15 `runAction("sidebar.toggle")`; 16 `openDialog("filepicker")`; 17 `runAction("editor.open")`; 18 and 19 `runAction("docker.enable")`, `runAction("docker.disable")`; 20 `openDialog("mcp_toggles")`; 21 `runAction("todos.toggle")` (P3.7); 22 `openDialog("notifications")`; 23 PAL's own; 24 `runAction("help.toggle")` (P3.8); 25 PAL's own; 26 `runAction("project.save")`; 27 `openDialog("projects")`; 28 `openDialog("background")`; 29 `openDialog("sub-agents")`; 30 `runAction("remote.open")`; 31 and 32 `runAction("updates.clis")`, `runAction("updates.crush")`; 33 `openDialog("skills")`; 34 `openDialog("kept_skills")`; 35 `runAction("composer.focusOnly")`; 36 `runAction("thoughtline.toggle")`; 37 `runAction("composer.customize")` (P3.8); 38 `openDialog("subagent_instructions")`; 39 `openDialog("memory_limit")`; 40 `openDialog("theme")`; 41 `runAction("cpus.toggle")`; 42 `openDialog("settings")`; 43 `openDialog("logs")`; 44 `runAction("usage.report")`; 45 `runAction("providers.refresh")`; 46 `openDialog("about")`; 47 PAL's own: `closeWindow()` (S5's `lib/closing.js`), with no sheet.

Rows 42 to 46 are desktop surfaces for rows the terminal serves from the command line (MISC-016 to MISC-019, MISC-023). The terminal's "Use Terminal Background" (`commands.go:642-646`, CMD-047) and "Disable/Enable Mouse" (`:667-671`, CMD-054) are left out (Q10). IDs `subagent_instructions` and `memory_limit` are the terminal's `SubAgentInstructionsID` and `MemoryLimitID` (`commands.go:661-662`). The state-dependent labels read the same config values as the terminal (`commands.go:508-531`, `:642-679`) from `GET …/config` (`internal/rpc/routes.go:41`), refreshed on `config_changed`.

**User entries (CMD-057, CMD-058).** Custom commands are Markdown files the terminal loads in-process from the user's commands folder, `~/.crush/commands` and `<data_directory>/commands`; user skills appear as entries with their description line under the title. There is no engine route for custom commands (engine gap G-7, 9.1); the desktop lists them through `GET …/commands` (G-7) returning `[{id, name, content, arguments[{name, title, required}], skill{name, description, skill_file_path}|null}]` (G-7's contract, `CustomCommand` in 6.7). Entry ids are `custom_` + id (`commands.go:439`). Choosing a skill entry attaches a skill chip to the typing box through P3.7's `attachSkill({id: skill_file_path, name})` (`commands.go:430-431`). Choosing a command with arguments opens the arguments sheet (below); without arguments its content is sent as the prompt (P3.7's `send`).

**MCP entries (CMD-059).** From `GET …/mcp/prompts` → `[proto.MCPPrompt]` with `id, title, description, prompt_id, client_id, arguments[{id, title, description, required}]` (`internal/rpc/routes.go:138`). Entry id `mcp_` + `id`, label `prompt_id` (`commands.go:446-455`). Choosing one opens the arguments sheet if it has arguments, then calls `POST …/mcp/get-prompt` with `{client_id, prompt_id, args}` → `{prompt}` (`internal/proto/requests.go:167-176`) and sends the prompt. While waiting, the palette footer shows "Generating Prompt..." with a spinner (`commands.go:313-315`, CMD-010). The lists refresh on `mcp_event` events (`internal/pubsub/events.go:20`) whose `type` is `prompts_list_changed` or `state_changed` (`internal/proto/mcp.go:86-94`), keeping filter and selection (`commands.go:398-417`, CMD-011).

**Keys inside the palette (CMD-003 to CMD-005, CMD-012).** ↑ and Ctrl+P move up, ↓ moves down, both wrap (`commands.go:178-193`). Enter and Ctrl+Y run the selected entry (`:194-199`). Any other key that equals a visible entry's shortcut runs that entry (`:210-218`); the check order is Close, Previous, Next, Select, Tab, Shift+Tab, then shortcuts, so Ctrl+Y never runs Toggle Yolo and Ctrl+P never opens anything from inside the palette, while Ctrl+N runs New Session. The desktop keeps this order, dead Ctrl+Y included (coordinator decision on pass 1's Q-P1, `desktop/PLAN.md:373`). Ctrl+C over the palette opens the quit sheet, unless text is selected (2.24; PAL's own Ctrl+C binding). Esc and Alt+Esc close.

**Mouse (CMD-013).** Hover moves the highlight; click runs; wheel scrolls.

**Shortcut column (CMD-008).** The shortcut of each entry shows on the right; the whole column hides when the widest shortcut would take more than 25% of the row (`commands.go:301-302`).

**Summarize (CMD-023).** Busy: warning "Agent is busy, please wait before summarizing session..." (`internal/ui/model/dialog_actions.go:143`, `:150`). Otherwise `POST …/agent/sessions/{sid}/summarize` (`internal/rpc/routes.go:110`) through `api.summarize(sid, callId)`. The engine writes the summary inside that request, so it goes with a `callId` and no time limit (3-4 design §1.2.4, D40); closing the window aborts it (`connection::close`, 3-4 design §1.2.6). While it runs the chat is busy (the summary registers among the chat's active requests, 3-4 design §1.2.4), so Esc in the typing box follows phase 3's rule for a busy chat (TYPE-046).

**Toggle Yolo (CMD-035).** Reads `GET …/permissions/skip` → `{skip}`, then `POST …/permissions/skip` `{skip: !skip}` (`internal/rpc/routes.go:86-87`, `internal/proto/proto.go:283-285`). The palette toggle is silent, like the terminal's (coordinator decision on pass 1's Q-P2, `desktop/PLAN.md:373`); Ctrl+Y outside the palette is phase 3's and shows its banner. YOLO is per project (section 4). The other app sees the change through `config_changed` after G-4 (9.1).

**Initialize Project (CMD-037).** Busy: warning "Agent is busy, please wait before initializing the project...". The terminal shows its summarize text here, a copy slip (`dialog_actions.go:150`); the desktop shows the corrected text and the terminal is left for the owner (coordinator decision on pass 1's Q-P3, `desktop/PLAN.md:374`; 9.4, D19). Otherwise `GET …/project/init-prompt` → `{prompt}` and `POST …/project/init` (`internal/rpc/routes.go:49-50`), sending the prompt as the chat's next message through P3.7's `send`.

**Phase 3/4 entries (CMD-019 to CMD-022, CMD-033, CMD-036, CMD-050).** The palette calls the actions `tree.open`, `tree.fork`, `tree.clone`, `restore.undo` (P4.4), `todos.toggle` (P3.7), `help.toggle` and `composer.customize` (P3.8) (3-4 design §1.10). Until an action is registered, its entry is hidden, not broken. "Undo Last File Restore" follows the engine's file restore as built (engine build items 13 to 16: the `undo_id` a fork carries, the two-call restore then move, the kept tree revision and the 409 `plan_expired`); P4.4 owns that behavior (3-4 design §3.6), and the palette only runs the action.

**Global keys (CMD-060 to CMD-064).** Bound with `bindKey` (S3's `lib/keys.js`). The terminal's priority order maps onto its scopes (CMD-061): secure entry and question forms are the `slot` scope (P3.7 draws the slot); the helper view is `main` (S5); a sheet is `overlay`; the task row is `tray`; the @-popup is `popup`; the typing box is `composer`; the chat is `timeline`; the sidebar is `panel`; then `global`. The terminal's inline editor, onboarding and init levels are sheets or slot forms here. PAL binds in `global`: Ctrl+P palette; Ctrl+L and Ctrl+M `openDialog("models")`; Ctrl+S `openDialog("sessions")`; Ctrl+X `openDialog("background")`; Ctrl+F `openDialog("filepicker")` while entry 16's condition holds; Ctrl+, `openDialog("settings")` (D3); Ctrl+Z minimizes the window when focus is not in an editable field (CMD-064, PLAN section 7; the terminal suspends). Ctrl+K opens the Sessions sheet with the cursor in its filter: the phases 3-4 design binds Ctrl+K to the chat-list search and says its results are phase 5's (3-4 design §1.8; PLAN.md:103), so S5 swaps that binding to `openDialog("sessions")` (7.1; 9.4, D4). Phase 3 keeps Ctrl+G help, Ctrl+T to-dos, Ctrl+N new chat and Ctrl+Y YOLO; TERM binds Ctrl+O in the typing box (2.26), LIVE binds Ctrl+B (2.15). Click focus (CMD-062): clicking the chat, sidebar or a list focuses it unless `composer_focus_only` is on (phase 3's rule, 3-4 design §1.8). CMD-063: S3's `ui.setFocus(target)` updates both the DOM focus and the focus look.

**Arguments sheet (DLG-258 to DLG-263).** Title "Custom Command Arguments" for custom commands (`internal/ui/model/dialog_actions.go:282`), the prompt's title or "MCP Prompt Arguments" for MCP prompts (`:308`), otherwise "Arguments". One field per argument, labelled in Title Case with "*" when required, prompt "> ". ↓/Tab next field, ↑/Shift+Tab previous; Enter moves to the next field and submits on the last (`internal/ui/dialog/arguments.go`). A missing required value shows the warning "Required argument '<Title>' is missing." and focuses that field (`arguments.go:207`). Custom command arguments are `$NAME` placeholders matching `\$([A-Z][A-Z0-9_]*)`, replaced in the content before sending. The desktop replaces the longest names first, so `$FOOBAR` is never cut by `$FOO`; the terminal replaces in map order and can (coordinator decision on pass 1's Q-P4, `desktop/PLAN.md:375`; 9.4, D20; `u:pal.args_longest_first`). While an MCP prompt is generated the footer shows "Generating Prompt..." and Esc cancels (DLG-263). Long forms scroll; fields accept paste (DLG-262).

**Concurrent terminal changes.** The palette rebuilds its entries on `config_changed`, `mcp_event` and `session` events while open, keeping the filter and the selected entry by id (`commands.go:398-417`), so a toggle the terminal flips changes the label under the cursor without running anything.

### 2.3 Model picker, effort, toggles, Usage, providers refresh (MOD; DLG-021 to DLG-040, DLG-281 to DLG-287; CMD-016, CMD-017, CMD-024 to CMD-027; MISC-011, MISC-019)

**Opening (DLG-021).** Ctrl+L, Ctrl+M, the palette entry, the typing box's model button (P3.7 calls `openDialog("models")`, 3-4 design §1.10), and onboarding (P3.9 calls `openDialog("models", {onboarding: true})`, START-001). Sheet id `models`.

**Sheet.** Title "Switch Model" (`internal/ui/dialog/models/models.go:278`). Title-side selector "Large Task" / "Small Task" (`models.go:33-40`), switched with Tab/Shift+Tab, hidden during onboarding (DLG-022). Filter placeholders "Choose a model for large, complex tasks", "Choose a model for small, simple tasks", onboarding "Find your fave" (`models.go:70-72`). Onboarding adds the line "To start, let's choose a provider and model." (`models.go:282`). Help footer includes "ctrl+e edit" only when the selected model's provider is configured (`models.go:131-132`, `:324-347`).

**Rows (DLG-023 to DLG-029).** The desktop builds the same groups as the terminal from `GET …/providers` → `[catwalk.Provider]` (fields `name, id, api_key, api_endpoint, type, default_large_model_id, default_small_model_id, models, default_headers`) and `GET …/config` (`models`, `recent_models`, `providers`) (`internal/rpc/routes.go:41-42`). Group building follows `models.go:433-560`:

1. "Recently used" first, from `recent_models.<large|small>`, provider name shown on each row; entries whose provider or model no longer exists are dropped and the pruned list is saved back with `POST …/config/set` `{scope: 0, key: "recent_models.<type>", value: [...]}` (`models.go:554-560`, DLG-025).
2. Custom providers (configured, not in the catalog), sorted by name then id.
3. Catalog providers in catalog order; a configured provider uses its configured name, and its configured models override catalog names; configured models missing from the catalog are appended (`models.go:509-530`, DLG-026).
4. Providers with `disable: true` are skipped (`models.go:483-486`, DLG-027).
5. With a ChatGPT or Grok sign-in, the provider lists the account's models (`chatgpt_models`, `grok_models` in its config) instead of the API catalog (`models.go:495`, `:541`, DLG-028).
6. If the provider list failed to refresh, known providers still show; if there are none, the sheet does not open and the error is shown (`models.go:148`, DLG-029).

Group headings show the provider name and, when it fits beside the name, the badge "Configured" (`internal/ui/dialog/models/item.go:45-56`). The filter matches the group name plus the model name, ignoring spaces and case, and keeps the order inside each group (`internal/ui/dialog/models/list.go:175`).

**Moving (DLG-030).** ↑/Ctrl+P and ↓/Ctrl+N wrap and skip headings. Mouse: hover highlights, click selects, double-click chooses (2.0 rule 6, DLG-033).

**Choosing (DLG-031, DLG-034).** Enter/Ctrl+Y. The desktop follows `internal/ui/model/model_select.go:120-231` step by step:

1. Busy and no sign-in sheet open: warning "Agent is busy, please wait..." (`:124-126`).
2. Hyper with an expired sign-in token: `POST …/config/refresh-oauth` `{scope: 0, provider_id: "hyper"}` with a 15 s limit; on failure, open the Hyper sign-in sheet (`:23-38`, `:144-148`).
3. Copilot not configured: `POST …/config/import-copilot` → `{token, success}` once, then continue (`:150-155`).
4. OpenAI and xAI: no sign-in and no API key → sign-in method sheet; an existing sign-in → sign-in sheet; an existing API key → API key sheet (`:157-182`, `:299-353`).
5. Provider not configured → API key sheet (or Hyper/Copilot sign-in) (`:184-190`).
6. After a ChatGPT/Grok sign-in, if the account does not offer the chosen model: error "<model> isn't offered by your <provider> account; choose another model" and the picker reopens (`:197-210`).
7. Save: `POST …/config/model` `{scope: 0, model_type: "large"|"small", model: {model, provider, reasoning_effort, max_tokens, …}}` (`internal/proto/requests.go:25-29`); when no small model is set and a large one was chosen, `GET …/agent/default-small-model` then `POST …/config/model` for `small` (`internal/ui/model/config_write.go:53-69`).
8. Then `POST …/agent/update` and the info notice "<Large|Small> model changed to <model name>" (`model_select.go:254-266`). During onboarding: `POST …/agent/init` `{interactive: true}` first (`model_select.go:276-296`).
9. Theme follows provider: the terminal switches to a provider's theme (`internal/ui/model/theme.go:409`); every provider maps to the default theme except Hyper, whose "hyper" theme is an alias of charmtone-panther (THEME facts in 3.2). In the desktop the active theme does not change on a model change unless the user's theme came from the provider rule; 3.4 pins this.

**Edit sign-in (DLG-032).** Ctrl+E on a configured provider's model opens its API key or sign-in sheet with `reAuthenticate: true` (`models.go:205`).

**Effort sheet (DLG-035 to DLG-040).** Title "Select Reasoning Effort". Rows are the model's `reasoning_levels` in Title Case ("xhigh" shows "X-High"), with "current" on the level in force, falling back to `default_reasoning_effort` (`internal/ui/dialog/reasoning.go:270`, `:354`). Filter as 2.1; no selection means Enter does nothing (`reasoning.go:150`). ↑/Ctrl+P, ↓/Ctrl+N wrap; Enter/Ctrl+Y confirms. When the model supports Ultracode, a switch line "Ultracode on|off · workflows on every task" toggles with Tab and applies together with the effort (`reasoning.go:129`, `:233`). Confirm writes `POST …/config/model` with `reasoning_effort` (and `ultracode`), then `POST …/agent/update`; notice "Reasoning effort set to <raw level>[ · Ultracode on|off]" (`internal/ui/model/model_select.go:440-475`). A failed save keeps the sheet open with the error. Esc cancels both changes. Opening errors: "agent configuration not found", "model configuration not found", "no reasoning levels available" (DLG-040).

**FAST, Ultracode, Thinking (CMD-024 to CMD-026).** Busy: "Agent is busy, please wait..." (`internal/ui/model/fast_mode.go:13-15`, `:49-51`). Unsupported: "selected model does not support FAST mode", "selected model does not support Ultracode", "selected model does not support a thinking toggle" (`fast_mode.go:27`, `:63`; `model_select.go:418`). FAST flips `service_tier` between "fast" and "default"; Ultracode flips `ultracode`; Thinking flips `think`; each writes `POST …/config/model` for the coder's model type, then `POST …/agent/update`; notices "FAST mode enabled|disabled", "Ultracode on|off", "Thinking mode enabled|disabled" (`fast_mode.go:29-45`, `:65-76`; `model_select.go:420-433`). Support rules: FAST for Claude Code, Codex CLI and Abacus models; Ultracode for Claude Code models with reasoning levels; the thinking toggle for models that can reason without levels (`config.SupportsFastMode`, `SupportsUltracode`, `SupportsThinkingToggle`; the desktop reads the result from the three fields G-8 adds to `GET …/agent/capabilities`, 9.1, rather than porting the rules).

**Usage sheet (DLG-281 to DLG-287; CMD-017).** Title "Usage". For CLI-backed models it shows `GET /v1/limits` → `LimitsSnapshot{clis: [CLILimits{kind, state, read_at, error, windows: [LimitWindow{name, used, resets_at, model}]}]}` for the current model's CLI kind (engine design 5.9). Lines, in the terminal's order (`internal/ui/dialog/usage.go:80-140`): provider name or "Current model"; for non-CLI models "Subscription usage is available for CLI-backed models."; no windows "No usage windows reported by this CLI."; per window "<used>% used · <left>% left", "Model: <m>", "Resets: Mon, Jan 2, 2006 · 3:04:05 PM MST (UTC-07:00)", "In: [Nd ]HHh MMm SSs" or "Reset time not reported"; after a passed reset "Reset time passed; percentages above are last reported."; then "Context" or "Context (estimated)", "<n> / <m> tokens before compaction", "<k> tokens remaining" or "<n> tokens used", or "Compaction limit not reported"; footer "Refreshing usage…", "Refresh failed: <err>" (warning color) or "Checked: 3:04:05 PM MST" (`usage.go:93-134`). Context numbers come from the session's `context_budget` and `estimated_usage` (engine design 5.1 additions to `proto.Session`). R refreshes: `POST /v1/limits/{kind}/refresh` (202); the sheet updates on `limits` events, newest `read_at` wins (engine design 5.9). The countdown ticks every second. ↑/↓, PgUp/PgDn and the wheel scroll (DLG-287 fix, PLAN section 12); Esc closes.

**Refresh provider list (MISC-019).** Palette and Settings button. Calls G-9 `POST /v1/providers/refresh` (9.1), which runs the same work as `crush update-providers` (writes `providers.json`/`hyper.json`) and reloads; notice "<source> provider updated successfully." per source, errors as received.

**MISC-011.** The picker lists the same models as `crush models --json` (`provider, provider_name, model, name, reasoning_levels, default_reasoning_effort, fast`); the check compares the two lists on the same throwaway data.

**State.** Per window: open/closed, task type, filter, selection; nothing persisted beyond config writes.

**Concurrent terminal changes.** On `config_changed` the open picker re-reads `GET …/config` and `GET …/providers`, rebuilds groups and keeps the selection by `provider/model` key. A model the terminal chose meanwhile changes "current" marks and the typing box's model button (phase 3) without closing the sheet. Effort and Usage do the same.

### 2.4 Sign-in sheets (AUTH; DLG-041 to DLG-075; MISC-012, MISC-013)

The terminal runs every sign-in flow in its own process and saves the result through the server (`internal/ui/dialog/auth/oauth.go:613-633`; `crush login` saves with `SetProviderAPIKey`, `internal/cmd/login.go:101-207`). The desktop must not run OAuth flows in the webview (strict CSP, no network) and must not reimplement four OAuth protocols in Rust. It drives them through engine routes G-5 (9.1), which reuse `internal/login`'s flows (`internal/login/login.go:49-66`, `:125-138`: `Start(ctx) (url, userCode, err)`, `Wait(ctx) (*oauth.Token, err)`, `CompleteWithCode(ctx, input)`, `Close()`). Everything the user sees is the terminal's.

**Method sheet (DLG-041 to DLG-045).** Title "Let’s Auth <Provider>", prompt "How would you like to authenticate?", two cards "ChatGPT Account\nwith Subscription" (Grok: "Grok Account\nwith Subscription") and "API Key", subscription selected first (`internal/ui/dialog/auth/auth_method.go:175-197`). Every arrow key, Tab/Shift+Tab, H and L switch cards; Enter/Ctrl+Y confirms; Esc goes back (`auth_method.go:84`, `:117`). Click selects; a second click within 400 ms confirms; a click outside clears the double-click memory (`auth_method.go:36`, `:134`). Cards sit side by side when each can be at least 20 columns' worth wide (220 picture px), else stacked (`auth_method.go:185`).

**API key sheet (DLG-046 to DLG-053).** Title by state: "Enter your <P> Key.", "Verifying your <P> Key...", "<P> Key validated.", "Invalid <P> Key. Try again?" (`internal/ui/dialog/auth/api_key_input.go:211-229`). Field placeholder "Enter your API key...", prompt "> ", plain text (not masked, as in the terminal) (`api_key_input.go:78`, `:236`). Notice under it: "This will be written in your global configuration:" and the global data config path (`api_key_input.go:178`), read from G-6's `config_path` (9.1). Enter/Ctrl+Y verifies through G-5 `POST …/auth/verify-key` `{provider_id, api_key}` → `{ok, error}`; the spinner shows at least 750 ms (`api_key_input.go:303-305`). While verifying, all keys including Esc are ignored and the field is blurred (DLG-048). Invalid: error look, field refocused. Validated: Enter/Ctrl+Y **or Esc** saves (kept on purpose, PLAN section 12) with `POST …/config/provider-key` `{scope: 0, provider_id, kind: "string", api_key: "<key>"}` (`internal/proto/requests.go:45-58`), once; a failed save shows "failed to save API key: <err>" and can be retried (`api_key_input.go:339`). Then the original model choice resumes (step 7 of 2.3). Esc before validation cancels; during onboarding it returns to the picker (DLG-052). The key value never goes to the probe file, notices or logs (SET-035).

**Sign-in sheet (DLG-054 to DLG-070).** Title "Let’s authenticate with <Hyper|GitHub Copilot|ChatGPT|Grok>" (`oauth.go:378`). States and texts (`oauth.go:402-505`):

| State | Body |
|---|---|
| initializing | spinner + "Initializing..." |
| display, device flow (Hyper, Copilot, Grok fallback) | "Press enter to copy the code below and open the browser.", the code in a large box, "Browser not opening? Pay a visit to:" + link, spinner + "Verifying..." |
| display, browser flow (ChatGPT, Grok) | "Press enter to open the browser and authenticate.", link line, spinner + "Waiting for browser authentication..." |
| display, Grok browser flow, code entry | adds "Declined or browser didn't connect? Enter the code from the page:" and a field "Paste the code from the browser..." |
| saving | spinner + " Fetching models..." |
| success | "Authentication successful!" |
| error | "Authentication failed." + the error text |

Keys: C copies the code ("Code copied to clipboard"), U copies the URL ("URL copied to clipboard"), Enter/Ctrl+Y opens the browser through the shell-open service (device flows copy the code first: "Code copied and URL opened") (`oauth.go:573-611`). Tab focuses the Grok code field; Enter there submits through G-5 `POST …/auth/flows/{flow_id}/code` `{code}`; an empty Enter opens the browser; Esc only leaves the field (DLG-060, DLG-061). Enter/Esc are ignored while saving; on success Enter/Ctrl+Y/Esc continues the model choice (DLG-062, DLG-063). Esc in the display or error state cancels the flow: `POST …/auth/flows/{flow_id}/cancel` (DLG-065). The link is a real link (DLG-070) that opens in the system browser on click.

Flow routes (G-5, this design's contract, 9.1): `POST …/auth/flows` `{platform: "hyper"|"copilot"|"openai"|"grok"}` → `{flow_id, url, user_code, code_entry}`; events `auth_flow` `{flow_id, state: "waiting"|"saving"|"done"|"failed", error}`. The engine saves the credential itself (as `crush login` does) and fetches the account's model list before `done`. Hyper keeps its minimum visible wait of 750 ms (`internal/ui/dialog/auth/oauth_hyper.go:36`). Unix socket only; never logged.

**AWS SSO sheet (DLG-071 to DLG-074).** Opens with key grace when an `agent_event` with `type: "aws_sso_auth"` arrives for this window's chat, carrying `aws_sso_command` and `aws_sso_url` (`internal/proto/agent.go` AgentEvent; `internal/ui/model/agent_events.go:171-252`). Title "AWS SSO Authentication"; "Starting <command>..." until a URL arrives, then "Press enter to open the authorization page." and spinner + "Waiting for authentication..."; the URL updates when a newer event brings one (`internal/ui/dialog/auth/aws_sso.go:169-253`). Enter/Ctrl+Y opens the URL. Esc closes the sheet without cancelling the refresh. `aws_sso_auth_result` with no error closes it; with an error shows "Authentication failed." and the error, dismissible.

**Re-sign-in (DLG-075).** An `agent_event` with `type: "re_authenticate"` opens the provider's API key or sign-in sheet with `reAuthenticate: true` (`agent_events.go:195-209`). The event carries no provider id over the server today (engine gap G-10, 9.1); until then the desktop opens the sheet for the coder model's provider.

**Settings rows (MISC-012, MISC-013).** Settings → Accounts lists Hyper, GitHub Copilot, ChatGPT, Grok with "Sign in" (opens the sign-in sheet without a model choice; on done shows the info notice "Signed in to <name>" (desktop text, 9.4 D5)) and, when signed in, "Sign out". Sign out asks "Are you sure you want to log out of <Charm Hyper|GitHub Copilot|ChatGPT|Grok>?" with No as the default (`internal/cmd/logout.go:102`), then removes the same keys `crush logout` removes with `POST …/config/remove` `{scope: 0, key}`: hyper and copilot `providers.<id>.api_key`, `providers.<id>.oauth`; openai `providers.openai.oauth`, `providers.openai.chatgpt_models`, `providers.openai.api_key`; xai `providers.xai.oauth`, `providers.xai.grok_models`, `providers.xai.api_key` (`logout.go:131-183`), and shows "Successfully logged out of <name>." (`logout.go:137`).

**Concurrent terminal changes.** A credential saved by the terminal arrives as `config_changed`; an open API key sheet for that provider stays as it is (the user is typing), and the model picker re-reads providers. Two apps running a sign-in for the same platform both finish; the later save wins, as with two terminals.

### 2.5 Sessions and projects (SES; DLG-076 to DLG-088, DLG-113 to DLG-118; CMD-015, CMD-038, CMD-039; MISC-014, MISC-015)

**Sessions sheet (DLG-076 to DLG-087).** Opened by Ctrl+S, the palette, Ctrl+K (S5's swap, 7.1; 9.4 D4), START-021's errors with the given ID in the filter (PLAN.md:134, "with the Sessions search open"; S5's swap, 7.1), and the left list's search box: a typed character, Enter or a click in it opens the sheet with the box's text in the filter and empties the box (S5's change to `ChatList.svelte`, 7.1). Focus goes to the filter. Title "Sessions", filter placeholder "Enter session name" (`internal/ui/dialog/sessions/sessions.go:102`, `:309`). Rows from `GET …/saved-sessions?selected=<shown chat id>` (`internal/client/sessions.go:62-65`) → `[session.Session]` with PascalCase keys (`ID`, `Title`, `Directory`, `DataDirectory`, `MessageCount`, `CreatedAt`, `UpdatedAt`; `internal/session/session.go:51-72`; `internal/rpc/routes.go:55`): top-level chats of this workspace with at least one user message, newest first. Each row: title, then "~/short/folder · " before the relative updated time in the info column (35% budget, `kit/infocolumn.go:13`) (`sessions/item.go:82`, `:104`). The current chat is selected when the sheet opens.

Keys: ↑/Ctrl+P, ↓/Ctrl+N wrap; Enter, Tab and Ctrl+Y open; Ctrl+R renames inline (the row becomes a field prefilled with the title, the line "Rename this session?" above), Enter saves the trimmed title (empty is ignored), Esc cancels (`sessions.go:171`, `:193`, `:322`, `:443-458`); Ctrl+X asks "Delete this session?" (`sessions.go:316`), Y or Enter deletes and removes the row at once, N or Esc cancels; a busy chat cannot be deleted: "Agent is busy, please wait..." (`sessions.go:196-198`). Both changes go through the saved-chat route the terminal uses, `POST …/saved-sessions` (`internal/rpc/routes.go:56`), with the PascalCase body `session.CatalogChange{"DiscardEmpty": false, "Session": <the row as read, Title changed for a rename>, "Remove": <true for delete>}` (`internal/session/catalog.go:18-22`; `sessions.go:420-430`, `:469-477`). The engine checks the row's `Directory` and `DataDirectory` against the registered projects and refuses to delete a chat that is still streaming with "this chat is still running" (`internal/sessioncatalog/catalog.go:122-155`); errors show as error notices and the row comes back on the next list read. Mouse: click selects, double-click within 400 ms opens; ignored outside the list and during rename or delete (`sessions.go:247`). The wheel scrolls without snapping back to the selection (`sessions.go:236`, `:299`). Esc closes (in rename/delete it returns to the list first).

Opening a row: if the chat is in this project, the window shows it with `openChat(id)` (P3.3); a chat this app closed (`isClosed(id)`, P4.1) is reopened first with `reopen(id)` (PLAN.md:103: a closed chat without a link comes back from the Sessions search). Side effect kept from the terminal: listing saved sessions deletes this workspace's empty drafts older than 5 minutes except the selected one (`internal/sessioncatalog/catalog.go:89`, `:111-118`); G-11 (9.1) asks the engine to skip drafts another app is showing.

**Another folder (DLG-088).** A row whose `Directory` belongs to another project: busy → warning "Agent is busy, please wait before switching folders..."; folder missing → warning "That chat's folder is no longer available: <dir>" (`internal/ui/model/session_load.go:82-100`). Otherwise the desktop opens (or focuses) that project's window and shows the chat there, through `window_open` with `projectDir` and `sessionId` (6.4) (PLAN section 7: windows are per project), instead of relaunching the process. A window that `window_open` focuses gets `window-request` with that `sessionId`, and SES's plugin calls `openChat` for it. The opening screen is LIST-034's (2.21).

**Open Project sheet (DLG-113 to DLG-118).** Title "Open Project" (`internal/ui/dialog/projects.go:192`). Rows: saved projects from `projects.json` (format below), short path, "current" on this window's project, filter by path. No saved projects: the sheet does not open and the info notice "no saved projects yet, use Save Project first" shows (`projects.go:72`). ↑/Ctrl+P, ↓/Ctrl+N wrap; Enter/Ctrl+Y opens: the current project closes the sheet with "Already in this project" (`internal/ui/model/dialog_actions.go:411`); busy → "Agent is busy, please wait before switching projects..." (`:415`); otherwise this window switches to the chosen project (DLG-117, PLAN section 7: "reopen the window on the new folder") with `projectSwitch(projectDir)` and a page reload (6.2, 6.4): the window retires its client, ensures the new project's engine, and shows its newest chat, carrying the YOLO and debug flags. A path that no longer exists shows the error from ensuring the engine. Ctrl+X removes the entry (not the folder) at once with "Removed saved project <~/short>" (`projects.go:136-150`).

**Save Project (CMD-038).** Marks this window's project saved in `projects.json`, info "Saved project <~/short>" (`dialog_actions.go:406`).

**projects.json.** SES's Rust module `projects.rs` reads and writes it directly through the commands `projects_list`, `project_save`, `project_remove` and `recent_folders` (6.4), like the terminal (`internal/projects`; no engine route): path beside the global data config (`<global data dir>/projects.json`), JSON `{"projects":[{"path", "data_dir", "last_accessed", "saved"}]}` (the `crush projects --json` shape, MISC-015). Writes keep unknown fields, write to a temp file in the same folder with mode 0600 and rename. The terminal writes it in place, mode 0600, without a temp file (`internal/projects/projects.go:75`); 9.2 lists the race.

**MISC-014, MISC-015.** The left list and the Sessions sheet show the same chats as `crush session list --json`; the Open Project sheet the same saved projects as `crush projects --json`. Checks compare both on the same data.

**Concurrent terminal changes.** A chat renamed or deleted in the terminal arrives as a `session` event (`updated`/`deleted`); the open sheet updates the row in place or removes it, keeping the selection on the same id (or the next row). `projects.json` has no event: the Open Project sheet re-reads the file each time it opens.

### 2.6 Question forms (ASK; DLG-141 to DLG-188)

**Arrival (DLG-141).** A `question_batch_request` event (`created`) carries `proto.QuestionRequest{id, session_id, tool_call_id, questions[QuestionItem{id, type, label, question, description, choices[QuestionChoice{id, label, description, image}]}], confirm_title, confirm_description}` (`internal/proto/proto.go:224-251`). The engine sends it only to windows that show that chat (2.0 rule 10). P3.3's `prompts` store keeps it in `prompts.question` (3-4 design §1.5.3); while that request names the shown chat, ASK's plugin puts the form in the Q8 slot: `ui.openSlot({kind: "question", component: QuestionForm, props: {request}})` (3-4 design §1.10). A newer request replaces an older form. While a permission prompt or a secure form holds the slot, `openSlot` refuses, and the form opens when the slot frees (S5's slot rule, 7.1). The form also opens when the user switches to a chat whose record has `pending_prompt: "question"` (engine design 5.1 e), by reading the open request through G-12 `GET …/questions/pending` (9.1): today no route returns a pending request, and missed events are not replayed. On arrival the window plays the sound "message-new-instant" and sends the notification "Crush is waiting..." / "<N> questions need your input" under the rules of 2.18 (`internal/ui/model/update_workspace.go:86-97`). An open CLI update offer gives up the slot and becomes an update notice (UPD-008, 2.19). A `question_batch_notification` whose `batch_id` equals the form's request id closes the form: another app answered (`internal/proto/proto.go:277-280`).

**Layout and limits (DLG-142 to DLG-151).** Types `yes_no`, `single_choice`, `multi_choice`, `free_text`, `secure_entry` (2.7); an unknown type is shown as free text (`internal/ui/dialog/questionform/form.go:109`). The engine already enforces the limits (at most 11 questions, question ≤240, description ≤600, choice label and description ≤200, one optional PNG/JPG per choice; `internal/question/question.go:194`, `:219-224`); the desktop does not re-check them. Each question shows its focus badge, the wrapped question, and its description as sanitized Markdown (P3.4's `MarkdownView` with `{text, messageId: "", sessionId, finished: true, plan: "none"}`, 3-4 design §1.7) (DLG-144). A batch shows a tab row: each tab's label is the item's `label` or its first three words, plus a final "Confirm" tab (`form.go:130`, `:205`); a single question has no tabs and no Confirm page. Narrow windows show only the active label with "N/M" (`questionform/tabs.go:78`). The form reserves the height of its tallest page so switching tabs never moves the chat (`form_draw.go:15-35`). When focus moves to the chat and the form is taller than 2/5 of the main column, it folds into one line: the active question text and "(<answered>/<total> answered)" for a batch, the question text alone for one question, followed by a rule; help "answer questions" (`form_draw.go:37-110`).

**Keys (DLG-145 to DLG-181).** As the checklist rows and their evidence: `[`/`]` and Ctrl+←/→ switch tabs and wrap, keeping the live answer (secure fields type brackets) (DLG-146); Tab moves focus between the form and the chat, not between questions, and does nothing with `composer_focus_only` on; clicking the typing box's area brings the form back (DLG-147). Enter answers and moves on; on the last question it goes to Confirm; a single question submits (DLG-152). Esc and Alt+Esc cancel the whole batch, except inside a note, where Esc closes only the note (PLAN section 12 fix; DLG-153). Yes/no starts on No; ←/→/H/L move, Enter answers, Y/N answer and advance (DLG-155, DLG-156). Choices: ↑/K and ↓/J wrap through the choices and the "Something else?" field (`questionform/editor.go:70`, `:171`); 1-9 move only (DLG-159, DLG-160). Single choice: Enter confirms the cursor (or the hovered row); a click selects without advancing (DLG-161, DLG-162). Multi choice: preselected from the request's `selected` list (needs G-13: `proto.QuestionItem` lacks `selected`, `internal/question/question.go:61-63`); Space or a click toggles, 1-9 move and toggle, Enter finishes even with none, ids returned in the original order (DLG-163 to DLG-165). The custom answer holds at most 500 characters, 1-4 rows; Enter needs trimmed non-empty text; real arrow keys leave it, typed J/K stay text (DLG-166, DLG-167). Alt+N opens a note (300 characters) on the cursor's choice or the question, prefilled; Enter saves, empty removes (DLG-168, DLG-169). Free text: "Type your answer..." (`questionform/freetext.go:49`), at most 1000 characters, 1-6 rows; Enter submits trimmed non-blank text, Shift+Enter and Ctrl+J insert a newline (DLG-173, DLG-174). Confirm page: title "Ready to go?" or the request's `confirm_title`, the description, a bullet per answer (labels resolved, secure answers as status only, notes not shown), "WARN <n> question(s) unanswered" when some are missing, and the buttons "Yup!" (default) and "Not yet" (`questionform/confirm.go:59-64`, `:207-236`); ←/→ move, Y/N move only, Enter acts; Esc or "Not yet" goes back to the first unanswered question, else the last (DLG-176 to DLG-181).

**Mouse (DLG-148, DLG-157, DLG-171, DLG-172, DLG-175, DLG-181, DLG-188).** Hover follows the pointer without losing edits; the arrows continue from the hovered row. Clicking a tab switches; clicking yes/no answers at once; clicking "Yup!" submits. The wheel scrolls long forms with no snap to the cursor and a thin scrollbar.

**Text, links, pictures (DLG-182 to DLG-187).** Form text is selectable by dragging and copied with Ctrl+C (2.0 rule 8); dragging never picks a choice. Links in descriptions (Markdown links, http(s), absolute or `~` paths with `:line:col`) open on a click without drag, beating the choice click; web links go to the browser and file links to the editor through P3.4's `guardLinks` and `openLink(href)` (3-4 design §1.7; PLAN section 7 "Links"). A choice's `image` (an absolute path on this computer) shows in a picture area of 16 rows' worth (416 picture px) that follows the cursor or the hovered row, keeping its aspect; texts "Loading the picture…", "This picture can't be opened.", "No picture for this choice." (`questionform/preview.go:260-279`). The picture is an `<img>` whose source is a `data:` URL built from P3.2's `file_read(path, cwd, maxBytes)` → `{mime, base64}` (3-4 design §1.2.8; the content rule allows `data:` pictures, `desktop/src-tauri/tauri.conf.json:26`), clipped by the form's scroll area (DLG-187, MISC-007). `maxBytes` is 20 MB (inferred bound; the terminal reads the whole file, `questionform/preview.go:188`); a larger or unreadable file shows "This picture can't be opened.". Clicking the picture opens it in the image viewer, `ui.openViewer({path})` (S3, P3.8; PLAN section 7).

**Answering.** Submit: `POST …/questions/answer` with `proto.QuestionAnswer{batch_request_id: <request id>, responses: [{request_id: <item id>, selected_ids, fill_in_text, yes, notes}]}` → `{resolved}` (`internal/rpc/routes.go:89`, `internal/proto/proto.go:255-273`). `notes` maps a choice id (or the question id) to its note. `resolved: false` means another app answered first: the form closes silently. Cancel: `POST …/questions/cancel` → `{resolved}` (`routes.go:90`); it cancels the workspace's pending batch (`internal/server/handlers_agent.go:165-169`).

**State.** Per window: the request, per-question live answers, notes, active tab, focus side (form or chat), scroll. Released on submit, cancel, close notification, or chat switch (the form comes back from G-12 when the user returns).

**Concurrent terminal changes.** The terminal app showing the same chat gets the same request. Whichever app answers first wins; the other gets `question_batch_notification` and closes its form.

### 2.7 Secure entry (ASK; DLG-189 to DLG-201)

**Arrival (DLG-189, DLG-190).** `crush secure-entry` asks (agents run them; `--label` default "API key", `--placeholder` default `%s`, `--occurrence` from 1; `internal/cmd/secure_entry.go:14`, `:55-58`) reach the window as a `secure_form` event (`created`) sent only to windows that view the chat, carrying `proto.SecureForm{form_id, session_id, request, fields[SecureField{question_id, label, status}]}` (`internal/proto/share.go:80-98`; engine design 5.8). `GET …/secure-forms` lists the open forms for this window and is read on reconnect and on chat switch (`internal/rpc/routes_share.go:18`). The form takes the Q8 slot with the kind `secure`, `ui.openSlot({kind: "secure", component: SecureForm, props: {form}})` (3-4 design §1.10), with the secure questions drawn as secure fields. ASK keeps one form per `form_id`: the engine sends `secure_form` again each time a client's view of the chat changes (`internal/backend/secure.go:125-160`), and a repeat for a `form_id` ASK already holds changes nothing (`u:ask.secure_repeat`). A form for a chat this window has not loaded is kept by `form_id` and shown with that chat (6.1, event order). A `secure_form_closed` event `{form_id, reason}` closes it; reasons `submitted`, `cancelled`, `answered_elsewhere`, `closed` (`internal/proto/share.go:100-105`). The engine never forwards secure forms to the phone (engine design, Events). The texts "Unavailable" and "Secure entry works only in the local Crush terminal." (`questionform/secure.go:305-307`) appear only for a field the engine marks unusable; the desktop is a local app, so they normally never show.

**Field (DLG-191 to DLG-196, DLG-199).** A masked one-line field: each character shows as a bullet; the value never enters answers, the summary, a store, the probe file, notices or logs. Keys: ←/→, Home/Ctrl+A, End/Ctrl+E, Backspace/Ctrl+H, Delete; `[` and `]` are literal characters; Ctrl+U wipes the value and its error (DLG-192, DLG-194). Ctrl+V, Shift+Insert and middle-click paste into the focused secure field only (DLG-193); the desktop reads the paste event's data synchronously, so the terminal's 500 ms clipboard timeout has no counterpart (9.4, D6). Status texts: "Enter secret…" (empty), "Entered · saved on submit", "Saved", "(not entered)" on the Confirm page; `unavailable` shows "This file was already saved. Request this entry again to add it." (`secure.go:261-269`, `:305-315`). No click places the caret (DLG-199).

**Validation (DLG-195).** Before submitting, the window applies the engine's own value rules and shows their texts on the field: "enter a value before saving", "value exceeds 64 KiB", "enter a single-line value without control characters" (`internal/secureentry/secureentry.go:153-168`); one trailing newline is stripped first. The engine checks again.

**Submit and cancel (DLG-197, DLG-198, DLG-201).** Values are written only on submit: `POST …/secure-forms/{form_id}/submit` with `proto.SecureSubmit{answers: [QuestionAnswer for the ordinary questions], values: {<question_id>: <value>}}` (`internal/proto/share.go:107-113`). This call MUST go through `apiSecret("secure-forms/{form_id}/submit", submit)` over the Tauri command `api_secret` (6.4), never the generic `api` command: the Rust side serializes the body, sends it on the unix socket and zeroes its copy; the webview clears the fields right after. A 422 `secure_field_error` with `field` keeps the form open on that field with the error text, and the window re-reads `GET …/secure-forms` for the new statuses (engine design 5.8, "Field status"). Esc cancels: `POST …/secure-forms/{form_id}/cancel`; nothing is written and the fields are cleared. The engine does the file checks (sha256 fingerprint, no symlinks, mode 0600, one form at a time; `internal/secureentry/secureentry.go:110-215`).

**Covered form (DLG-200).** While any sheet covers the window (for example the Quit sheet), pastes are dropped, not forwarded to the sheet. The question sound plays on arrival. "Answered elsewhere" closes the desktop form when the terminal app answered the same form (engine design 5.8), which differs from a lone terminal (9.4, D7).

**Concurrent terminal changes.** Both apps viewing the chat get the form; the first submit wins and the other gets `secure_form_closed` with `answered_elsewhere`.

### 2.8 Skills manager and kept skills (EXT; DLG-202 to DLG-222; CMD-045, CMD-046)

**Skills sheet (DLG-202 to DLG-215).** Title "Skills" with the title-side selector "Installed" / "Browse skills.sh", like the palette's categories; the terminal writes the tab into the title instead, "Skills · Installed" or "Skills · Browse skills.sh" (`internal/ui/dialog/skillbrowser/skills.go:277-279`; 9.4, D15). Tab/Shift+Tab and a click on a label switch tabs; each tab keeps its own filter; placeholders "Filter installed skills" and "Search skills.sh (Enter to search)" (`skills.go:68`, `:145-148`).

- Installed: `GET …/skills/installed` → `[{id, name, description, label, source, user_invocable, enabled}]` (`internal/rpc/routes.go:130`; `internal/skills/catalog.go:21-28`, `internal/skills/management.go:15-18`). Rows: name and "Enabled"/"Disabled", disabled rows dimmed; the selected skill's description on a detail row; labels `system:`, `user:`, `project:` (DLG-215). Filter by name and description. Enter toggles: `POST …/skills/manage` `{action: "toggle", name, enabled}` (`internal/proto/skill_management.go:14-19`), then status "Settings apply to subsequent turns in Crush." (`skills.go:121`) and the palette's user entries reload (G-7). A reload failure after a successful change still refreshes the list (DLG-214).
- Browse: typing at least two characters and Enter searches skills.sh; fewer shows "Type at least two characters to search skills.sh." (`skills.go:169`). The search runs in the terminal's own process today (`https://skills.sh/api/search`, `internal/skills/directory.go:39-47`); the desktop calls G-14 `GET …/skills/search?q=<query>` (9.1), which returns the same `[{id, name, source, skillId, installs}]` (`internal/skills/directory.go:23-28`). Rows: name and "<N> installs" or "Installed"; detail "<source> · https://skills.sh/<id>" (`skills.go:232`, `:299`). Enter on a result installs: `POST …/skills/manage` `{action: "install", skill: {id, name, source, skillId, installs}}`; status "Installed in the shared skills folder." (`skills.go:123`); an installed skill shows "Already installed. Switch to Installed to enable or disable it." (`skills.go:185`). A changed query needs a new search before Enter installs.
- Status texts (the sheet's own line above its help footer): "Loading installed skills…", "Searching skills.sh…", "Saving skill setting…", "Installing skill…", "Error: <err>", "<N> results from skills.sh" (`skills.go:82`, `:110`, `:118`). Empty texts: "No installed skills match this filter.", "Search skills.sh above, then select a skill to install.", "No results. Try another search." (`skills.go:284-288`).
- Keys: ↑/Ctrl+P, ↓/Ctrl+N without wrap; Ctrl+R refreshes the installed list (not while a request runs); Enter is ignored while a request runs; Esc closes and cancels the running request (DLG-209, DLG-212). Mouse added (2.0 rule 6).
- If `GET …/skills/installed` fails with `not_found`, the sheet shows "Skill management is unavailable in this workspace." (`internal/ui/model/dialogs.go:75`, DLG-213).

**Toggle skills sheet (DLG-216 to DLG-222).** Title "Toggle skills" (`internal/ui/dialog/kept_skills.go:251`), note "On skills are reminded every message and come back after compression." (`kept_skills.go:25`). Rows: every skill from `GET …/skills` (`proto.SkillInfo`, `routes.go:128`) sorted by name, in groups "On", "Toggleable", "Off" (`kept_skills.go:38`) from the chat's `kept_skills[{id, name, by_agent}]` (`proto.Session`): not kept = Off, kept with `by_agent: true` = Toggleable, kept with `by_agent: false` = On. Enter cycles Off → Toggleable → On → Off (`kept_skills.go:207-222`) and calls `POST …/sessions/{sid}/kept-skills` `{id, name, on, toggleable}` → the updated `proto.Session` (`internal/proto/session.go:63-70`). Selection follows the skill when it changes group. Filter; ↑/↓ clamp; at most 12 rows then "↓ <N> more" (`kept_skills.go:330`). Empty: "No skills found." or "No skills match." (`kept_skills.go:287-290`). Before the chat has a session, the window creates it first (`ensureSession()`, P3.3), as the terminal does when it saves the first kept skill (`internal/ui/model/kept_skills.go:64-96`). The open sheet follows `session` events for this chat, so changes the agent makes (`crush skill on|off`) show at once; those changes also add the system notice row in the chat (phase 3, DLG-222).

**Concurrent terminal changes.** Skill toggles in the terminal arrive as `skills_event` (installed list) and `session` events (kept skills); open sheets re-read and keep the selection by skill id.

### 2.9 MCP toggles, MCP sign-in, Docker MCP (EXT; DLG-232 to DLG-244; CMD-031, CMD-032)

**Toggle MCPs sheet (DLG-232 to DLG-238).** Shown only when the config has MCP servers. Title-side selector "Local" / "Global" (`internal/ui/dialog/mcpservers/toggles.go:63-66`), Local first; Tab switches. Rows (sorted by name): status dot, name, status text from `GET …/mcp/states` (`map[name]proto.MCPClientInfo{name, state, error, tool_count, …}`, `routes.go:140`): "starting", "connected", "error: <err>", "error", "needs authentication", "disabled", "offline" (`internal/ui/model/mcp.go:343-363`). Local scope reads `GET …/mcp/disabled` and `GET …/mcp/enabled` (repository overrides) and shows a local disable as overriding the live state (DLG-236); Global reads each server's `disabled` flag from `GET …/config` (`mcp.<name>.disabled`) (`mcp.go:280-313`). ↑/K, ↓/J clamp; Enter or Space toggles: Local `POST …/mcp/disabled` `{name, disabled}`, Global `POST …/mcp/config-disabled` `{name, disabled}` (`internal/proto/requests.go:139-144`); notices "MCP \"<name>\" enabled|disabled for this repository" and "MCP \"<name>\" enabled|disabled globally" (`mcp.go:320-340`). The row changes at once (a local enable shows "starting") and follows `mcp_event` `state_changed`. Marks follow the terminal's sidebar icons in meaning (`internal/ui/dialog/mcpservers/toggles.go:289-304`; `internal/ui/styles/quickstyle.go:583-589`), with the state colors of 3-4 design §4.11: connected an `--ink` dot, starting the working mark, error a cross in `--error-ink`, needs authentication an `--attention` dot, disabled a `--slate` dot, offline S3's offline icon in `--slate` (3.1 tokens; look 5.2; DLG-237). Empty: "No MCP servers configured." (`toggles.go:242`). Esc closes without reverting. The list scrolls when long (the terminal clamps without scrolling, DLG-234; scrolling is the desktop's normal list behavior, 9.4 D8).

**MCP sign-in sheet (DLG-239 to DLG-244).** Opens by itself when an `mcp_event` reports `state: "needs auth"` for a server not disabled here: the window reads `GET …/mcp/pending-auth` → `[{name, url}]` and drops servers in `GET …/mcp/disabled` unless they are in `GET …/mcp/enabled` (`internal/ui/model/mcp_auth.go:45-96`). Reopening brings it forward. Title "Authenticate with <name>" (`mcpservers/auth.go:252`); body "Press enter to open your browser." with " (<n>/<total>)" when several wait; Enter starts `mcpAuth(name, callId)`, `POST …/mcp/auth` `{name}`, which blocks until the flow ends and so carries a `callId` (6.1), while the window polls `GET …/mcp/auth-url` → `{auth_url}` every 200 ms and opens the URL in the browser once it appears (`openLink`, P3.4); spinner " Waiting for authorization..." (`auth.go:310`); Enter while waiting reopens the URL. C or U copies the auth URL (else the server URL) without starting (`clipboard_write`, P3.2). S skips to the next server (help shows it only with several). Success "Authentication successful!", Enter finishes; failure "Authentication failed." with the error, or "authentication timed out" when the flow was cancelled (`mcp_auth.go:19-43`). Esc closes and cancels the request (`api.abort(callId)`, 3-4 design §1.2.4). No retry button (DLG-244).

**Docker MCP (CMD-031).** The palette shows "Enable Docker MCP Catalog" when Docker MCP is not configured and `docker mcp version` succeeds; EXT's command `docker_mcp_version` (6.4, `docker.rs`) runs it with a 5 s limit and keeps the answer for 10 s (`internal/config/docker_mcp.go:28-62`), because the terminal checks it in its own process. Enable: `POST …/mcp/docker/enable`, notice "Docker MCP enabled and started successfully"; disable: `POST …/mcp/docker/disable`, notice "Docker MCP disabled successfully" (`internal/ui/model/commands.go:112-132`; `routes.go:146-147`).

**Concurrent terminal changes.** Toggles from the terminal arrive as `mcp_event` and, after G-4, `config_changed`; the open sheet re-reads the three lists.

### 2.10 Add Image picker (VIEW; DLG-245 to DLG-257; CMD-029)

**Opening.** Ctrl+F or "Open File Picker", only with a session and a model that takes images; Ctrl+F with another model does nothing, as in the terminal (`internal/ui/model/keypress.go:252-255`). Title "Add Image" (`internal/ui/dialog/filepicker.go:254`). It starts in the project folder, falling back to the app's working folder, then home (`filepicker.go:100`, `:125`).

**Listing.** VIEW's command `dir_list(path)` (6.4, `dirlist.rs`) returns `{path, entries: [{name, is_dir, is_symlink, readable}]}`. Folders first, then files, both by name; hidden entries are left out with no toggle (bubbles filepicker `:34`, `:197`). Files other than `.jpg`, `.jpeg`, `.png` are shown dimmed and cannot be chosen; the extension check ignores case, so `.PNG` and `.JPG` can be chosen (PLAN section 12 fix; DLG-256). Symlinks show "→"; unreadable entries are dimmed; an empty folder shows the terminal's empty text (DLG-254).

**Keys and mouse (DLG-248 to DLG-251, DLG-257).** ↑/K/Ctrl+P and ↓/J/Ctrl+N clamp; PgUp and PgDn page; `g` first, `G` last; →/L/Enter open a folder; ←/H/Backspace go up and restore the previous selection; Enter on an allowed file chooses it; Esc and Alt+Esc close. Click selects, double-click opens or chooses, wheel scrolls.

**Preview (DLG-252, DLG-253).** The highlighted image shows as an `<img>` thumbnail beside the list, its source a `data:` URL from P3.2's `file_read(path, cwd, 5 MB)`; a blank placeholder until it loads; hidden when the sheet is too short for it (less than 300 picture px of list height). Preview errors are not shown (DLG-254).

**Choosing (DLG-255).** VIEW checks the size first with P3.2's `file_info(path, cwd)`: over 5 MB → error "file too large, max 5MB"; then it reads the file with `file_read(path, cwd, 5 MB)` → `{mime, base64}`; a read failure → "unable to read the image: <err>" (`internal/ui/dialog/actions.go:221-245`). The MIME type comes from the file's first bytes (P3.2 uses the `infer` crate, then the extension, 3-4 design §1.2.8; the terminal sniffs the first 512 bytes). The picture becomes an attachment chip in the typing box through `addAttachment({kind: "picture", name, mime, path, base64})` (P3.7; 3-4 design §1.5.6 `Attachment`), and P3.7's send turns it into `proto.Attachment{file_path, file_name, mime_type, content}` (`internal/proto/message.go:651-656`).

**MISC-007.** Every picture this design shows (question choices, the picker's preview) is an `<img>` element, never a canvas or drawn blocks.

### 2.11 Changes sheet (kept by the phases 3-4 design: DLG-294 to DLG-311)

The phases 3-4 design maps DLG-294 to DLG-311 (its §2.8). P3.6's `plugins/tools.js` registers the stack entry `changes`; the tool group's "Changes" button opens it with `openDialog("changes", {sessionId, messageId, toolCallIds, title})`, and the sheet reads the existing route `GET sessions/{sid}/messages/{mid}/review` (3-4 design §2.8, §1.10). This design has none of its rows and none of its files, and pass 1's gap for a review route is withdrawn (9.1). Two things here touch it: the theme tokens of 3.1 color it (`--diff-*`, `--error-ink`, `--figure`), and it sits in S3's dialog stack with every sheet of this design (2.1).

### 2.12 Small setting sheets (SET; DLG-264 to DLG-280; CMD-018, CMD-034, CMD-051, CMD-052)

Each is also a Settings row (2.13); the palette opens the same sheet.

**Fallback compaction (DLG-264 to DLG-266).** Title "Fallback compaction" (`internal/ui/dialog/autocompact.go:86`), one field with prompt "Tokens: " (`autocompact.go:42`) prefilled with the saved value or 400,000 (`internal/config/autocompact.go:32-40`). Enter: commas are removed; not a whole number → inline error "Enter a whole number of tokens" (`autocompact.go:64`); below 1,000 → "Enter at least 1,000 tokens" (`internal/config/autocompact.go:44-46`); the error clears on typing. Busy → warning "Agent is busy, please wait before saving fallback compaction" (`internal/ui/model/autocompact.go:28-30`). Save: `POST …/config/set` `{scope: 0, key: "options.auto_compact_token_limit", value: <n>}`, then `POST …/agent/update`, info "Fallback compaction set to <n> tokens" (`autocompact.go:42-52`). Esc cancels.

**Memory limit (DLG-267 to DLG-271).** Linux only (the engine reports `supported`). Title "Memory Limit", hint "Most memory all Crush windows and everything they start may use. At the limit, the biggest program inside is stopped." (`internal/ui/dialog/memory_limit.go:25`, `:124`), field prompt "GB: " prefilled with the saved limit (`:50-52`), usage line "Empty = no limit" + " · now <used> GB" + " of <total RAM> GB" with one decimal (`:63-67`), values from `GET /v1/memory` → `MemoryStatus{…, limit_bytes, box_used_bytes, total_ram_bytes}` (engine design 5.9). Enter: a decimal with optional "GB"; empty or 0 means no limit; otherwise at least 2 GB; texts "Enter a number of GB, or leave it empty for no limit", "Enter at least 2 GB, or leave it empty for no limit" (`memory_limit.go:78-90`; `internal/config/memory_limit.go:30-35`). Save: `PUT /v1/memory/limit` `{value}` → `{limit_bytes, applied, notice}` (engine design 5.9); the window shows the returned `notice` as info: "Memory limit removed", "Memory limit set to <g> GB for every Crush window", or the error "saved, but couldn't apply the memory limit: <err>" (`internal/ui/model/memory_limit.go:37-46`). Esc cancels.

**Sub-agent instructions (DLG-272, DLG-273).** Title "Sub-agent Instructions", hint "Every chat's agent follows these when it starts sub-agents.", placeholder "Which models to use for which work, when to ask first…" (`internal/ui/dialog/subagent_instructions.go:19`, `:40`, `:73`). A multiline editor (2.1), 3-14 rows high then scrolling, no length limit. Enter is a newline; Ctrl+S saves; Esc cancels. The file is `subagent-instructions.md` beside the global `crush.json` (the folder of `$CRUSH_GLOBAL_CONFIG`, else `$XDG_CONFIG_HOME/crush`, else `~/.config/crush`); the text is trimmed and ends with one newline; written to `<path>.tmp` and renamed (`internal/config/subagent_instructions.go`). The engine reads the file on every agent call, so no reload is needed. Read and written by SET's commands `subagent_instructions_read` and `subagent_instructions_write(text)` (6.4, `configfiles.rs`), which use the same path rule; info "Sub-agent instructions saved" (`internal/ui/model/subagent_instructions.go:28`).

**Notification style (DLG-274 to DLG-280).** Title "Notification Style", filter "Type to filter" (`internal/ui/dialog/notifications.go:91`, `:194`). Rows "Auto", "Native", "OSC", "Bell", "Disabled" with "current" on the saved one (`notifications.go:33-37`, `:311`); the descriptions exist but are not drawn. "Native" is hidden where the terminal hides it (illumos, Solaris; `notifications.go:250-253`), which never applies to the desktop's Linux and macOS builds. ↑/↓ wrap, Enter saves: `POST …/config/set` `{scope: 0, key: "options.notifications", value: "<id>"}`, info "Notifications set to: <id>" (lower-case id, `internal/ui/model/config_write.go:106-120`). The desktop meaning of each style is in 2.18: auto, native and osc send a system notification; bell plays the system alert sound; disabled is silent (PLAN section 7; DLG-275 to DLG-279).

### 2.13 Settings page (SET; SET-003 to SET-037; CMD-028, CMD-048, CMD-049, CMD-055)

**What it is.** A sheet with id `settings`, opened by Ctrl+, and the palette entry "Settings" (9.4 D3). PLAN section 10 names "settings layout" as a phase 5 look; CHECKLIST SET-031 says the settings screen must cover the eight options crushrc cannot set. The page holds only rows the plan or the checklist asks for; everything else stays a JSON or crushrc option the engine honors, as in the terminal.

| Group | Row | Key (global scope `0`) | Control | Write | Terminal fact |
|---|---|---|---|---|---|
| Appearance | Theme | none: the desktop setting `theme`, not shared with the terminal's `options.tui.active_theme` (3.3) | opens Themes (2.14) | `settings_set("theme", name)` (3.3) | SET-009 |
| Appearance | Show sidebar | `options.tui.compact_mode` (inverted) | switch | `setCompact(enabled)` (P3.3): `POST …/config/compact` `{scope: 0, enabled}` | `internal/ui/model/config_write.go:71-80`; SET-007, CMD-028 |
| Typing | Keep focus on the typing box | `options.tui.composer_focus_only` | switch | `POST …/config/set`; info "Focus stays on the composer" / "Tab moves focus again" | `config_write.go:151-174`; SET-003, CMD-048 |
| Typing | Show the model's newest thought while busy | `options.tui.thought_line` | switch | `POST …/config/set`; info "Busy status shows the model's newest thought" / "Busy status hides the model's thinking" | `config_write.go:186-199`; SET-005, CMD-049 |
| Typing | Composer footer | `options.tui.composer_footer` | button runs `runAction("composer.customize")` (P3.8's Customize Composer form) | P3.8 | SET-011 |
| Work | Fallback compaction | `options.auto_compact_token_limit` | value + "Change…" (2.12) | 2.12 | SET-012 |
| Work | Check-in messages every | `options.check_in_minutes` | number field, minutes (default 2) | `POST …/config/set` | SET-031, LIVE-013 |
| Work | Sub-agent instructions | file | "Edit…" (2.12) | 2.12 | DLG-272 |
| System (Linux) | Memory limit | `options.memory_limit_gb` | value + "Change…" (2.12) | 2.12 | SET-014 |
| System (Linux) | Run AI work on part of the CPU | `options.agent_cpus_enabled` | switch | G-16 `POST /v1/cpu-affinity` `{enabled}` → `{cpus}` | `config_write.go:122-149`; SET-004, CMD-055 |
| System (Linux) | CPUs for AI work | `options.agent_cpus` | text field, CPU list (empty = detect) | `POST …/config/set`, then G-16 if enabled | SET-004 |
| Notifications | Notification style | `options.notifications` | value + "Change…" (2.12) | 2.12 | SET-015 |
| Accounts | Hyper, GitHub Copilot, ChatGPT, Grok | providers.* | "Sign in" / "Sign out" (2.4) | 2.4 | MISC-012, MISC-013 |
| Phone | Phone opens new chats in | G-17 key `options.remote_launch_target` ("terminal" default, "desktop") | two choices | `POST …/config/set` | REM-015 (2.27) |
| Phone | Launcher password | file `remote-password` | "Set password…" (2.27) | 2.27 | REM-016 |
| Providers | Refresh provider list | — | button | G-9 | MISC-019 |

The CPU switch keeps the terminal's texts: info "AI work now runs on CPUs <list>" or "AI work can use the whole CPU again", and the error "CPU <n> is not online (online: <list>)" (`internal/cpuaffinity/cpuaffinity_linux.go:64`). The terminal pins its own process tree (`cpuaffinity_linux.go:24-46`); in the desktop the agents run in the engine, so the engine must pin itself (G-16).

Below the rows, a quiet line "Other settings are read from crush.json and crushrc." with the config file paths from About (2.28). Each row shows the effective value from `GET …/config` and re-reads on `config_changed` (section 4).

**Honored options (SET-006, SET-008, SET-010, SET-013, SET-016 to SET-030, SET-032 to SET-037).** These are config the engine and the existing phase 3 surfaces read; the desktop adds no rows. What the desktop must do per row:

| Row | Desktop duty |
|---|---|
| SET-006 `options.tui.scrollbar` | The chat follows it through phase 3 (CHAT-094, CHAT-095), and so does the helper view, which is P3.5's Timeline. Every scrolling surface of these pieces (sheets, the left list's added sections, terminal tabs) uses KIT's `src/components/kit/Scrollbar.svelte` (`{target, mode}`, 7.1), which gives it CHAT-094's look and modes: `default` shows the bar while scrolling and hides it 2 s after; `always`; `never` (PLAN section 7). |
| SET-008 `options.tui.diff_mode` | Phase 3's permission sheet reads it for its starting mode; this design only checks it. |
| SET-010 `completions.max_depth`, `max_items` | Phase 3's @ popup reads them; checked here. |
| SET-013 `disable_auto_summarize` | Engine. Checked: no automatic summary runs past the limit. |
| SET-016 `request_timeout` | Engine; the desktop shows nothing. The terminal's schema text says 60 s while the code uses 2 minutes (`internal/config/options.go:198-212`); PLAN section 12 fixes the text in both apps (G56). |
| SET-017 to SET-028, SET-033, SET-034, SET-036, SET-037 | Engine. The checks set each option in the throwaway config and confirm the desktop shows the effect (tools offered, context files used, skills listed, hook results shown, provider list, attribution in commits). |
| SET-029, SET-030 | crushrc: the engine reads it; checks set the options through a throwaway crushrc. |
| SET-032 | `CRUSH_DISABLE_METRICS`, `DO_NOT_TRACK`, `disable_metrics`: the desktop passes its environment to the engine unchanged and sends no metrics of its own. |
| SET-035 | Keys and tokens never appear in the desktop's probe file, logs, notices or screenshots; the API key field is the only place a key is typed (2.4). |

**Concurrent terminal changes.** Every row re-reads on `config_changed`. Section 4 says when each change reaches the other app.

### 2.14 Themes sheet and color editor (THM; THEME-008 to THEME-027, THEME-028 to THEME-082 rows; CMD-053)

The full theme design is section 3. The sheet is the terminal's theme picker and editor, drawn as sheets:

- **Themes sheet (THEME-008 to THEME-017).** Title "Themes", filter "Type to filter", first row "New Theme...", then a "System" group (built-ins, an overridden built-in marked " (overridden)") and a "User" group, "current" on the active theme (3.2). ↑/↓ preview the highlighted theme live (the whole window recolors); Enter or Ctrl+Y applies and saves; Esc reverts the preview and closes. Ctrl+E edits; Ctrl+R renames a user theme inline ("Rename this theme?"), Enter confirms, Esc cancels; Ctrl+X asks "Delete this theme?" (Y/Enter deletes, N/Esc keeps); Ctrl+D reverts an overridden built-in. The help footer lists only the actions the highlighted row allows. Notices: "Theme switched to <name>", "Theme saved", "Reverted <name> to its built-in colors", "Renamed theme <old> to <new>", "Deleted theme <name>"; deleting the active theme switches to the default (3.3).
- **New theme (THEME-018 to THEME-020).** Title "New Theme", one field with placeholder "my-theme", based on the active theme; errors "theme name cannot be empty", "use lowercase letters, numbers, hyphens, and underscores only", "\"<name>\" is a built-in theme; edit it instead", "theme \"<name>\" already exists"; success "Created new theme: <name>" and the editor opens.
- **Editor (THEME-021 to THEME-027, THEME-028 to THEME-082).** Title "Edit Theme: <name>" (`internal/ui/dialog/theme/editor.go:145`). One row per palette role in the terminal's order (3.1 table): the role name, a swatch, the value as text; every row starts filled with the theme's resolved value (`editor.go:194-238`). ↓/Ctrl+N/Tab next, ↑/Ctrl+P/Shift+Tab previous, wrapping; moving applies a valid value first (`editor.go:70-72`, `:96-111`). Typing previews the value live; values are hex (`#rgb`, `#rgba`, `#rrggbb`, `#rrggbbaa`), an ANSI index 0-255 or a Charmtone name, and a value equal to a Charmtone color is stored under its name (`internal/ui/styles/palette.go:321-366`). An invalid value keeps the old color, shows the swatch in the theme's `error` color and blocks saving; an empty value keeps the role's current color (`editor.go:244-259`, `:294-298`). Enter or Ctrl+S saves the whole resolved palette with its base through G-2 (3.2); Esc leaves without saving and reverts the preview (`editor.go:84-95`). A broken theme file or base silently falls back to the built-in of that name, else charmtone-panther (`editor.go:211-238`, THEME-026). Mouse: click a row to edit it, wheel scrolls (THEME-027).

Each field row THEME-028 to THEME-082 is checked by `w:thm.field[<role>]`: set a distinctive value in the editor, save, confirm the theme file holds it, and confirm the mapped token (3.1) changed on screen where that role is used.

### 2.15 Tray entries, Background Processes and Sub-agents sheets, Ctrl+B (LIVE; LIVE-001 to LIVE-004, LIVE-008 to LIVE-017; DLG-223 to DLG-231; CMD-040, CMD-041)

**Data.** Per window, for the shown chat only (none on the start screen):

- Helpers: `GET …/sessions/{sid}/helpers` → `[proto.Helper{id, session_id, child_session_id, name, cli, model, effort, fast, read_only, status, started, ended, delivered, started_by}]` (engine design 5.3). Read when the chat opens, on reconnect, and every 1 s while any listed helper has `status: "running"` (5.3 Rules; the terminal's cadence, `internal/ui/model/tasks.go:146-152`). Between reads, `helper` events whose `session_id` is the shown chat replace the record with the same `id`. A helper's own helpers belong to that helper's chat, so the parent's tray counts only its direct helpers (`tasks.go:56-69` filters on `SessionID`).
- Jobs: `GET …/sessions/{sid}/jobs` → `[proto.Job{pid, job_id, command, name, started, session_id, started_by}]`, every 1 s while the chat is shown (`tasks.go:22`, `:161-172`; 5.4 Rules: there is no job event).
- Server jobs: a job that listens on a port is shown in the left list's server rows (2.22), not in the tray, as inside the terminal's session host (`tasks.go:161-172`; `internal/agent/servers.go:36-61`). The window learns which jobs listen from G-18 (9.1). Until G-18 exists, every job counts in the tray.
- Linux only (PLAN Q11, `desktop/PLAN.md:32`, `:187`): jobs and listening ports come from the process table, and only `internal/agent/procs/table_linux.go` and `ports_linux.go` replace its empty readers (`internal/agent/procs/procs.go:19-23`; read through `internal/agent/background.go:60` and `internal/agent/servers.go:44`). On a Mac the lists stay empty, as the terminal's. Every check leg that reads them runs on Linux only.

**Tray entries (LIVE-001).** LIVE puts two entries into S3's Q7 tray with `registerTraySection({id: "helpers", order: 30, …, downFocus: true, onKey})` and `registerTraySection({id: "jobs", order: 40, …, downFocus: true, onKey})` (3-4 design §1.10), helpers first (`tasks.go:243-265`); P3.7's to-dos (order 10) and queue (order 20) come before them.

- Helpers entry: shown while at least one helper of the shown chat has `status: "running"`. Label "1 subagent" or "<N> subagents" (`tasks.go:245-248`), after a turning mark that steps through the four quarters (◐◓◑◒) once per wall-clock second (`tasks.go:37`, `:249-256`). Finished helpers leave the count at once. The terminal keeps their records for 8 s (`tasks.go:21`, `:92-99`) but counts only running ones (`tasks.go:56-69`); CHECKLIST LIVE-001's "linger 8 s" describes the records, not the count.
- Jobs entry: shown while at least one non-server job exists. Label "1 background process" or "<N> background processes" after the gear mark (`tasks.go:259-264`).
- Hint after the entries: "↓ manage" while the tray is not focused, "enter list · ←/→ select · ↑ back" while it is (`tasks.go:277-287`).
- The tray is hidden while the Q8 slot holds a form, and is never shown on the start screen (`tasks.go:121-128`). This is S3's tray rule (3-4 design §1.10); the entries need nothing extra.

**Tray keys and mouse (LIVE-002).** P3.7's typing box calls `focusTray({down: true})` when ↓ is pressed with the caret at the end of the text and no history entry shown (`internal/ui/model/keypress.go:295-300`; 3-4 design §1.10). Both LIVE sections set `downFocus`, so the call focuses the first of them that shows; it returns `false` when neither shows, and the typing box then handles ↓ itself (`tasks.go:191-204`). In the tray, S3's tray keys and each section's `onKey` share the work (3-4 design §1.10, the tray rules):

- ← and → move between the entries, wrapping (`tasks.go:219-222`); S3's keys move between all visible tray sections, P3.7's to-dos and queue included.
- ↑ and Esc give focus back to the typing box (`tasks.go:223-224`).
- Tab and Shift+Tab leave the tray along the window's focus order (3-4 design D26); the terminal's Tab moves between the entries (`tasks.go:219-222`; 9.4).
- Enter, Space, X, Delete, Backspace and Ctrl+X open the selected entry's sheet (`tasks.go:214-217`, `:225-226`); the section's `onKey` takes them.
- Any other key that types text gives focus back to the typing box and is typed there (`tasks.go:227-229`).
- When an entry disappears, the selection moves to the remaining one; an empty tray drops focus (`tasks.go:101-108`).
- Mouse: hover shows the focus look; a click opens that entry's sheet.

**Opening the sheets (DLG-223, DLG-224, CMD-040, CMD-041).** The helpers entry opens "Sub-agents" and the jobs entry opens "Background Processes". The two sheets replace each other (`tasks.go:299-324`). Ctrl+X in a chat opens the selected entry's sheet when the tray has focus, and Background Processes otherwise (`keypress.go:592-599`; `bindKey("global", "ctrl+x", …)`, a key 3-4 design §1.8 leaves to phase 6). The palette entries "Background Processes" (aliases processes, kill; `internal/ui/dialog/commands.go:625`) and "Sub-agents" (aliases agents, tasks, stop; `commands.go:626`) are always offered; on the start screen they open the empty sheet. Sheet ids are the terminal's `dialog.BackgroundID` and `dialog.SubAgentsID`, `background` and `sub-agents` (`internal/ui/dialog/background.go:24-26`; 2.1).

**Rows (DLG-225, DLG-226).**

- Sub-agents: running helpers of the shown chat, oldest first (`tasks.go:56-69`). Title "◐ <name>"; info "<cli>/<model>[/<effort>][ · read-only] · <elapsed>" (`internal/ui/dialog/background.go:251-267`). The `fast` flag is not shown, as in the terminal.
- Background Processes: non-server jobs. Title "⚙ <command>"; info "job <job_id> · <elapsed>", or "pid <pid> · <elapsed>" when there is no job id (`background.go:255`, `:269-272`).
- Elapsed time reads "<m>m<ss>s" under an hour and "<h>h<mm>m" from an hour on (`background.go:275-281`), and is redrawn every second.
- Row identity is the helper `id`, else "job:" + `job_id`, else the pid (`background.go:238-246`). A refresh keeps the selection on the same identity (DLG-227; `tasks.go:181-189`).

**Keys and mouse (DLG-227 to DLG-230).**

- ↑/K/Ctrl+P and ↓/J/Ctrl+N move and wrap (`background.go:145-158`).
- Enter ("open chat") opens the helper view (2.16) for a Sub-agents row with a `child_session_id`; it does nothing on Background Processes rows (`background.go:100`, `:159-162`; DLG-228).
- X, Delete and Backspace stop the selected row at once, with no confirmation. The help calls it "stop" in Sub-agents and "kill" in Background Processes (`background.go:91-95`; DLG-229). The row leaves the list at once and the selection stays at the same position (`background.go:163-171`).
- Esc and Alt+Esc close. There is no filter (DLG-230).
- Desktop additions (2.0 rule 6): a click selects; a double-click opens a helper; the selected or hovered row shows a "Stop" (Sub-agents) or "Kill" (Background Processes) text button at its right end that does what X does; the wheel scrolls.

**Stop and kill (DLG-229, DLG-231).** Always through the engine, never in the window:

- Helper: `POST …/sessions/{sid}/helpers/{hid}/stop` (5.3).
- Job: `POST …/sessions/{sid}/jobs/kill` with `proto.JobRef` `{"job_id": …}` when the job has one, else `{"pid": …}` (5.4).
- Success shows the info "Stopped sub-agent <name>" or "Killed <command>"; failure shows the engine's message as an error (`background.go:172-183`). `helper_not_found` and `job_not_found` mean the item ended or belongs to another chat; the next refresh drops it.
- The terminal app sees the change through the same engine: its task row gets the `helper` event in client mode (5.3), and its job list reads the same route. DLG-231's check proves this.

**Empty (DLG-230).** "No background processes are running." and "No sub-agents are running." (`background.go:205-213`). An open sheet stays open when its last row goes.

**Ctrl+B (LIVE-003).** Ctrl+B in a chat, whatever has focus except a sheet (`bindKey("global", "Ctrl+B", …)`, which S3's router does not ask while a sheet is open, 3-4 design §1.8), calls `POST …/sessions/{sid}/background` → `{moved}` (5.4). In the terminal it is a global key matched before the typing box (`keypress.go:303-311`, `:600-608`).

- `moved: true`: the engine's `message` event changes that tool result, and P3.6's tool group draws it with the `background` status (3-4 design §1.5.6 `ToolCallView.status`, CHAT-061; the terminal marks it at `keypress.go:604-606`). LIVE hides the 60-second hint for that command at once.
- `moved: false`: info "No running command can move to the background" (5.4 Rules).
- No session: the key does nothing.
- The help bar shows Ctrl+B only when `GET …/agent/capabilities` → `{kind, can_background}` says `can_background: true` (`internal/ui/model/help.go:42-46`, `:110-112`; `workspace_cache.go:215`). The window reads it when a chat opens and after every `config_changed` (a model change).

**The 60-second hint (LIVE-004).** "ctrl+b to run in background" shows under a command that has run 60 s, while `can_background` is true, until Ctrl+B is pressed for that command (`internal/ui/chat/toolgroup.go:73-75`, `:206-220`, `:421-423`). S5's change to P3.6's `ToolGroup.svelte` draws the line `toolHint(item)` under a working group (7.1). LIVE registers the function with `registerToolHint(fn)` (`src/lib/toolhints.js`, 6.4): for a group whose running bash call LIVE first saw 60 s ago or more (the terminal also times from the first sight, `toolgroup.go:208-213`), while MOD's `capabilities.can_background` is true and Ctrl+B was not pressed for that call, it answers that text; otherwise `null`.

**Engine-side rows (LIVE-008 to LIVE-017).** The engine already does these; the desktop shows them and its checks prove them.

| Row | Desktop part |
|---|---|
| LIVE-008 | `proto.Helper.status` (`running`, `done`, `failed`, `stopped`; ids like `t12`, `internal/agent/tasks.go:46-53`, `internal/proto/live.go:23`) drives the count, the rows and the helper view band |
| LIVE-009 | The helper's `<crush-task-result>` message reaches the parent chat and is drawn by phase 3 (CHAT-009) |
| LIVE-010 | " · read-only" on the row (DLG-226); the agent preamble is engine-side (`internal/agent/tasks_spawn.go:281-297`) |
| LIVE-011 | API-provider helpers run without permission asks: no Permissions sheet appears (engine) |
| LIVE-012 | Interrupted helpers and jobs resume after an engine restart (engine design 5.5 Rules); the tray shows them again |
| LIVE-013 | Check-in messages every `options.check_in_minutes` (Settings row, 2.13), drawn by phase 3 |
| LIVE-014 | Stray `nohup`/`&` processes are reported in the chat when they end (engine, Linux) |
| LIVE-015 | `crush spawn` flags `--cli`, `--model`, `--effort`, `--read-only`, `--name` show on the helper row |
| LIVE-016 | `crush bg` jobs show as job rows ("⚙ <command>", "job <id> · …") |
| LIVE-017 | The engine gives a chat's AI tool `CRUSH_TASKS_DIR` and `CRUSH_SESSION_ID` (`internal/agent/tasks.go:319-322`), a helper only `CRUSH_TASKS_DIR`, and a read-only helper neither (`internal/agent/tasks_spawn.go:129-135`); the desktop's terminal tabs drop both (2.25) |

**State.** Per window: helpers by id, jobs, server jobs (G-18), `can_background`, tray focus and selection, the open sheet, and two 1 s timers. Everything resets when the shown chat changes.

**Concurrent terminal changes.** A helper stopped or a job killed in the terminal: the `helper` event (`status: "stopped"`) or the next 1 s jobs read removes the row. If both apps stop the same item, the second one gets 404, and its row is gone after the next refresh.

### 2.16 Helper view (LIVE; LIVE-005 to LIVE-007, LIVE-021)

**Opening.** Enter on a Sub-agents row (DLG-228) opens the view for a helper of the shown chat that has a `child_session_id`, while no Q8 form is open (`internal/ui/model/subagent_view.go:53-56`). It closes the Sub-agents sheet (`:62`). The view takes the middle column's place through `showMain({kind: "helper", component: HelperView, props: {helper}})` (S5's `src/lib/mainview.js`, 6.4); `Conversation` stays mounted underneath, hidden (S5's change to `App.svelte`, 7.1). The left list, the right panel and the parent chat's state stay as they are (`subagent_view.go:25-26`).

**Content.** "Opening conversation…" until the first read (`subagent_view.go:305-306`). Then the helper's own chat, drawn by P3.5's `Timeline` with `{sessionId: child_session_id, readOnly: true}` (S5 adds `readOnly`, 7.1). The child chat enters the stores through `api.getSession(id)`, `upsertSession(record, {fromEvent: false})` and `load(id)` (P3.3; `GET …/sessions` leaves child chats out, `internal/db/sql/sessions.sql:37-41`), which read `GET …/sessions/{child_session_id}` and `GET …/sessions/{child_session_id}/messages` (`subagent_view.go:75-91`).

- It reads again at most every 100 ms while `message` or `session` events for the child chat, or `helper` events for this helper, arrive, and never runs two reads at once (`subagent_view.go:93-102`, `:163-177`).
- After a read it keeps the scroll position, unless it was following the bottom (`subagent_view.go:129-149`).
- Spinners move only while the helper runs (`subagent_view.go:133-141`).
- A failed read closes the view and shows the error (`subagent_view.go:125-128`).

**Band and footer.** Where the typing box sits, a read-only band reads "<name> · <status>[ · <model>] · read only". The model is named only while the right panel shows (it shows the parent's model; `subagent_view.go:276-283`). "read only" is always there: it describes the view, not the helper. Under the band: phase 3's footer with the helper chat's model stats (`internal/ui/model/composer_footer.go:209-243`), the tray line without its key hint (`subagent_view.go:321-322`; LIVE-021), and the button "← Return to parent" followed by "esc" (`subagent_view.go:267`, `:324-328`).

**Closing (LIVE-005).** Esc, the button, a Q8 form opening, or the shown chat changing (`subagent_view.go:159-162`).

**Keys (LIVE-006).** (`subagent_view.go:337-372`; `keypress.go:53-61`)

- Esc closes.
- Tab, Shift+Tab, ← and → move focus onto or off the Return button.
- Enter and Space press the button when it has focus, else open or fold the selected item.
- ↑/K scroll up a line and take focus off the button; ↓/J scroll down, and at the bottom put focus on the button.
- PgUp/Ctrl+U and PgDn/Ctrl+D scroll half a page; Home and End go to the ends.
- Ctrl+C or Y copy the selection with the info "Selected text copied to clipboard". Without a selection they do nothing, so Ctrl+C never reaches Quit here.

**Mouse (LIVE-007).** A click on the Return button closes the view. Click and drag select. The scrollbar drags. Links open as in the chat. The wheel scrolls the view 3 lines per notch: this is the PLAN section 12 fix of the terminal bug in which the wheel scrolled the hidden parent chat instead (`subagent_view.go:194-200`; CHECKLIST "Likely terminal-app bugs", LIVE-007). Sheets over the view take the input (`subagent_view.go:178-181`).

**Paste and copy (LIVE-021).** Pastes are ignored (`subagent_view.go:192-193`). Releasing a drag selection does not copy; only Ctrl+C or Y copy (`subagent_view.go:221-224`).

**State.** The helper record (kept current by `helper` events), the child chat's messages, scroll and follow state, button focus. One helper view per window.

**Concurrent terminal changes.** The terminal stopping the helper: the `helper` event updates the band's status and stills the spinners; the view stays open.

### 2.17 Memory warning, memory note, memory box (LIVE; LIVE-018 to LIVE-020)

Linux only. On other systems `MemoryStatus.supported` is `false` and nothing shows (engine design 5.9 c; `memwatch_other.go:7-12`).

**Source.** `GET /v1/memory` → `proto.MemoryStatus{supported, percent, high, episode, top[{name, bytes}], message, limit_bytes, box_used_bytes, total_ram_bytes}` at window start and on reconnect, then `memory_status` events, which the engine sends when `percent`, `high` or `episode` changes (5.9 c). The engine checks every 10 s, warns at 85 %, clears below 80 % and names the top 3 programs (`internal/memwatch/memwatch.go:21-33`, `:144-166`).

**Notice line (LIVE-018).** While `high` is true, every window shows `message` as a warning in the notice line (SIDE-028, P3.8) with no time limit: `ui.notify("warn", message, 0)` at every `memory_status` reading (S3's `ttlMs` 0 keeps it until another notice replaces it, `desktop/src/lib/stores/ui.svelte.js:23-38`). Another notice may cover it; it comes back at the next reading, as in the terminal, where it is refreshed every check (`internal/ui/model/memory_warning.go:24-49`). It clears when `high` turns false: `ui.clearNotice(message)`, which clears only while that text shows (S5 adds the `text` argument to S3's `clearNotice()`, `ui.svelte.js:40-43`; 7.1).

**Pop-up (LIVE-018).** When `high` turns true, each window's SYS listener calls `claimMemoryNotice(episode)`, `POST /v1/memory/claim-notice` → `{claimed}` (6.5); several windows get the same event, and the engine's claim lets one caller win per episode. Only the window that gets `claimed: true` sends the system notification "Crush · Memory almost full" with the body `message`. It is sent even while a window is focused, and with style `auto`; it is not sent with style `disabled` (`memory_warning.go:51-78`). The claim file is shared by every Crush process (`memwatch.ClaimNotify(config.GlobalCacheDir())`, 5.9 c), so terminal and desktop windows together show at most one pop-up per 10 minutes (`memwatch.go:31`).

**Memory note (LIVE-019).** The engine adds `<crush_memory_warning>` to agent turns while memory is high (`memwatch.go:101-110`; `agent/memory_notice.go:13`), using its own checks (5.9 c). The desktop does nothing; its check reads the note in the prompt the fake AI server records.

**Memory box (LIVE-020).** The engine joins `crush.slice` when it starts and ignores a failure (`_ = joinMemoryBox(memlimit.Slice)`, engine design 5.1 b). The desktop app's own process does not join (it starts no agents). Where the box cannot be joined (no systemd user manager, as in the test sandbox), the desktop shows no error and works normally. The limit itself is the Memory Limit sheet (2.12).

**Test seam.** The real-window check needs the engine to report high memory without filling the computer's memory: G-21 (9.1).

### 2.18 Alerts: notifications, sounds, window title, taskbar badge and progress (SYS; NOTE-001 to NOTE-009, LIST-052)

**Window title (NOTE-001).** "<chat title> · <project>" (PLAN section 7). The chat title is the shown chat's `title`, or "New session" when it has none or there is no chat yet (the terminal's empty-title text, `internal/ui/sessionhost/draw.go:267-272`). The project is the base name of the window's project folder (inferred: the plan says "project"). It follows chat switches and `session` events that rename the shown chat. The terminal's own titles differ ("crush <short cwd>" in a chat, `internal/ui/model/draw.go:249`; "crush · <title>" in the session host, `internal/ui/sessionhost/draw.go:190`; CHECKLIST Conflicts 27); the plan's rule settles it.

**One alert service per app.** SYS raises every alert: permission asks (`permission_request`; the prompt itself is P3.7's), questions and secure forms (2.6, 2.7), the memory pop-up (2.17) and finished turns. Each goes through SYS's Rust module `alerts.rs`, shared by all windows, with the commands `alert_notify`, `alert_sound`, `alert_window_state` and `alert_handler` (6.4). Who handles an alert (`alert_handler(projectDir, sessionId)` answers with the window's label):

- A window handles alerts for the chat it shows.
- For a chat of a project that no window shows, the project's lead window handles it: the earliest-opened window of that project still open. This is the desktop form of LIST-052: in the session host, hidden chats are told they are unfocused, so they still notify (`internal/ui/sessionhost/session.go:85-88`, `:359-370`; `host.go:337-348`).

**Focus rule (NOTE-003).** A notification is sent only when the handling window is not focused, or the chat is not the one it shows. Sounds play whether the window is focused or not. Style `disabled` stops both (`internal/ui/model/notification.go:13-29`, `:97-106`). The memory pop-up is the exception (2.17).

**Kinds (NOTE-002, NOTE-006).**

| Trigger | Sound | Notification (title / body) | Terminal fact |
|---|---|---|---|
| `agent_event` with `type: "agent_finished"` | "complete" | "Crush finished" / "\"<session_title>\" is done" | `completion_notification.go:28-45` |
| `permission_request` (P3.7's prompt) | none | "Crush is waiting..." / "Permission required to execute \"<tool_name>\"" | `update_workspace.go:74-83` |
| `question_batch_request` (2.6) | "message-new-instant" | "Crush is waiting..." / "<N> questions need your input" | `update_workspace.go:86-97` |
| `secure_form` (2.7) | "message-new-instant" | none | `secure_question.go:74` |
| `agent_event` with `type: "error"` | none | none; the badge shows the error state | `agent_events.go:167-170` |
| `agent_event` with `type: "re_authenticate"`, `"aws_sso_auth"`, `"aws_sso_auth_result"` | none | none; the sign-in sheets of 2.4 open | `agent_events.go:171-176` |
| `memory_status` rise (2.17) | none | "Crush · Memory almost full" / `message` | `memory_warning.go:67` |

The finished alert is checked twice, as in the terminal: when the event arrives and again right before the sound and the notification. Both times the chat must not be busy (`is_busy` from `GET …/agent/sessions/{sid}`) and must have no queued prompts (`GET …/agent/sessions/{sid}/prompts/queued` → `0`) (`completion_notification.go:15-26`, `:47-60`).

**Backends (NOTE-004, DLG-275 to DLG-279).** The style is `options.notifications` (2.12). On the desktop:

- `auto`, `native` and `osc` send a system notification: freedesktop notifications on Linux, the Notification Center on a Mac, through `alert_notify(title, body)` (6.4; `tauri-plugin-notification` 2.7.0, `desktop/PLAN.md:376`). The terminal's `auto` turns notifications off where the terminal sends no focus reports (`notification.go:80-89`); a desktop window always knows its focus, so `auto` always notifies (9.4, D9). An unknown value counts as `auto` (`notification.go:57-58`).
- `bell` plays the system alert sound instead of a notification: the player chain below with the sound id "bell" on Linux, the system beep on a Mac (inferred; the terminal writes a bell character and lets the terminal decide).
- `disabled` sends nothing and plays nothing.

**Sounds (NOTE-005).** The terminal's player chain, run by `alert_sound(id)` (6.4) with one 5 s limit for the whole chain (`internal/ui/notification/sound.go:30-33`, `:54-57`): on Linux `canberra-gtk-play --id <sound> --description Crush`, then `pw-play /usr/share/sounds/freedesktop/stereo/<sound>.oga`, then `paplay` with the same file; on a Mac `afplay /System/Library/Sounds/Glass.aiff`. The first player that succeeds ends the chain; a missing player is skipped silently (`internal/ui/notification/sound.go:17-64`). Test runs stay silent: the sandbox has no sound device, and recording stand-ins for the three players come first on its PATH (section 8).

**Taskbar badge (NOTE-007).** Each window has an alert state, as the terminal's program status (`internal/ui/model/program_status.go:16-37`):

- blocked: a permission ask (phase 3), a question form (2.6) or the AWS sign-in sheet (2.4) is waiting in it;
- working: its chat is busy (2.0 rule 2);
- done: a turn finished there while the window was not focused; error: a turn ended with an error event while it was not focused;
- idle otherwise.

Each window reports its state with `alert_window_state(state, projectDir, sessionId, focused)` (6.4). From that app-wide table, `taskbar.rs` shows on the taskbar or dock icon a count of windows that are blocked, done or in error, and asks for attention when a window becomes blocked while not focused.

**Progress (NOTE-008).** While any window is working, `taskbar.rs` shows an indeterminate progress bar on the icon, from the same table. `options.progress: false` turns it off (`internal/config/options.go:169`; `internal/ui/model/ui.go:536`). The terminal sends a random percent only to keep one terminal from hiding the bar (`internal/ui/model/draw.go:285-293`); the desktop has no such need.

**Focus clears (NOTE-009).** When a window gains focus, its done and error states clear, and the badge count follows (`internal/ui/model/update_ui.go:56-66`). The terminal also asks for the terminal's background color there; that has no desktop meaning (MISC-003 and MISC-004 are left out).

**State.** App-wide: per-window alert state, the lead window per project, the last memory `episode` claimed. Per window: focused, shown chat.

**Concurrent terminal changes.** Each app alerts for its own windows. When both apps show the same chat, both may alert, as two terminal windows do today.

### 2.19 Updates (SYS; UPD-001 to UPD-008, CMD-043, CMD-044)

The checks run once per app start, when the first window is ready, and again from the palette. Offers appear in one window: the focused one, else the first window opened.

**Upstream notice (UPD-001).** On an `update_available` event (`{current_version, latest_version, is_development}`; `internal/server/events.go:99-107`), and only when `GET /v1/updates/crush` reports `has_checkout: false` (fork builds get UPD-004 instead; `internal/ui/model/update_ui.go:184-188`), the window shows for 10 s the update notice "Crush update available: v<current> → v<latest>." or "This is a development version of Crush. The latest version is v<latest>." (`update_ui.go:189-199`).

**Agent CLI updates (UPD-002, UPD-003, CMD-043).**

- Automatic check: `GET /v1/updates/clis?manual=false` → `proto.CLIUpdateCheck{skipped, updates[{name, bin, path, current, latest}], errors}` (engine design 5.9 d). The engine applies the hourly gate and skips declined versions (`internal/cliupdate/state.go:14`, `:62-98`). `skipped: true` or no updates: nothing shows.
- Palette "Update Agent CLIs" (`commands.go:628`): info "Checking agent CLIs for updates…", then `GET /v1/updates/clis?manual=true`. No updates: success "All agent CLIs are up to date", or the warning "Couldn't check every CLI: <errors joined with "; ">" (`internal/ui/model/cliupdate.go:52-95`).
- Offer: a yes/no form in the Q8 slot, built locally with ASK's form component (2.6) and answered locally, never sent to the engine: label "CLI updates", question "Install updates for <names>?", description "<name> <current> → <latest>" joined with " · ", then ". Each CLI runs its own updater in the background. No skips these versions until newer ones come out." (`cliupdate.go:97-130`). Names join as "a", "a and b" or "a, b and c" (`cliupdate.go:211-217`). SYS opens it with ASK's `askYesNo({label, question, description, onAnswer})` (7.4), which opens the slot with the kind `update` (S5's slot rule, 7.1).
- Yes: update notice "Updating <names>…", then `POST /v1/updates/clis/install` `{bins}` (202). The result arrives as `cli_updates_installed` `{results[{name, latest, error}]}` and shows "Updated <name latest, …>" with "; failed: <errors>" when some failed: success when all passed, error otherwise (`cliupdate.go:144-188`). Update notices last 10 s (`cliupdate.go:190-194`).
- No: `POST /v1/updates/clis/decline` `{bins}` and info "Skipped these CLI updates" (`cliupdate.go:121-123`).

**Crush updates (UPD-004 to UPD-007, CMD-044).** These need the fork checkout; `GET /v1/updates/crush?manual=…` → `proto.CrushUpdateCheck{has_checkout, release{tag, current, dir}, staged, declined, error}` (5.9 d).

- Palette "Update Crush" (`commands.go:629`): no checkout → warning "This Crush has no fork checkout to update"; an update already running → info "Crush is already updating"; otherwise info "Checking for a new Crush release…" (`internal/ui/model/selfupdate.go:47-75`).
- Results: `error` → warning "Couldn't check for a new Crush: <error>" (manual only); no `release` → success "Crush is on the latest stable release" (manual only); automatic and `declined` → nothing (`selfupdate.go:77-91`).
- `staged` set (already built): the install offer comes at once (`selfupdate.go:96-98`).
- First offer: yes/no form, label "Crush update", question "Update Crush to <tag>?", description "Upstream released <tag> (you have <current>). Crush first saves any uncommitted changes in a commit, then brings the update into <dir>, builds it and runs every test in the background. If files clash or tests fail, an AI agent fixes them. You're asked again before anything is installed. Saying No skips this version." (`selfupdate.go:99-108`). No → `POST /v1/updates/crush/decline` `{tag}` and info "Skipped Crush <tag>".
- Yes (UPD-005, UPD-006): update notice for one hour "Updating Crush to <tag> in the background: saving changes, merging, building and testing…" (`selfupdate.go:146`). SYS's command `selfupdate_run(tag)` (6.4) runs the whole background update as one child process: commit, merge, AI fix of clashes, build and test, AI fix of failures, with the log in `selfupdate.LogPath()` (`~/.local/state/crush/update/update.log`), the model `codex-cli/gpt-6.1-sol` unless `CRUSH_SELF_UPDATE_MODEL`, and the test run under `systemd-run` with a 25-minute cap when it is present (`internal/selfupdate/selfupdate.go:249-257`, `:436-448`, `:466`; `internal/ui/model/selfupdate.go:142-170`). Today the CLI has `merge`, `build` and `install` steps but no AI-fix step, and `merge` stops with an error on clashes (`internal/cmd/self_update.go:51-73`); the desktop needs G-19 (9.1). Failure → error "Crush <tag> update stopped: <first line>. Details: <log path>" (`internal/ui/model/selfupdate.go:172-177`).
- Install offer (UPD-007): label "Crush update", question "Install Crush <tag> now?", description "It merged cleanly" or "An AI agent fixed the clashes in <files>", then " and every test passed. The old version is backed up, and the merge is uploaded to your fork on GitHub. Restart Crush afterwards to use it. Saying No keeps the tested build for later (Update Crush in the command palette)." (`internal/ui/model/selfupdate.go:182-197`). Yes runs `selfupdate_install()` (6.4), that is `crush self-update install --json` (G-19 adds the machine-readable output): success "Installed Crush <version> and uploaded it to GitHub. Restart Crush to use it."; upload failed → warning "Installed Crush <version>, but <first line>. Restart Crush to use it."; failure → error "Couldn't install the Crush update: <first line>" (`internal/ui/model/selfupdate.go:200-208`). No → info "The tested Crush <tag> waits in Update Crush".
- "Restart Crush" means the engine: the running engine keeps the old binary until every app on the project has left (engine design 5.2). The desktop does not restart it by itself.

**Offers while another form is open (UPD-008).** When the Q8 slot already holds a form, the offer becomes a 10 s update notice instead, `ui.notify("update", text, 10000)`: "<name> <latest> is available. Run Update Agent CLIs from the command palette." (or "Updates for <names> are available. …"), "Crush <tag> is out. Run Update Crush from the command palette.", or "Crush <tag> is built and tested. Run Update Crush from the command palette to install it." (`cliupdate.go:138-142`, `:196-201`; `internal/ui/model/selfupdate.go:107`, `:110-116`, `:197`). When a question form or a secure form takes the slot from an open offer (S5's slot rule, 7.1), the offer becomes the same update notice (`cliupdate.go:43-50`).

**State.** App-wide: the last check results, whether a Crush update child is running, the window that holds the offer.

**Concurrent terminal changes.** The state files are shared (`internal/cliupdate/state.go`), so a version declined in one app is skipped by the other, and the hourly gate counts both. Nothing stops the terminal app and the desktop app from starting a Crush update at the same time: "Crush is already updating" is kept per process (`internal/ui/model/selfupdate.go:60-65`), and `internal/selfupdate` has no lock (9.2).

### 2.20 Left list (LST; LIST-001 to LIST-003, LIST-008 to LIST-011, LIST-013, LIST-018, LIST-044, LIST-045, LIST-053, LIST-055; LIST-012's phase 6 part; THM: LIST-054)

The left list is the desktop form of the terminal's session host (PLAN section 7, first row). The first screen already draws it from sample data (`desktop/src/components/ChatList.svelte`: wordmark, New chat, search, chats by day, the open chat on a band with a vermilion edge). Phase 3 makes the chat rows live: P3.9 owns `ChatList.svelte` and `list/ChatRow.svelte` and fills them from `listmodel.buildRows(sessions, {parentOf, isClosed})`; P4.1 nests branches, hides closed chats and owns the row's close control (3-4 design §3.3, §4.8). The phases 3-4 design maps LIST-012's phase 3 states and LIST-033 to LIST-037 (its §2.6) and LIST-016 (its §3.3); section 1 points to them. LST adds the rest through S5's registries (7.1): the terminal rows and server rows as list sections (`registerListSection`), the phase 6 row states (`registerRowState`), the strip, and the list keys.

**What it lists (LIST-001, LIST-010).** Top to bottom:

1. The project's chats, as P3.9 draws them from the sessions store (`GET …/sessions`: top-level chats; P4.1 nests branches and hides closed chats), newest `updated_at` first, in groups by local calendar day (the first screen's groups, `desktop/src/components/ChatList.svelte:29-31`; 9.2): "Today", "Yesterday", then "<weekday> <day> <month>", for example "Tue 14 Oct" (inferred wording; the picture shows "Today"). A new chat that has no session yet shows on top as "New session" while this window shows it.
2. The terminal tabs of this window (2.25), LST's list section `terminals` in the `rows` place, under a group label "Terminals [<n>]", as in the terminal's group line "Terminals [n]" (`internal/ui/sessionhost/draw.go:450-469`). Chats always come before terminals (`internal/ui/sessionhost/draw.go:18-23`).
3. Server rows (2.22), LST's list section `servers` in the `footer` place.

**Rows (LIST-011, LIST-018).** A chat row is P3.9's `ChatRow` (3-4 design §4.8): the title (else "New session", `internal/ui/sessionhost/draw.go:267-272`), the AI tool under it (from the chat's `context_budget.provider`; PLAN section 15 decision), the updated time on the right, and the state line (below); LIST-011's check proves that content on live data. A terminal row has "Terminal" (`internal/ui/sessionhost/terminal.go:50`) and its current folder's base name (2.25). Everything comes from engine records and events: `session` events add, update and remove rows (title, `updated_at`); `is_busy`, `has_unfinished_work` and `pending_prompt` come only from reads, never from events (engine design 5.1 a). So LST reads `api.getSession(id)` (`GET …/sessions/{sid}`) and stores the answer with `upsertSession(record, {fromEvent: false})` (P3.3) for a chat when an event names it (`message`, `session`, `run_complete`, `agent_event`, `helper`), at most once per 500 ms per chat with one read in flight (inferred cadence), and every 1 s for chats in the working or background state (the terminal's 1 s job poll, `internal/ui/model/tasks.go:22`); P3.9's row states follow the store. `prompt_pending` events (`{session_id, kind, pending}`, engine design 5.1 e) set and clear the waiting state at once. The whole list is read again on reconnect.

**States (LIST-012, phase 6 part).** The terminal's rule (`internal/ui/model/session_host.go:58-66`; marks `internal/ui/sessionhost/draw.go:274-297`) takes the first match of waiting, working, background, unread, ready and unknown. P3.9's `ChatRow` draws the first three (3-4 design §2.6, LIST-012): waiting (`pending_prompt` "permission" or "question": clock mark, "Waiting for permission" or "Waiting for your answer"), working (`busy(id)`: working mark, "Working…") and background (`has_unfinished_work`: "Background"). LST adds the other three with `registerRowState(fn)`, which S5's change to `ChatRow` draws only on a row that has none of phase 3's states (7.1):

| State | When | Desktop state line | Terminal mark |
|---|---|---|---|
| unread | the chat went from working or background to ready while not shown in this window | check mark, "Done" | `✓` |
| ready | otherwise | no state line | `⠶` |
| unknown | before the first read | no state line | `·` |

"Done" is inferred. Unread clears when the chat is shown, and when it starts working again (`internal/ui/sessionhost/host.go:378-393`, `:350`). LST keeps the unread set in `src/lib/leftlist/unread.js`, watching `busy(id)` and `has_unfinished_work` in the sessions store. A terminal row has the terminal mark (the terminal draws "›"; look 5.2) and no state line. The terminal's "×" stopping mark has no desktop use: closing a chat hides it (phase 4) and a closed terminal tab leaves at once (2.25).

**Working mark (LIST-013).** Eight frames, 100 ms each, aligned to the wall clock so every mark in every window shows the same frame (`frame = floor(now_ms / 100) mod 8`), and the timer runs only while a visible row is working or in background (`internal/ui/sessionhost/activity.go:9-63`). The mark is drawn (look 5.2), not the braille glyphs. `u:lst.spinner_clock` checks the frame function.

**Switching (LIST-008).** A click on a row, or Alt+1 to Alt+9 (the row's number among all rows, chats first; `internal/ui/sessionhost/input.go:53-56`), shows that chat or terminal in this window. For a chat this is `openChat(id)` (P3.3); for a terminal tab it is TERM's `showTerminal(id)` (2.25). Showing a chat clears its unread state.

**Reordering (LIST-009).** Alt+Shift+↑ and Alt+Shift+↓ move the shown row one place, and dragging a row moves it, only within its own group (`input.go:47-50`, `:245-286`). Chats are ordered by `updated_at` and grouped by day, so dragging applies to terminal tabs only; on a chat row the keys and the drag do nothing (inferred: the picture's day groups leave no manual order). The terminal tab order is per window and not saved.

**Collapsing (LIST-002, LIST-003, LIST-044).** The control "‹‹" at the right end of the list's top row, Alt+S, or a click on "›" at the top of the strip switch between the open list and the narrow strip (`internal/ui/sessionhost/draw.go:39-49`, `input.go:41-43`). The strip shows, top to bottom: "›" (the whole top row opens the list), "+" (new session through the folder picker, 2.21), ">_" (new terminal through the folder picker), then one block per row: its state mark and its number (the Alt+number, at most two digits), with a bar on the shown row and a rule between chats and terminals (`internal/ui/sessionhost/draw.go:471-500`; CHECKLIST LIST-044). The strip has no notice line and no server rows (`servers.go:96-99`). Below the narrowest width that holds the open list, the strip is forced, and the saved choice is kept (`host.go:20-22`, `:440-443`). S3's App hides the right panel first, while the page's inner width is less than `COLUMN_LIMITS.left.min + COLUMN_LIMITS.right.min + COLUMN_LIMITS.middleMin` (1,060 picture px), the window is under 640 tall, or compact mode is on (`desktop/src/App.svelte:22-27`, `:66-85`); the list is then forced to the strip while the inner width is less than `COLUMN_LIMITS.left.min + COLUMN_LIMITS.middleMin` (740; `desktop/src/lib/columns.js:5-10`). The choice is saved as `list_collapsed` in the desktop settings (section 4), so every window and the next start agree (the terminal's `session-list.json`, `internal/ui/sessionhost/prefs.go:9-28`, stays the terminal's own).

**Scrolling (LIST-045).** The list scrolls normally with the wheel and its scrollbar (SET-006), and keeps the shown row in view when it changes (CHECKLIST LIST-045's GUI note; the terminal instead drops rows off the top, `internal/ui/sessionhost/draw.go:95-110`).

**Covered pictures and pointer capture (LIST-053, LIST-055).** These are the normal web behavior: sheets draw over images, which come back when the sheet closes, and a drag keeps the pointer (`setPointerCapture`) until release or cancel. The checks prove that a picture under a sheet is not drawn over it, and that a row drag ends on release outside the list.

**Theme (LIST-054, THM's row).** The list draws in the desktop's theme from the first frame: S5's `public/theme-boot.js` sets THM's cached tokens before the page draws (3.3). The shown row sits on the band color with the accent bar (3.1).

**State.** Per window: rows, per-chat states and their read timers, unread set, terminal tab order, hover, drag. App-wide: `list_collapsed`.

**Concurrent terminal changes.** Chats the terminal app creates, renames or deletes arrive as `session` events and show at once; its prompts and runs change the states through the reads above.

### 2.21 New chat, folder picker, opening screen, start flags (LST; LIST-004, LIST-006, LIST-007, LIST-038 to LIST-042, LIST-048, LIST-049; CMD-014; the phases 3-4 design's LIST-033 to LIST-037)

**New chat (CMD-014; LIST-033 is the phases 3-4 design's, its §2.6).** The New chat button, Ctrl+N and the palette entry "New Session" (alias clear; `internal/ui/dialog/commands.go:463`; `runAction("chat.new")`, P3.9) start a new chat in this window's project folder, with no picker. The steps below are the terminal's, which P3.9 builds (3-4 design LIST-033, D13):

- Only when a chat is shown (`internal/ui/model/session.go:317-322`); on the start screen they do nothing.
- Busy → warning "Agent is busy, please wait before starting a new session..." (`session.go:323-325`).
- The chat being left is discarded if it is an empty draft: `POST …/saved-sessions` with `{"DiscardEmpty": true, "Session": {"ID": "<sid>"}}` (`internal/client/sessions.go:74-79`; `session.go:403`).
- The window shows the new-chat screen and reports `POST …/current-session` with an empty session (`session.go:418`). The session itself is created by phase 3's send when the first message goes.
- The terminal also stops the language servers and every background process of its process (`session.go:406-416`). In the shared engine both belong to more than this chat: the PLAN section 15 decision keeps the language servers running, and this design keeps the chat's jobs running too, visible in that chat's tray (9.4, D10).

**Opening a chat (LIST-034 to LIST-037, the phases 3-4 design's rows, its §2.6).** P3.9 and P3.3 build these; the terminal facts stay here for LST's checks of the folder-picker and switch paths that lead to them:

- LIST-034: until its messages are read, the middle column shows the spinner and "Opening <title>…" (title with runs of spaces joined), or "Opening conversation…" for an untitled chat, and the elapsed seconds " <n>s" from 1 s on (`internal/ui/model/session_load.go:574-594`). A newer switch wins; a chat deleted while opening is abandoned (CHECKLIST LIST-034).
- LIST-035: the chat's last model comes back if still offered: the newest assistant message that is not a summary names `provider` and `model`; when they differ from the current large model and `config.IsModelAvailable` holds, the window saves them with `POST …/config/model` and calls `POST …/agent/update` (`internal/ui/model/model_select.go:40-101`). The terminal also applies the provider's theme at that point (`model_select.go:87`); 3.4 says what the desktop does with it.
- LIST-036: events for the chat that arrive while it loads are kept and applied on top of the read, skipping chunks older than the read (`session_load.go:195-238`, `:530-572`). This is P3.3's message store (3-4 design LIST-036).
- LIST-037: a `session` event `deleted` for the shown chat moves the window to a new chat (`internal/ui/model/update_workspace.go:114-126`).

**Folder picker (LIST-004 to LIST-007, LIST-038 to LIST-042).** A sheet (PLAN section 7: a text box with the same completion, recent folders, errors and keys, plus the system folder dialog). Opened by Alt+N, the strip's "+" and the palette (title "New session in…"), and by Alt+T, the strip's ">_" and the Terminals group's "+" (title "New terminal in…") (`internal/ui/sessionhost/draw.go:636-640`; `input.go:35-40`). It does not open while a close box is open (`host.go:284-287`).

- Field (LIST-038): starts with the shown chat's folder, or the shown terminal tab's current folder (2.25), else the project folder; home is written `~`; it ends in "/", the caret is at the end, no row is picked (`host.go:284-301`; `picker.go:32-52`).
- Matches (LIST-039): folders inside the typed folder whose names start with the last typed part, ignoring case; hidden folders only once the part starts with "."; links to folders count; at most 5; `~` is home and a relative path starts at the field's starting folder; an unreadable folder or an empty path gives no matches (`picker.go:67-118`). LST runs this rule in `src/lib/leftlist/picker.js` over VIEW's `dir_list` (6.4).
- Recent (LIST-039): SES's `recent_folders(5)` (6.4): the heading "Recent" and up to 5 folders from `projects.json` (all entries, latest first), skipping missing folders and duplicates; no heading when there are none (`picker.go:36-43`; `internal/cmd/session_host.go:50-61`; `draw.go:664-669`). CHECKLIST LIST-039 says "saved project folders"; the code lists every entry.
- Hint: "↑↓ pick · tab open folder · enter start · esc cancel" (`draw.go:673`).
- Browse: a "Browse…" button opens the system folder dialog (`folder_pick(start)`, 6.4, `folderdialog.rs`); a chosen folder starts there at once; cancelling changes nothing.
- Keys (LIST-007, LIST-042): ↑, Shift+Tab and Ctrl+P pick the previous row and ↓ and Ctrl+N the next, with the typed path as a stop above the first row, wrapping (`picker.go:126-133`); Tab fills in the picked row, else the first match, with "/" added (`picker.go:135-145`); Home/Ctrl+A and End/Ctrl+E; Backspace; Delete/Ctrl+D; Ctrl+W and Alt+Backspace delete the last folder name with its slash (`picker.go:201-216`); Ctrl+U clears; Enter starts; Esc cancels (`input.go:73-107`). Alt+letter text is not typed (`input.go:103`). Pasted text loses newlines, returns and tabs (`picker.go:168-182`).
- Errors (LIST-040): Enter with an empty path → "Type a folder."; a path or picked row that is not a folder → "No folder at <path>" with home as `~`, cut from the front with "…" when long (`picker.go:147-166`; `draw.go:670-688`). Any edit, or a click on a row, clears the error; moving with ↑/↓ leaves it (`picker.go:60-65`, `:126-133`).
- Mouse (LIST-041): a click on a match or recent row picks it; a click on the picked row starts there; a click outside the sheet closes it; other clicks inside do nothing (`draw.go:690-708`).

**Starting (LIST-004, LIST-005, LIST-048).**

- New session: if the chosen folder is this window's project folder, a new chat starts in this window (as Ctrl+N). Otherwise the app opens, or focuses, the window for that folder and starts a new chat there, through `window_open` with `projectDir` and `newChat: true` (6.4; a focused window gets `window-request` and LST's plugin runs `chat.new` there) (windows are per project, PLAN section 7; the engine joins folders by project, engine design 5.2). It fails with the warning line "Couldn't start a session." on the list's bottom row, above the server rows, never in the strip, cleared by the next attempt (`internal/ui/sessionhost/host.go:319-326`; `draw.go:319-326`).
- New terminal: a terminal tab in this window, in the chosen folder: TERM's `openTerminal({cwd})` (2.25, 7.4). Failure: "Couldn't start the terminal." in the same place (`terminal.go:24-30`).

**Start flags (LIST-049).** The first window gets every start flag (`--cwd`, `-s`, `--data-dir`, `--yolo`, `--debug`; P3.9 owns the parsing, START-021; S5's `app_info` answers per window, 6.4). Windows opened later get only the folder plus the first window's `--yolo` and `--debug` values, as the session host passes `--cwd <folder> --yolo=<on/off> --debug=<on/off>` to later tabs (`internal/cmd/root.go:220-232`; `internal/cmd/session_host.go:28-48`). `-s` and `--data-dir` apply to the first window only.

**State.** Per window: the picker's field, caret, rows, picked row, error. Nothing persists.

**Concurrent terminal changes.** `projects.json` may change between openings; Recent is read each time the picker opens.

### 2.22 Server rows (LST; LIST-028 to LIST-032, LIST-047)

**What they are (LIST-028).** Ports that a chat's background jobs listen on, at the bottom of the open list, each port once, in list order (`internal/ui/sessionhost/servers.go:48-62`). They show only in the open list, and only as many as fit while leaving room for one chat (`servers.go:94-104`). The data comes from G-18 (9.1): for every chat of the project, the listening ports of its jobs, with the port, the program holding it, the job's `name` and `command`, its `job_id` or `pid`, and its `session_id`. The window reads it every 1 s while it has a workspace (the terminal's job poll, `internal/ui/model/tasks.go:22`). These jobs leave the tray (2.15). Linux only (PLAN Q11, `desktop/PLAN.md:32`, `:187`; the ports come from `internal/agent/procs/ports_linux.go` through `internal/agent/servers.go:44`): on a Mac the rows stay empty, as the terminal's, and the `w:lst.server_*` checks run on Linux only.

**Rows (LIST-029, LIST-047).** As the terminal draws them (`internal/ui/sessionhost/draw.go:336-375`; `servers.go:64-92`):

- A single server: "↗ <name>" with ":<port>" on the right, or "↗ localhost:<port>" when the job has no name. The name is the job's `name`, else the first word of its command without `=`, base name only (`internal/agent/servers.go:21-32`). The label is a link to `http://localhost:<port>`.
- A program with two or more ports folds into one row "▸ <name>" with "<N> ports"; opened, it shows "▾ <name>" and its servers indented under it, each named after the program holding its port.
- The picked row gets the selected band, the bar, and "×" at the right; the room for "×" is kept on every row so nothing moves.
- Below the rows, while one is picked: " ↵ open · x stop · esc back" (`servers.go:37`; `draw.go:331-333`).

**Keys and mouse (LIST-030).** (`servers.go:181-210`; `input.go:219-243`)

- A single server: the first click picks it; a second click or Enter opens it in the browser.
- A folded program: one click picks it and opens or closes the fold.
- X, Shift+X or a click on "×" stop the picked server's job.
- ↑/↓ move between server rows; Esc lets go; any other key lets go and goes on to the chat. A picked row that disappears lets go by itself (`servers.go:139-151`).

**Open (LIST-031).** `http://localhost:<port>` in the browser through P3.4's `openLink(url)` (P3.2's `open_link`). Failures are only logged (`servers.go:39-46`).

**Stop (LIST-032).** A direct engine call (PLAN section 7): `POST …/sessions/{session_id}/jobs/kill` with `{"job_id": …}` or `{"pid": …}` (engine design 5.4). The rows of that job leave at once; the next read shows whatever still listens (`internal/ui/model/session_host.go:80-95`). This replaces the terminal's private OSC 7374 request (`servers.go:171-179`).

**State.** Per window: server rows, open folds, picked row.

**Concurrent terminal changes.** A job the terminal stops disappears at the next 1 s read.

### 2.23 Closing a window, the last window, empty drafts (LST; LIST-017, LIST-026, MISC-024)

**Closing a window (LIST-026).** The system close button (the window event `close-requested`, 6.4), Ctrl+Shift+W (LST's `bindKey("global", "ctrl+shift+w", …)`; inferred key: the terminal's window is closed by its terminal), and Quit (2.24) close this window through S5's `closeWindow()` (`src/lib/closing.js`), which runs the registered close steps by `order` and then `window_close` (6.4). In order:

1. LST's step `draft` (order 10), MISC-024: if the shown chat is an empty draft and not busy, discard it with `POST …/saved-sessions` `{"DiscardEmpty": true, "Session": {"ID": "<sid>"}}` (`internal/cmd/root.go:158-164`; `internal/client/sessions.go:74-79`). A busy draft is kept.
2. TERM's step `terminals` (order 20): stop the window's terminal tabs with `pty_close_all` (2.25).
3. SHARE's step `remote` (order 30): stop the window's phone share, if any, with `POST …/remote/stop` (2.27).
4. `window_close`: `connection::close` (P3.1) sends `DELETE /v1/clients/{client_id}` (engine design "What the desktop app (Rust side) calls" 2, step 7), then the window is destroyed. The engine then applies Q2 at once: this window's prompts, helpers and jobs keep going when another app (a desktop window or the terminal app) shows their chat, and stop otherwise (engine design 5.5, `GoneRetired`). If the window dies without the call, the same happens after the 10 s grace (`GoneExpired`).

There is no confirmation, as closing the terminal app's window has none (CHECKLIST LIST-026).

**The last window (LIST-017).** When the last window closes, the app exits, as the session host does when its last tab ends (`internal/ui/sessionhost/host.go:409-438`). On a Mac the app also exits then (inferred; PLAN Q11 lists no Mac exception, and windows are the app's only content).

**State.** None beyond the steps above.

**Concurrent terminal changes.** Covered by Q2: the terminal app showing a chat keeps its work alive when the desktop window closes.

### 2.24 Quit sheet (PAL; DLG-288 to DLG-293, CMD-056)

**What Quit means.** In the terminal, quitting ends that terminal window's Crush. The desktop's counterpart is the window: "Yep!" closes this window as in 2.23 (`closeWindow()`), and the app exits after its last window (`desktop/PLAN.md:379`). On a Mac, the app menu's Quit (Cmd+Q) closes every window, each as in 2.23.

**Opening (DLG-288).**

- Ctrl+C anywhere, over any sheet or Q8 form, except while the Quit sheet is open anywhere in the stack (`internal/ui/model/keypress.go:103-109`). PAL's `src/lib/plugins/palette.js` binds it with a plain `bindKey("global", "Ctrl+C", …)`, and the handler calls `openDialog("quit")`. S3's router asks `global` first for Ctrl+C, before every other scope and before a sheet's grace, unless the quit sheet is open (3-4 design §1.8; `desktop/src/lib/keys.js:52-67`, `:91-96`), so the plain binding works over every sheet and Q8 form. On the desktop Ctrl+C is also copy (3-4 design D28): with text selected (in a field, a sheet or the chat) the handler returns `false`, so the key goes on to the scope that copies, and the sheet does not open (9.4, D16). While a terminal tab has focus the key belongs to the shell: S5's key rule 3 (7.1).
- Typing exactly "exit" or "quit" and sending opens it (`keypress.go:337-340`; P3.7's send calls `openDialog("quit")`, 3-4 design §1.10).
- If it is already open, it comes to the front (`internal/ui/model/dialogs.go:129-140`).
- The palette's "Quit" (aliases exit; always the last entry, `commands.go:481-484`) closes the window at once, without the sheet, with `closeWindow()` (CMD-056).

**Texts (DLG-289).** "Are you sure you want to quit?", then the hint "To quit without confirmation" and "press ctrl+c twice." (`internal/ui/dialog/quit.go:20-22`). Buttons "Yep!" and "Nope"; "Nope" is selected when the sheet opens (`quit.go:46-50`, `:148-151`). Help footer "←/→ switch options · enter/space confirm", the dialog's short help that the terminal shows in its help bar (`quit.go:53-58`, `:239-245`).

**Keys (DLG-290, DLG-291).** ←/→ and Tab switch the buttons; Enter and Space press the selected one; Y, Shift+Y and Ctrl+C quit; N, Shift+N and Esc cancel (`quit.go:51-75`, `:91-108`). So Ctrl+C twice quits without looking.

**Mouse (DLG-292).** Hover shows the hover look only; it does not move the selection (`quit.go:87-88`, `:152-154`). A click on the other button selects it; a click on the selected button presses it. There is no double-click timer (`quit.go:114-135`).

**Small window (DLG-293).** On narrow widths the frame's side padding shrinks to 0 (`quit.go:182-185`), and the buttons go inline in the sheet's text, still one row (`quit.go:209-216`).

### 2.25 Terminal tabs (TERM; LIST-005, LIST-014, LIST-015, LIST-019 to LIST-025, LIST-043, LIST-046, LIST-050, LIST-051, LIST-056)

A terminal tab is a shell in a pseudo-terminal, drawn in the middle column by a terminal widget (PLAN section 7). The pseudo-terminal lives on the Rust side: TERM's `pty.rs` on `portable-pty` 0.9.0, behind the commands `pty_spawn`, `pty_write`, `pty_resize`, `pty_cwd`, `pty_close` and `pty_close_all` (6.4). The widget is `@xterm/xterm` 6.0.0 with `@xterm/addon-fit` 0.11.0 and `@xterm/addon-image` 0.9.0 (approved, `desktop/PLAN.md:376`; S5 pins them, 7.1). A tab shows in the middle column through `showMain({kind: "terminal", component: TerminalTab, props: {id}})` (6.4); the chat stays mounted underneath.

**Starting (LIST-005).** From the folder picker (2.21), through `openTerminal({cwd})` (7.4), which calls `pty_spawn` (6.4). The shell is `$SHELL -i`, else `/bin/sh -i`, in the chosen folder (`internal/ui/sessionhost/terminal.go:32-47`). The tab is titled "Terminal" (`terminal.go:50`), joins the Terminals group of the left list (2.20) and is shown.

**Environment (LIST-025).** The app's login-shell environment, `login_env::login_env()` (P3.1; the one it sends the engine), cleaned by the session host's rule for shell tabs (`internal/ui/sessionhost/run.go:53-100`; `internal/cmd/session_host.go:43-45`):

- removed: `TERM`, `COLORTERM`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `LC_TERMINAL`, `LC_TERMINAL_VERSION`, `VTE_VERSION`, `WT_SESSION`; every name starting with `KONSOLE_`, `KITTY_`, `GHOSTTY_`, `WEZTERM_`, `ITERM_` or `TMUX`; `CRUSH_SESSION_HOST`, `CRUSH_TASKS_DIR`, `CRUSH_SESSION_ID` and `CRUSH_CRASH_REPORT`;
- also removed on the desktop: `CRUSH_REMOTE_LAUNCH`, so a `crush` started in a tab never takes the launcher's window ID (9.4, D11);
- added: `TERM=xterm-256color`, `COLORTERM=truecolor`.

`r:term.env_clean` checks the rule on the Rust side with every case above.

**Folder (LIST-020).** The tab's current folder comes from OSC 7 (`file://<host><path>`, accepted only for an empty host, "localhost" or this computer's name, and an absolute path) and, on Linux, from `/proc/<pid>/cwd` of the shell (`terminal.go:89-117`). It names the row's folder (2.20) and fills the folder picker (2.21).

**Colors (LIST-019).** The widget's theme comes from the desktop's theme (3.1), read from the tokens by `src/lib/terminal/xtermtheme.js`: background `--paper`, text `--ink`, cursor `--ink`, selection `--selection` with the text in its own color (9.4, D14), and the 16 ANSI colors `--ansi-0` to `--ansi-15`. A theme change (THM's `theme.revision`, 7.4) recolors every tab at once (`terminal.go:61-80`).

**Scrollback (LIST-021).** Shift+PgUp and Shift+PgDn scroll a page (the height minus one line) when the program is not on the alternate screen (`terminal.go:140-157`). The wheel scrolls 3 lines; on the alternate screen it sends ↑ or ↓ three times instead; when the program asked for mouse reports, the wheel goes to the program (`terminal.go:159-188`). New output keeps a scrolled-back view where it is (`terminal.go:119-133`).

**Selection and copy (LIST-022).** A left drag selects; the selection stays on its text while output arrives; releasing copies it to the system clipboard; Shift forces a selection when the program owns the mouse; a new click clears it (`terminal.go:190-324`). Ctrl+Shift+C also copies (inferred desktop convention; Ctrl+C goes to the shell).

**Paste, clipboard, focus, pictures (LIST-023).** Ctrl+Shift+V, Shift+Insert and middle-click paste, as bracketed paste when the program asked for it; Ctrl+V goes to the shell as its control character (inferred, as in other terminals). A program's OSC 52 clipboard write sets the system clipboard; OSC 52 reads are refused (inferred safety rule). Focus-in and focus-out reports reach the program when it asked for them, following whether the tab is shown and the window focused (LIST-052's rule). Sixel and iTerm2 inline pictures show through the image add-on; Kitty graphics do not (not in scope, 9.6).

**Shift+Enter (LIST-024).** Sends Ctrl+J, as the session host does for every tab (`internal/ui/sessionhost/input.go:109-125`); shells take it as Enter. In chats, Shift+Enter is phase 3's newline.

**Typing jumps (LIST-051).** Typing jumps back to the newest output and clears the selection; pasting jumps back and keeps the selection; resizing clears the selection (`input.go:58-69`; `host.go:199-202`; `session.go:372-376`).

**Cursor (LIST-056).** The widget draws the program's cursor shape (block, bar, underline) and blink, and hides it while scrolled back, except on the alternate screen (`internal/ui/sessionhost/draw.go:192-203`). The folder picker uses the normal text caret (`draw.go:675-677`).

**Closing (LIST-014, LIST-015, LIST-043, LIST-050).** Alt+W on a shown terminal tab, or its row's "×" (shown on hover and on the shown row), calls `askCloseTerminal(id)` (7.4), which opens the close box, TERM's sheet `terminal_close`, over the tab (`input.go:44-46`; `draw.go:392-395`):

- title "Close “Terminal”?", detail "This closes the terminal. Its output is not saved." (`draw.go:546`; `terminal.go:82-87`);
- buttons "Close" and "Keep", "Close" selected first (`host.go:109-112`; `draw.go:553-559`);
- keys: ←, →, Tab, Shift+Tab, H and L switch; Enter and Space press the selected button; Y and Shift+Y close; Esc, N and Shift+N keep (`input.go:13-26`);
- mouse: the buttons are clickable; other clicks, other keys and pastes do nothing while it is open (`input.go:137-142`; LIST-043).

Closing removes the tab from the list at once. `pty_close(id)` (6.4) closes the pseudo-terminal (the shell and its foreground job get the same hang-up as a closed terminal window), sends SIGHUP to the shell, then SIGTERM, and kills its process group if it has not ended after 5 s (`internal/ui/sessionhost/session.go:397-425`; `proc_unix.go:10-21`). Closing the window or quitting the app stops every terminal tab this way and waits at most 6 s (`run.go:32-51`): TERM's close step `terminals` calls `pty_close_all` (2.23). For chat rows, Alt+W runs P4.1's action `chat.close` (LST binds Alt+W for both kinds, 3-4 design D38) and "×" is P4.1's (LIST-014 to LIST-016, chat part).

**When the shell exits (LIST-046).** The tab closes by itself (`exit` in the shell, or a program in it ending) and the row after it is shown, else the one before; when no terminal rows are left, the window shows its chat again (`host.go:409-438`). The terminal's "could not start a Crush session: <error>" for a first session that cannot start (`run.go:24-29`) has its desktop form in the engine connection path: the window shows the error of `crush engine ensure` with Retry (engine design "What the desktop app (Rust side) calls" 4; phase 3).

**State.** Per tab: the pseudo-terminal id, title, folder, scroll and selection (in the widget). Per window: the tab list. Nothing persists: output is not saved, as the close box says.

**Concurrent terminal changes.** None; terminal tabs belong to the window.

### 2.26 External editor (TERM; CMD-030, TYPE-015)

The phases 3-4 design moved TYPE-015 (Ctrl+O in the typing box) to phase 6, where the terminal tabs are built (its TYPE-015 row; `desktop/PLAN.md:305`); this section maps it with CMD-030 (section 1, last block).

**Triggers.** Ctrl+O in the typing box, which TERM binds with `bindKey("composer", "ctrl+o", …)` (3-4 design §1.8 keeps the key reserved for it), and the palette's "Open External Editor" (`runAction("editor.open")`). The palette shows the entry when `$VISUAL` or `$EDITOR` is set in the app's login-shell environment (the terminal checks `$EDITOR` only, `internal/ui/dialog/commands.go:570-577`).

**Steps.**

1. Busy → warning "Agent is working, please wait..." (`internal/ui/model/keypress.go:366-371`; `dialog_actions.go:341-345`).
2. The text is the typing box's text, `composer.drafts[sessions.currentId || ""].text` (P3.7), with "!" in front while `composer.mode` is `"bang"` (`keypress.go:372-375`).
3. `editor_prepare(text, line, column)` (6.4, `editor.rs`) writes it to a new temporary file `msg_<random>.md` (mode 0600) and builds the command: `$VISUAL`, else `$EDITOR`, else `nano` (PLAN section 7 names `$VISUAL`; the terminal's helper reads `$EDITOR` and falls back to `nano`, `github.com/charmbracelet/x/editor@v0.2.0/editor.go:15-17`, `:126-140`). The value is split on spaces. The caret's line and column become the editor's position argument exactly as the helper does: `vi`, `vim`, `nvim` → `+call cursor(<line>,<column>)`; `nano` → `+<line>,<column>`; `emacs`, `kak` → `+<line>:<column>`; `gedit` → `+<line>`; `code` → `--goto <file>:<line>:<column>`; `hx` → `<file>:<line>:<column>`; the file path last unless the option holds it (`editor.go:61-86`, `:103-124`).
4. `openTerminal({cwd, argv, title: "Editor"})` (7.4) runs the command in a new terminal tab titled "Editor" (inferred), shown at once.
5. When the editor exits, or its tab is closed, `editor_finish(path)` reads and deletes the file, as the terminal does when its editor returns (`internal/ui/model/editor.go:164-174`); the text, trimmed of surrounding white space, replaces the typing box's text through `setText(text)` (P3.7), and an emptied file clears it (`internal/ui/model/editor.go:181-193`). The tab closes and the window shows the chat again. A failure shows its error.

Editors that return at once (for example `code` without `--wait`) give the text back unchanged, as in the terminal (9.2).

### 2.27 Phone sharing and the launcher (SHARE; REM-001 to REM-019, CMD-042)

The engine runs the share (engine design 5.7). The window starts and stops it, shows its state, tells it what the window shows, and runs the three actions that change window state.

**Starting (REM-001, CMD-042).** The palette's "Remote Control" (aliases rc, phone; a typed "/remote" filters to it; `commands.go:627`) does one of these (`internal/ui/model/remote.go:163-202`):

- sharing is on → the status sheet;
- the setup sheet is open → bring it to the front;
- a start is in flight → nothing;
- otherwise → the setup sheet, showing "Checking Tailscale…" and "Looking for a private connection to Pocket Agents." (`internal/ui/dialog/remotecontrol/setup.go:98-101`), and `POST …/remote/start` `{launch_id}` → `proto.RemoteStatus{state, host, port, phones, setup{state, title, detail, action, button}, error}`.

The terminal's client-mode refusal "Remote Control needs Crush running in this terminal, not as a client" (`remote.go:176-179`) does not apply: the engine runs the share.

**Start results.**

- `state: "on"`: the setup sheet closes, the mode mark shows the REMOTE badge, `setBadge("remote", "REMOTE")` (P3.7, SIDE-025; the terminal's `m.status.SetRemote(true)`, `remote.go:235`, draws "REMOTE", `internal/ui/styles/quickstyle_dialogs.go:109`), and the status sheet opens, unless the start was quiet (REM-009).
- `state: "setup"`: the setup sheet shows `setup.title` and `setup.detail` as sent (texts from `internal/remote/setup.go:62-96`) and checks again every 2 s, by calling the start route again, while it is open and the state is not `unsupported` (`remote.go:227-253`).
- `state: "off"` with `error`: title "Sharing could not start", detail "<error>. Choose Check again to retry." (`remote.go:218`); no automatic re-check.

**Setup sheet (REM-002 to REM-004).** Title "Remote Control" (`setup.go:122`).

- Buttons: while checking, "Cancel" only; with no `action`, "Check again" and "Cancel"; with an action, "<button>", "Check again" and "Cancel" (`setup.go:45-53`). A new state selects the first button again (`setup.go:38-43`).
- Keys: Tab, → and ↓ go to the next button and Shift+Tab, ← and ↑ to the previous, wrapping; Enter and Space press it; Esc cancels (`setup.go:66-80`). Help "←/→ choose · enter confirm · esc cancel" (`setup.go:124`).
- Mouse: one click presses a button (`setup.go:81-90`). Buttons stack when they do not fit in a row (`setup.go:116-119`).
- Cancel closes the sheet and drops a start in flight; a share that comes up after the cancel is stopped with `POST …/remote/stop` (`remote.go:204-210`, `:255-258`).
- Only the explicit button runs a step (`setup.go:22-23`; `remote.go:264-270`).

**Setup step (REM-005).** The step the sheet shows runs as `crush remote setup <action>` in a new terminal tab titled "Remote setup" (inferred), through TERM's `openTerminal({cwd, argv: ["crush", "remote", "setup", <action>], title: "Remote setup"})` (7.4; `pty_spawn` runs an `argv[0]` of `"crush"` as the binary P3.1's `crush_bin::resolve` found, 6.4), so the system's password prompt and Tailscale's sign-in link and QR code reach the owner (PLAN section 7; `remote.go:260-283`). Nothing from it is captured or logged. When it exits: success → check again; failure → title "Setup did not finish", detail "The step was cancelled or failed. You can retry it, choose Check again if you finished elsewhere, or cancel setup." (`remote.go:285-300`).

**Status sheet (REM-006 to REM-008).** Title "Remote Control"; "On. Your phone can see this chat."; the rows "Computer" "<host> · port <port>" and "Phones" with the phones joined by ", " or "none yet"; "Open Pocket Agents › PC on your phone."; "Only your own Tailscale devices can connect."; buttons "Turn off" and "Keep on", with "Keep on" selected first (`internal/ui/dialog/remotecontrol/remote.go:90-128`).

- Keys: ←, →, Tab and Shift+Tab switch; Enter and Space press; Esc closes without stopping (`remotecontrol/remote.go:72-88`). The desktop adds clicks (2.0 rule 6; the terminal has none here).
- "Turn off" → `POST …/remote/stop`, the badge goes (`setBadge("remote", null)`), info "Remote Control is off" (`internal/ui/model/remote.go:315-325`).
- The phone list stays current from `remote_status` events (REM-008); long values end with "…" (`remotecontrol/remote.go:97-102`).
- A newly connected phone → info "Remote Control: <phone> connected" (`internal/ui/model/remote.go:327-337`), from `remote_status`.

**Quiet start (REM-009).** A window the launcher opened (REM-017) starts sharing at once, with no sheet. A failure shows the error "Remote Control: <error>" (`remote.go:204-217`; `internal/ui/model/ui.go:564-576`). Each launch ID is sent by the window it was given to, and later windows never reuse it (the terminal clears `CRUSH_REMOTE_LAUNCH` after reading it, REM-009).

**Presence (REM-011).** `POST …/remote/presence` `{session_id, focused, plan}` whenever the shown chat, the window's focus or plan mode changes, and only then (`internal/ui/model/remote.go:135-143`, `:339-353`). `plan` is `composer.mode === "plan"` (P3.7).

**Phone actions the window runs (REM-010).** `remote_action` events with `kind` "new_chat_send", "yolo" or "mode"; every other kind runs in the engine (engine design 5.7, "Who runs each action"). The window answers each with `POST …/remote/actions/{action_id}/done` within 25 s:

- `new_chat_send`: the window creates its new chat as typing would (`runAction("chat.new")`, then `ensureSession()`, P3.9 and P3.3), shows it, and answers `{session_id}`; the engine then sends the text and the uploads.
- `yolo`: `runAction("yolo.toggle")` (P3.7), which also leaves plan mode (`input_mode.go:43-54`, `:126-134`), then `{}`.
- `mode`: `runAction("mode.cycle")` (P3.7), then `{}`; while the window is busy it answers `{error: "Agent is busy, please wait..."}` (`internal/ui/model/remote.go:407-410`, `:434`).
- `session_id` not the shown chat → `{error: "the Crush window switched to another chat"}` (`remote.go:375-377`).

**Engine-side rows (REM-012 to REM-014, REM-018, REM-019).** Listening on the Tailscale IPv4 address only, on ports 7373 to 7392, for the same Tailscale user (`internal/remote/tailnet.go:253-267`); the phone's event stream (`internal/remote/remote.go:279-324`) with a ping every 15 s (`:303-316`); the 64 MiB upload cap, the refusal of browser origins and non-JSON posts, 409 for chats no longer shown, and secure entry never reaching the phone (REM-014); step details and labels (REM-018, REM-019). The desktop adds nothing to these; the checks drive the stand-in phone (section 8) against a share a desktop window owns.

**Launcher target (REM-015).** Settings row "Phone opens new chats in" with the choices "Terminal" (default) and "Desktop app" (PLAN section 7), saved as `options.remote_launch_target` (G-17). The launcher service (`crush remote launcher`) reads it at each launch (G-17): "desktop" runs `crush-desktop --project <home>` with `CRUSH_REMOTE_LAUNCH=<id>`; "terminal" keeps today's Konsole command or `CRUSH_LAUNCH_COMMAND` (`internal/remote/launcher.go:77-101`).

**Launcher password (REM-016).** Settings row "Launcher password" shows "Set" or "Not set" (SET's `remote_password_exists()`, 6.4: whether the file `remote-password` exists beside the global data config: `internal/cmd/remote.go:20-23`; `internal/config/paths.go:65-71`). "Set password…" opens a sheet with two masked fields, "New password" and "Type it again" (the CLI's prompts, `internal/cmd/remote.go:65-75`). Saving runs `crush remote password --stdin` (G-20) through SHARE's command `remote_password_set(first, second)` (6.4, `remote.rs`), which writes both on the child's standard input and never puts them in arguments, the environment, a log or the probe; the webview clears both fields after the call (as 2.7). Texts come from the CLI: "the two passwords don't match; nothing changed" (`internal/cmd/remote.go:73-74`), "the password needs at least 8 characters" (`internal/remote/launcher.go:59-62`), and on success "Password set. The phone asks for it before it opens a new Crush window here." (`internal/cmd/remote.go:79`). The hash and the 5-wrong-tries lockout stay in the launcher (`launcher.go:41-45`, `:133-155`).

**Launcher window (REM-017).** `crush-desktop --project DIR` with `CRUSH_REMOTE_LAUNCH=<id>` in its environment. If the app already runs, the new process hands the request to it and exits; the hand-off carries only arguments, so the new process turns the variable into `--launch-id <id>` first: it re-executes itself with that argument and without the variable, before Tauri starts (S5's `main.rs`, 7.1), and `tauri-plugin-single-instance` 2.5.4 then hands its arguments to `windows::handoff`. The running app opens a window for DIR, or focuses the one that shows it and sends it `window-request` with the launch ID (`window_open`'s rule, 6.4). If the app was not running, it starts with that window. Either way the window starts sharing quietly with that `launch_id` (REM-009), and the share reports it, which is how the phone finds the window (engine design 5.7 Rules, "Launcher"). A focused window always calls `POST …/remote/start` `{launch_id}` again with the new ID, even while it already shares and even while a start of its own is in flight: the engine runs one start per workspace at a time (`internal/backend/share.go:256-257`; the lock is in each workspace's share state, `share.go:199-200`, `internal/backend/workspace.go:110`), and a start from the owner of a running share sets the share's reported launch to the new ID and answers `on` (G-26b, 9.1).

**State.** Per window: share state (`off`, `starting`, `setup`, `on`), the last `RemoteStatus`, known phones, last presence sent, the launch ID (used once).

**Concurrent terminal changes.** One share per app (engine design 5.7): the terminal app may share its own window at the same time. Each app sees only its own share.

### 2.28 Log viewer, Usage report, About and version, crash guard and notice, debug overlay (SYS; MISC-002, MISC-016 to MISC-018, MISC-023, MISC-025, START-020)

**Log viewer (MISC-016).** Palette "Logs" (2.2) opens a wide sheet titled "Logs" (inferred) on the project's log, `<data folder>/logs/crush.log` (`internal/cmd/logs.go:54-58` reads the folder from `options.data_directory`). `logs.rs` takes the folder from the window's `Ensured.data_dir` (3-4 design §1.2.3), the same folder, since the engine runs one per data folder (engine design 5.2):

- The last 1,000 lines (`logs.go:21`), read by `logs_tail(1000)` (6.4). Each JSON line shows its time, level and message, then the other keys sorted by name, with `source` as "<file>:<line>" (`logs.go:167-215`); lines that are not JSON are skipped (`logs.go:168-171`).
- Following is on: new lines are added as they are written (`logs_follow(onLines)`, 6.4; `logs.go:78-131`).
- No file: "Looks like you are not in a crush project. No logs found." (`logs.go:59-63`). When 1,000 lines were shown: "Showing last 1000 lines. Full logs available at: <path>" (`logs.go:104-107`).
- Keys: ↑/↓, PgUp/PgDn, Home/End; F turns following on or off (inferred key); Esc closes. The wheel scrolls; the text can be selected and copied.

**Usage report (MISC-017).** Palette "Usage report" runs `crush stats --cwd <project>` through SYS's command `crush_stats` (6.4, `crushinfo.rs`). That command writes the HTML report and opens it in the browser itself, with `github.com/pkg/browser` (`internal/cmd/stats.go:19`, `:147-157`); test runs put S5's recording `xdg-open` first on PATH (8.4). The window shows its output lines as notices: "Stats generated: <path>" as info, or "Could not open browser: <err>" as a warning (`stats.go:152-156`).

**About (MISC-018, MISC-023).** Palette "About Crush" (2.2) opens a sheet titled "About Crush Desktop" (inferred):

- the CRUSH AI wordmark;
- "Crush Desktop <version>": `app_info().version`, the package version (`desktop/src-tauri/src/commands.rs:36`) that `crush-desktop --version` prints (`desktop/src-tauri/src/main.rs:29-31`);
- "Crush <version>", with its commit and platform, from `GET /v1/version` → `{version, commit, build_id, go_version, platform, protocol}` (`internal/proto/version.go:4-12`);
- Crush's folders from `crush dirs --cwd <project>`, run by SYS's command `crush_dirs` (6.4); without a terminal it prints one folder per line in the order Config, Data, then Project folders (`internal/cmd/dirs.go:25-58`), which the sheet labels "Config", "Data" and "Project" (`dirs.go:82-91`), followed by "Configs merge from top to bottom" (`dirs.go:79`);
- the log file and the crash report folder.

Each folder opens in the file manager on click (`openLink(path)`, P3.4). `crush-desktop --version` prints "crush-desktop <version>" (`main.rs:29-31`).

**Crash guard and notice (START-020, MISC-025).** PLAN section 7: a crash notice with "Open report" and "Restart". The phases 3-4 design moved START-020 to phase 6 (its §2.10), so the guard and the notice are both SYS's, in `crash.rs` behind `crash_pending` and `crash_restart` (6.4).

- Reports go to the terminal's crash folder, `$XDG_STATE_HOME/crush/crashes` (else `~/.local/state/crush/crashes`), mode 0700 (`internal/log/crash.go:12-24`), as `crash-desktop-<YYYYMMDD-HHMMSS>-<pid>.log` with mode 0600 (the terminal's name and mode, `internal/cmd/crash_output.go:35-41`, with "-desktop" added).
- At start, `crash::install()` (S5 calls it first in `main.rs`, 7.1) deletes empty reports older than 24 hours from that folder, as the terminal does (`internal/cmd/crash_output.go:26-34`), then sets the panic hook.
- A Rust panic writes its message and backtrace there, then the app ends; the notice shows at the next start (`crash_pending()`). A crashed web view writes a report for that window, and the Rust side opens a fresh web view for the window that shows the notice at once.
- Release builds keep symbol names for these backtraces (`desktop/PLAN.md:272`: deferred from phase 0 to phase 6): `[profile.release]` changes from `strip = true` (`desktop/src-tauri/Cargo.toml:28-29`) to `strip = false` with `debug = "line-tables-only"`.
- Text: title "Crush Desktop crashed" (inferred); body "If you'd like to report it, please open the report and open an issue at https://github.com/charmbracelet/crush/issues/new?template=bug.yml". This is the terminal's text (`internal/cmd/root.go:156`) without its first two sentences: "Crush crashed." becomes the title, and the desktop sends no metrics (SET-032), so "If metrics are enabled, we were notified about it." would be false (9.4, D12).
- Buttons: "Open report" opens the report file (`openLink(path)`, P3.4); "Restart" (`crash_restart()`) restarts the app (after a panic) or reloads the window (after a web view crash).

**Debug overlay (MISC-002).** With `CRUSH_UI_DEBUG=true` in the app's environment (`app_info().ui_debug`, 3-4 design §1.2.9; S5 mounts SYS's `system/DebugOverlay.svelte` then, 7.1), a small square near the top left changes to a random color on every redraw, above everything and ignoring the mouse (`internal/ui/model/draw.go:106-114`: 4 by 2 cells at 4, 1). On the desktop, a redraw is an animation frame in which the page changed: a `MutationObserver` schedules one color change per frame. Size and place are in look 5.23.

## 3. Themes (Q9)

The desktop keeps the terminal's theme system for colors: the same 55 palette roles, the same theme files in the same folder, the same editor, and the terminal's own Go functions to read, resolve and write them (G-2). It adds two built-ins to the shared list (G-2): `scholars-edition`, the desktop's default, and `charcoal-amber`. The desktop never parses or writes a theme file itself.

The **theme choice is not shared** (coordinator decision, `desktop/PLAN.md:377`): the desktop keeps its own `theme` key in its own settings file (`settings.json`, P3.2, 3-4 design §1.2.8), and choosing a theme in one app never changes the other. The desktop never reads or writes the terminal's `options.tui.active_theme`. With no theme chosen, the desktop shows `scholars-edition`.

### 3.1 Roles and tokens

**Rules.**

1. Every token below is a CSS custom property on `:root`. THM sets all of them in one frame from a resolved theme (`src/lib/themes/apply.js`): G-2's `resolved` object (every role as `#rrggbb`) and its `syntax` and `markdown` objects.
2. A **direct** token copies one role or one resolved color. A **derived** token mixes two roles in sRGB: `mix(a, b, t)` is, per channel, `round(b + (a − b) × t)`. A **picked** token takes whichever of two roles contrasts more with a third.
3. **Exact built-in values.** The two desktop built-ins carry an exact table for their derived tokens; the Scholar's edition table is the app.css value of each token. A derived token takes its table value while every role it is derived from still holds the root built-in's value, else its formula. So the approved picture stays exact, and editing one role never shifts tokens that do not depend on it. The terminal uses the same idea for untouched built-ins (`internal/ui/styles/palette.go:512-522`).
4. Tokens used for text, and marks that carry meaning, pass the contrast rule (3.6) before use. The adjusted value is used on screen only and never written anywhere.
5. The non-color tokens (`--zoom`, `--px`, the faces `--face-*`, the type roles `--t-*` including the phases 3-4 design's `--t-h1` to `--t-h3` and `--t-code`, `--track-label`, `--track-path`; `desktop/src/app.css:8-73`, `:107-111`; 3-4 design §4.0) are not themed. Every theme uses the same faces, sizes and zoom.

**The app.css color tokens** (`desktop/src/app.css:12-41`), with their source and the Scholar's edition value:

| Token | Source | Scholar's edition | Used for |
|---|---|---|---|
| `--paper` | `bg_base` | `#f9f8ef` | window and sheet background |
| `--paper-raised` | `bg_least_visible` | `#fbfaf2` | raised surfaces at rest: the New chat button, the slot box (3-4 design §4.9) |
| `--band` | `bg_less_visible` | `#f3f1e6` | selected rows in lists and sheets; code blocks (through `--code-bg`) |
| `--ink` | `fg_base` | `#0f0f0f` | text |
| `--slate` | `fg_more_subtle` | `#353533` | secondary text: times, agent names, placeholders, key hints; disabled and offline marks |
| `--link` | `info` | `#14323f` | links; the REMOTE badge |
| `--rule-frame` | `mix(fg_base, bg_base, 0.338)` | `#aaa9a3` | column frames, sheet frames |
| `--rule` | `separator` | `#b0afa9` | rules between rows, help separators, section rules |
| `--rule-faint` | `mix(fg_base, bg_base, 0.18)` | `#cfcec7` | band edges, boxed blocks |
| `--rule-control` | `mix(fg_base, bg_base, 0.45)` | `#908f89` | outlines of fields and buttons; scrollbar thumbs |
| `--rule-table` | `mix(fg_base, bg_base, 0.545)` | `#7a7973` | outer table rules |
| `--rule-table-inner` | same as `--rule-frame` | `#aaa9a3` | inner table rules |
| `--mark` | `fg_most_subtle` | `#6f6e6f` | + corner marks; help-footer descriptions |
| `--figure` | `fg_subtle` | `#383634` | drawn figures: branch tree lines, side-panel figures |
| `--vermilion` | `primary` | `#e52b0e` | the one accent: focus outline, the shown row's edge, rail key borders |
| `--vermilion-fill` | `button` | `#e34524` | the primary button fill (Send); icons only, never text (3.6) |
| `--vermilion-wash` | `mix(primary, bg_base, 0.04)` | `#faf5f0` | rail key background |
| `--vermilion-ink` | `accent` | `#d9290d` | accent letters: rail keys, the update badge |
| `--amber` | `warning` | `#e0a523` | the warning icon's fill; its "!" is drawn in `--on-amber` (3.6) |
| `--meter` | `mix(fg_base, bg_base, 0.77)` | `#454640` | meter fill |
| `--meter-track` | `bg_most_visible` | `#edeee8` | meter track |
| `--meter-edge` | `mix(fg_base, bg_base, 0.25)` | `#bdbdb9` | meter outline |

The derived values are exact for the Scholar's edition through rule 3. The formulas alone give `--rule-frame` and `--rule-faint` exactly and land close for the others (for example `#797975` for `--rule-table` `#7a7973`, `#f8f0e6` for `--vermilion-wash` `#faf5f0`), which is why rule 3 exists.

**The tokens the phases 3-4 design adds** (3-4 design §4.0, "phases 5-6 themes set the same names"), with the source each takes in every theme. Their Scholar's edition values are 3-4 design §4.0's, unchanged, as S3 wrote them into `desktop/src/app.css:75-105`:

| Token | Source | Scholar's edition | Used for (3-4 design) |
|---|---|---|---|
| `--error-ink` | `error` | `#a1261b` | error texts and tags, exit codes, failed counts, removed counts; on this design's surfaces also field errors and failed states (3-4 design §4.11) |
| `--amber-ink` | `warning_subtle` | `#8a5a00` | warning texts ("Refresh failed: …", WARN counts) |
| `--diff-add-ink` | `diff_insert_fg` | `#2f5a1e` | "+N" counts, added squares |
| `--diff-add-bg` | `diff_insert_code_bg` | `#eaf0dc` | added lines |
| `--diff-add-gutter` | `diff_insert_gutter_bg` | `#dce6c8` | added line numbers |
| `--diff-del-bg` | `diff_delete_code_bg` | `#f6e6df` | removed lines |
| `--diff-del-gutter` | `diff_delete_gutter_bg` | `#efd5ca` | removed line numbers |
| `--selection` | `mix(warning, bg_base, 0.35)`, exact `#e6d9a8` | `#e6d9a8` | text selection everywhere, terminal tabs included (9.4, D14); selected text stays in its own color |
| `--skill-tint` | `mix(plan, bg_base, 0.12)`, exact `#ece6d2` | `#ece6d2` | `$skill` words in the field (deferred rows TYPE-031 to TYPE-034) |
| `--code-inline` | G-2's `markdown.code`: the terminal's inline-code color, `destructive` unless the theme overrides it (`internal/ui/styles/quickstyle_markdown.go:116`; Gruvbox's orange, `internal/ui/styles/themes.go:240-246`) | `#6a3d12` (the edition's override, 3.7) | inline code |
| `--code-bg` | not themed: `var(--band)` | `--band` | code blocks and tool bodies |
| `--scrim` | `bg_base` at alpha 0.92 | `rgb(249 248 239 / 0.92)` | the veil behind sheets and the image viewer |
| `--hl-keyword` | G-2's `syntax.keyword` | `#0f0f0f` (the 3-4 value `var(--ink)`) | keywords (weight 600 is P3.4's, not themed) |
| `--hl-string` | `syntax.literal_string` | `#3f5f22` | strings |
| `--hl-number` | `syntax.literal_number` | `#7a4a08` | numbers |
| `--hl-comment` | `syntax.comment` | `#5f5e5a` | comments (italic is P3.4's) |
| `--hl-function` | `syntax.name_function` | `#14323f` (the 3-4 value `var(--link)`) | function names |
| `--hl-type` | `syntax.keyword_type` | `#3e4f7a` | types |
| `--hl-attr` | `syntax.name_attribute` | `#6a3d12` | attributes |
| `--hl-meta` | `syntax.comment_preproc` | `#5f5e5a` | preprocessor and meta lines |
| `--ansi-0` … `--ansi-15` | `ansi_black` … `ansi_bright_white`, in order (`internal/ui/styles/quickstyle.go:681-686`) | the 16 values of 3-4 design §4.0 (3.7) | program output in tool rows (phase 3) and terminal tabs (2.25) |

**The tokens S5 adds** (7.1; used by this design's surfaces):

| Token | Source | Scholar's edition | Used for |
|---|---|---|---|
| `--on-amber` | picked: `fg_base` or `bg_base`, whichever contrasts more with `warning` | `#0f0f0f` | the "!" stroke and dot of the warning icon (3.6) |
| `--choice` | `secondary` | `#14323f` | selected choice marks (radio and check on), the text caret (`caret-color`) |
| `--on-accent` | `on_primary` | `#f9f8ef` | icons on `--vermilion-fill` (the Send icon, S5's change to `composer/Composer.svelte`); text on `--info-strong` tags |
| `--keyword` | `keyword` | `#7c4a1e` | key names in sign-in texts ("enter"), as the terminal (`internal/ui/styles/quickstyle_dialogs.go:75`) |
| `--attention` | `attention` | `#a64b00` | the WARN tag fill (with `--paper` text, as `quickstyle.go:399`); the needs-authentication dot |
| `--busy` | `busy` | `#8a5a00` | the working mark's arc, the helpers mark (look 5.2) |
| `--info-strong` | `info_more_subtle` | `#2f5d73` | the helper tag fill (with `--on-accent` text, as `quickstyle.go:417`) |
| `--info-soft` | `info_most_subtle` | `#4b6b78` | secondary row details: job command and pid, MCP tool names |
| `--plan` | `plan` | `#2f5d73` | the On and Toggleable squares of Toggle skills (look 5.9) |
| `--control-rest` | `button_subtle` | `#fbfaf2` | a sheet button at rest |
| `--control-hover` | `button_hovered` | `#f3f1e6` | a hovered button or row |
| `--control-disabled` | `button_inactive` | `#edeee8` | a disabled button |
| `--disabled-band` | `bg_most_visible` | `#edeee8` | a disabled row that is selected (pickers) |

Pass 1's other tokens are gone: `--select-paper` and `--select-ink` (now `--selection`, with text in its own color), `--danger` (destructive labels are ink, as the phases 3-4 design's Deny button, D24; error tags and the error dot use `--error-ink`), `--error`, `--warning`, `--success`, `--success-mid`, `--success-soft` (success is an ink check, 3-4 design §4.11), `--yolo` and `--plan-soft` (phase 3 draws the YOLO and plan marks without them, 3-4 design §4.9), `--md-*` (Markdown takes `--ink` headings, `--link` links, `--code-inline` and `--rule`, 3-4 design §4.1), `--code-<class>` (now the eight `--hl-*`), `--review-*` (the Changes sheet is the phases 3-4 design's) and the `--diff-insert-*`/`--diff-delete-*` names (now `--diff-add-*`/`--diff-del-*`).

A theme is **dark** when the Rec. 709 luma of `bg_base` is below half (`internal/ui/dialog/changes.go:575-581`); the engine returns it as `dark`. The desktop sets `color-scheme: dark` or `light` on `:root` with it, so scrollbars, form controls and WebKitGTK's own pieces follow (THEME-006).

**Roles in the terminal's order** (`internal/ui/styles/palette.go:47-104`; the editor lists them in this order). "Seen in" names the screen where `w:thm.field[<role>]` confirms the change; a role no desktop surface draws is checked in the saved file only.

| # | Role | Desktop tokens | The terminal uses it for | Seen in |
|---|---|---|---|---|
| 1 | `primary` | `--vermilion`, `--vermilion-wash` | selection, focused user mark, shell prompt, question tag, tab borders (`quickstyle.go:505-510`, `:526`, `:657`, `:725`) | focus outline of the Themes sheet's filter |
| 2 | `secondary` | `--choice` | caret, textarea selection, selected choices, dialog spinner (`quickstyle.go:222`, `:239`, `:507-515`; `quickstyle_dialogs.go:54`) | a selected radio in a question form |
| 3 | `accent` | `--vermilion-ink` | prompt mark, image links, update badge (`quickstyle.go:212`, `:485`; `quickstyle_dialogs.go:116`) | rail key letters in a chat with a tool run |
| 4 | `keyword` | `--keyword` | sign-in "enter", 5-hour limit, reserved words (`quickstyle_dialogs.go:75`; `quickstyle.go:621`) | "enter" on the sign-in sheet |
| 5 | `fg_base` | `--ink`, the derived rules and meters, `--on-amber` (picked) | all body text (`quickstyle.go:175`) | sheet titles and rows |
| 6 | `fg_subtle` | `--figure` | hints, error details, shell output, radio labels (`quickstyle.go:397`, `:518-520`, `:697`) | branch tree lines in the left list |
| 7 | `fg_more_subtle` | `--slate` | muted text, help keys, paths, disabled dots (`quickstyle.go:176`, `:262`, `:589`) | the time column of the Sessions sheet |
| 8 | `fg_most_subtle` | `--mark` | placeholders, help descriptions, section titles (`quickstyle.go:177`, `:211`, `:263`, `:571`) | + corner marks of a sheet |
| 9 | `bg_base` | `--paper`, `--scrim`, the derived tokens | background (`quickstyle.go:200`) | window background |
| 10 | `bg_most_visible` | `--meter-track`, `--disabled-band` | offline dots, disabled selected rows (`quickstyle.go:331`, `:583`) | a selected dimmed file in the Add Image picker |
| 11 | `bg_less_visible` | `--band` (and so `--code-bg`) | code blocks (`quickstyle_markdown.go:122`, `:205-207`) | the selected row of any list sheet |
| 12 | `bg_least_visible` | `--paper-raised` | composer and user message band (`quickstyle.go:229`, `:656-658`) | the New chat button |
| 13 | `on_primary` | `--on-accent` | text on primary and error tags (`quickstyle.go:396`, `:469`, `:725`) | the Send icon |
| 14 | `separator` | `--rule` | help separators, section lines (`quickstyle.go:264-265`, `:572`) | rules between Sessions rows |
| 15 | `destructive` | `--code-inline` where the theme has no inline-code override | error tags, error dots, exit codes, inline code (`quickstyle.go:396`, `:585`, `:698`; `quickstyle_markdown.go:116`) | inline code in a chat under charmtone-panther |
| 16 | `error` | `--error-ink` | error marks, negative button, deletions (`quickstyle.go:369`, `:473`, `:606`) | an invalid value's error line in the new-theme form |
| 17 | `warning` | `--amber`; `--selection` (derived); `--on-amber` is picked against it | the WARNING badge (`quickstyle_dialogs.go:117`) | the side panel's warning icon (`desktop/src/components/SidePanel.svelte:43`) |
| 18 | `warning_subtle` | `--amber-ink` | warning diagnostics, context text, strings (`quickstyle.go:599`, `:620`) | Usage's "Refresh failed: …" line |
| 19 | `attention` | `--attention` | WARN tag, needs-auth dot (`quickstyle.go:399`, `:588`) | the WARN tag on a question form's Confirm page |
| 20 | `busy` | `--busy` | busy dots, YOLO mark and badge (`quickstyle.go:494`, `:584`; `quickstyle_dialogs.go:108`) | a starting MCP server's dot |
| 21 | `info` | `--link` | tool names, links, headings, REMOTE badge (`quickstyle.go:372`; `quickstyle_markdown.go:32`, `:95`; `quickstyle_dialogs.go:109`) | a link in a question description |
| 22 | `info_more_subtle` | `--info-strong` | task tag, other limits (`quickstyle.go:417`, `:623`) | a helper row's tag in the Sub-agents sheet |
| 23 | `info_most_subtle` | `--info-soft` | job actions, todo ratio, MCP tool names (`quickstyle.go:412`, `:424`, `:434`) | the command line of a job row |
| 24 | `success` | none (3-4 design §4.11) | success marks (`quickstyle.go:368`, `:425`) | saved file only |
| 25 | `success_more_subtle` | none | hook labels, create verbs (`quickstyle.go:445`, `:457`) | saved file only |
| 26 | `success_most_subtle` | none | online dots, pending marks, focus bar (`quickstyle.go:586`, `:661`) | saved file only |
| 27 | `diff_insert_fg` | `--diff-add-ink` | insert signs and numbers (`quickstyle.go:294-303`) | a "+N" count in a tool row (phase 3) |
| 28 | `diff_insert_code_bg` | `--diff-add-bg` | insert code band | an added line in the permission prompt's diff (phase 3) |
| 29 | `diff_insert_gutter_bg` | `--diff-add-gutter` | insert number band | same |
| 30 | `diff_delete_fg` | none (removed counts use `--error-ink`, 3-4 design §4.0) | delete signs and numbers (`quickstyle.go:304-313`) | saved file only |
| 31 | `diff_delete_code_bg` | `--diff-del-bg` | delete code band | a removed line in the permission prompt's diff |
| 32 | `diff_delete_gutter_bg` | `--diff-del-gutter` | delete number band | same |
| 33 | `yolo` | none (the YOLO mark is the warning icon, 3-4 design §4.9) | YOLO banner (`quickstyle_dialogs.go:113`) | saved file only |
| 34 | `plan` | `--plan`; `--skill-tint` (derived) | plan mark, plan card, kept skills (`quickstyle.go:489`, `:715`; `quickstyle_dialogs.go:107`, `:139-140`) | the "On" squares in Toggle skills |
| 35 | `plan_more_subtle` | none | plan banner, skill remove (`quickstyle.go:627`; `quickstyle_dialogs.go:111`) | saved file only |
| 36 | `button` | `--vermilion-fill` | focused button (`quickstyle.go:469`) | the Send button |
| 37 | `button_subtle` | `--control-rest` | blurred button (`quickstyle.go:470`) | "Nope" on the Quit sheet |
| 38 | `button_inactive` | `--control-disabled` | inactive button (`quickstyle.go:471`) | the theme editor's Save button while a value is invalid (look 5.14) |
| 39 | `button_hovered` | `--control-hover` | hovered button (`quickstyle.go:472`) | a hovered sheet button |
| 40-55 | `ansi_black` … `ansi_bright_white` | `--ansi-0` … `--ansi-15` | ANSI remapping of command output (`quickstyle.go:678-686`) | a terminal tab running a 16-color test line |

The code colors come from G-2's `syntax` object, which the engine resolves through `styles.LoadTheme`, so each theme's code looks as it does in the terminal, pure built-in overrides included (Charmtone's classic syntax colors, `internal/ui/styles/themes.go:138-154`). The desktop draws eight classes, the ones phase 3's highlighter maps (3-4 design §4.0): `keyword`, `literal_string`, `literal_number`, `comment`, `name_function`, `keyword_type`, `name_attribute`, `comment_preproc`; the terminal styles about twenty-five (`internal/ui/styles/quickstyle_markdown.go:126-208`; 9.4, D17). Bold and italic stay P3.4's fixed choices. Highlighted code reads these properties, never inline colors, so a theme switch recolors code without re-rendering (THEME-005, THEME-007).

**Missing colors.** A desktop token never lacks a value:

1. A role left out of a theme file inherits from the file's `base`, user themes may chain, and the chain ends at charmtone-panther; a loop is refused with "theme inheritance cycle at \"<name>\"" (`palette.go:430-482`). Built-ins define every role except, in the terminal's own built-ins, the diff roles.
2. Unset diff roles are derived by the engine: foregrounds from `success` and `destructive`, backgrounds by blending them over `bg_base` at 20 %, 13 %, 15 % and 8 % in CIELAB (`quickstyle.go:110-157`).
3. Tokens with no role of their own use the formulas in the tables above.
4. Values that are not plain colors are resolved by the engine before the desktop sees them (G-2): Charmtone names through `styles.ColorString` (`palette.go:541-553`); ANSI indices 0-15 to the theme's own `ansi_*` value of that index, or the xterm default when that role is itself an index; 16-231 to the xterm 6×6×6 cube (levels 0, 95, 135, 175, 215, 255); 232-255 to the gray ramp `8 + 10 × (n − 232)`; `#rgb` expanded; an alpha channel dropped, since the terminal cannot draw it either (inferred).
5. A theme the engine cannot load (a broken file, an unknown name) falls back to the desktop default, `scholars-edition`, and the reason goes to the log, as the terminal falls back to its default with a logged warning (`internal/ui/styles/themes.go:534-545`).

### 3.2 Theme list and files

**List (THEME-001, THEME-008).** Both apps list the same themes: the built-ins `charcoal-amber`, `charmtone-panther`, `claude-code`, `graphite`, `gruvbox-dark`, `scholars-edition` and `terminal` (sorted by name, `internal/ui/styles/themes.go:460-501`, with G-2's two additions), then user themes. The Themes sheet shows "New Theme...", then the group "System" with every built-in (an overridden one labelled "<name> (overridden)"), then "User" with user-only themes, shown only when there are any; the filter matches the label; "current" marks the desktop's own theme (`internal/ui/dialog/theme/items.go:65-135`, `:137-198`). With no theme chosen, the desktop marks scholars-edition current; the terminal marks its own choice, or charmtone-panther (`internal/ui/dialog/theme/theme.go:417-423`; 9.4, D13). The old name "charmtone" still means charmtone-panther (`themes.go:477-491`).

**Files (THEME-004).** User themes are JSON files in `themes/` beside the global config file: `$CRUSH_GLOBAL_CONFIG/themes`, else `<config home>/crush/themes` (`internal/ui/styles/theme_file.go:91-101`; `internal/config/paths.go:18-23`). A file holds `base` plus any palette roles, flat (`theme_file.go:16-22`); unknown fields are warned about and ignored, and are dropped when the file is saved again (`theme_file.go:37-86`). Names match `^[a-z0-9]+([-_][a-z0-9]+)*$` (`theme_file.go:157-174`). Files are written with two-space indentation and a final newline, mode 0644, folder 0755 (`theme_file.go:69-86`). A user file named like a built-in shadows it (`themes.go:503-532`).

**Engine routes (G-2).** Global routes, not per workspace, like `/v1/memory`. Each calls the terminal's own function, so files are byte for byte what the terminal writes:

| Route | Does | Terminal function |
|---|---|---|
| `GET /v1/themes` → `{themes: [{name, source, overridden, dark}]}` | the list; `source` is "builtin" or "user" | `styles.ListAllThemes` (`themes.go:572-616`) |
| `GET /v1/themes/{name}` → `ThemeView` | one theme, below | `LoadTheme`, `MergePalette`, `ThemePalette` |
| `POST /v1/themes/resolve` `{base, palette}` → `ThemeView` without `name` | live preview of unsaved colors; 422 with "invalid palette colors: …" | `styles.MergePalette` (`palette.go:368-426`) |
| `PUT /v1/themes/{name}` `{base, palette}` → `ThemeView` | save the editor | `styles.ThemePath`, `SaveThemeFile` (`theme_file.go:69-117`) |
| `POST /v1/themes` `{name, base}` → `ThemeView` | New Theme: validate, export the base's resolved palette (falling back to the default when the base no longer resolves), save | `ValidateThemeName`, `ExportResolvedPalette` (`theme_file.go:176-190`; `themes.go:618-638`; the terminal's sequence `internal/ui/model/theme.go:302-339`) |
| `POST /v1/themes/{name}/rename` `{new_name}` | rename the file; when the terminal's `options.tui.active_theme` names the old theme it follows to the new name, as the terminal's own rename does, so the terminal keeps showing the same colors; the file is moved back when that write fails | `RenameThemeFile` (`theme_file.go:211-229`; `theme.go:344-368`) |
| `DELETE /v1/themes/{name}` | delete a user theme file; no setting changes (3.3) | `DeleteThemeFile` (`theme_file.go:231-246`) |
| `POST /v1/themes/{name}/revert` | remove the user file that shadows a built-in | the terminal's `os.Remove` of `FindThemeFile` (`theme.go:266-297`) |

`ThemeView` is `{name, root, base, source, overridden, pure, dark, revision, palette, resolved, syntax, markdown}`:

- `palette`: the 55 roles as the editor starts them, the user file merged with its base, else the built-in, with Charmtone names kept as names (`internal/ui/dialog/theme/editor.go:194-238`; `palette.go:526-539`).
- `resolved`: the same 55 roles as `#rrggbb` (3.1, "Missing colors" 4).
- `root`: the built-in the chain ends at. `pure`: no user file shadows a built-in of this name.
- `revision`: the theme file's size and modification time, "" for a built-in.
- `syntax` and `markdown`: the resolved code and Markdown colors, from `styles.LoadTheme(name)`; the desktop reads the eight `syntax` classes of 3.1 and `markdown.code`.

Errors are returned as the terminal's texts: "theme name cannot be empty", "use lowercase letters, numbers, hyphens, and underscores only", "\"<name>\" is a built-in theme; edit it instead", "theme \"<name>\" already exists", "unknown theme \"<name>\"; available themes: …" (`theme_file.go:164-190`; `themes.go:525`). After each write the engine publishes `config_changed` to its workspaces; other engines learn of it through G-1 (3.5).

### 3.3 The desktop's theme

- **Setting.** `theme` in the desktop settings file: the theme's name; absent or `""` means `scholars-edition`. THM reads it with `settings_get()` and writes it with `settings_set("theme", name)`; S5 adds the key to `settings::set_key` (7.1). It is never sent to the engine.
- **First frame (LIST-054).** THM keeps a cache in the web view's local storage under `crush-desktop.theme`: `{name, revision, dark, tokens: {"--paper": "#f9f8ef", …}}`. S5's `public/theme-boot.js`, a plain script that `index.html` loads in its `<head>` before the page draws, applies the cached tokens and `color-scheme` (7.1). THM's plugin then confirms with `settings_get()` and `GET /v1/themes/{name}`; when the name or `revision` differs from the cache, it applies the new tokens and rewrites the cache. With no cache, app.css's defaults (the edition) draw the first frame. This is the session host's rule: never show the default before the saved theme (`internal/ui/sessionhost/host.go:116-120`). Local storage belongs to the app's origin, so every window of the app reads the same cache (inferred; `w:thm.first_frame` checks it).
- **Preview (THEME-010, THEME-023).** Moving in the Themes sheet applies the highlighted theme to this window only (`GET /v1/themes/{name}`, kept for the sheet's life). Typing in the editor previews through `POST /v1/themes/resolve`, newest answer wins (the terminal numbers its preview loads the same way, `internal/ui/model/theme.go:154-171`). Esc reverts this window.
- **Switching (THEME-011, SET-009).** Enter or Ctrl+Y: `settings_set("theme", name)`. Every window of this app process takes the theme in the same frame through `settings-changed`, and other desktop processes within 1 s through P3.2's watcher; the info notice "Theme switched to <name>" shows and the sheet closes; on failure the preview is reverted and the error shows (`theme.go:176-201`). The terminal is not touched.
- **Saving colors (THEME-025).** `PUT /v1/themes/{name}`. The theme is applied to every desktop window when it is the desktop's theme, or when none is chosen and it is `scholars-edition` (the terminal's rule with the desktop's default, `theme.go:206-260`); "Theme saved"; the editor closes and the Themes list refreshes. The terminal reloads the file when it shows that theme (G-1, G-3).
- **New theme (THEME-018).** Its base is the desktop's theme, or `scholars-edition` when none is chosen (the terminal uses its own choice or charmtone-panther, `theme.go:302-305`; D13). `POST /v1/themes`, then the desktop's `theme` becomes the new name and the editor opens on it; "Created new theme: <name>".
- **Deleting (THEME-015).** `DELETE /v1/themes/{name}`. When it was the desktop's theme, the desktop clears its `theme` key and shows the edition. The terminal's setting is not touched: when it named the deleted theme, the terminal falls back to its own default the next time it loads it (`themes.go:534-545`).
- **Rename and revert.** As the terminal: "Renamed theme <old> to <new>", "Reverted <name> to its built-in colors"; the Themes sheet reopens on the list (`theme.go:262-368`). A rename sets the desktop's `theme` to the new name when it named the old one; the terminal's setting follows through G-2 (3.2).

### 3.4 Provider themes

The terminal swaps to a provider's theme only while no theme is set and none was picked this session (`internal/ui/model/theme.go:57-70`, `:401-418`); it does so on a model choice and when a chat brings back its model (`internal/ui/model/model_select.go:87`). The only provider theme, "hyper", is the charmtone-panther look under another name (`internal/ui/styles/themes.go:13-38`, `:164-167`). The desktop never swaps themes on a model choice or a chat opening: its default is the edition (Q9), and the provider rule could only pick the terminal's default (D13).

### 3.5 How a theme change reaches the other app

| Change | Made by | Reaches the other side by | Delay |
|---|---|---|---|
| Theme choice in the desktop | `settings_set("theme", …)` | never reaches the terminal (not shared); other windows of the same process at once (`settings-changed`), other desktop processes through P3.2's watcher | same frame; ≤ 1 s |
| Theme choice in the terminal | the terminal's own config write (`theme.go:186`) | never reaches the desktop | — |
| Theme colors, rename, delete, revert, desktop to terminal | G-2 routes | G-1 also watches the themes folder and sends `config_changed`; G-3 makes the terminal reload its theme when that theme's file changed | ≤ 1 s with G-1 and G-3 |
| Theme colors, rename, delete, revert, terminal to desktop | the terminal writes the file in its own process (`theme.go:229-242`) | G-1 sees the folder change and sends `config_changed`; THM re-reads `GET /v1/themes/{its theme}` and applies it when `revision` changed | ≤ 1 s |

Today the file half does not work across apps: the engine sends `config_changed` only after its own writes and has no file watch (`internal/config/load.go:188-217`; `ConfigStaleness` exists but only the `crush_info` tool calls it, `internal/config/store_staleness.go:25-74`), and the terminal loads a theme's colors only at start (`internal/ui/common/common.go:143-156`) and in its own theme actions (switch, save, revert, delete: `internal/ui/model/theme.go:173-201`, `:203-260`, `:262-297`, `:370-399`), never because another app changed the file; `ui.go:490-495` only seeds the active theme key at start. G-1 and G-3 (9.1) are the smallest additions that make it hold.

### 3.6 Readable contrast in every theme

PRODUCT.md asks for readable contrast in every theme. THM checks these pairs whenever it computes tokens, with the WCAG 2.x contrast ratio:

| Token | Against | Minimum | Why |
|---|---|---|---|
| `--ink` | `--paper`, `--paper-raised`, `--band`, `--control-rest`, `--control-hover`, `--selection` | 4.5 | body text, also selected text |
| `--slate` | `--paper`, `--band` | 4.5 | secondary text, also in selected rows |
| `--link`, `--keyword`, `--error-ink`, `--amber-ink`, `--diff-add-ink`, `--info-soft`, `--code-inline` | `--paper` | 4.5 | small text (11 px floor); a selected row draws its colored text in `--ink` (look 5.0), as the terminal draws selected rows in one style (`internal/ui/dialog/mcpservers/toggles.go:258-266`) |
| `--hl-keyword`, `--hl-string`, `--hl-number`, `--hl-comment`, `--hl-function`, `--hl-type`, `--hl-attr`, `--hl-meta`, `--code-inline` | `--code-bg` | 4.5 | code |
| `--mark` | `--paper` | 4.5 | help descriptions |
| `--vermilion-ink` | `--paper`, `--vermilion-wash` | 4.5 | key letters (the app.css rule, `desktop/src/app.css:34`) |
| `--paper` | `--attention` | 4.5 | WARN tag text |
| `--on-accent` | `--info-strong` | 4.5 | tag text |
| `--on-amber` | `--amber` | 4.5 | the warning icon's "!" (coordinator decision: at least 4.5:1, `desktop/PLAN.md:378`) |
| `--on-accent` | `--vermilion-fill` | 3 | icons only; text never sits on this fill |
| `--vermilion`, `--rule-control`, `--choice`, `--busy`, `--attention`, `--plan` | `--paper` | 3 | focus outline, control outlines, marks and dots (WCAG 1.4.11) |

**Adjusting.** When a pair fails, the first token of the pair is mixed in sRGB toward black when the background's relative luminance is above 0.18, else toward white, in steps of 2 %, until every listed background passes. Backgrounds, the decorative rules of 3.1 (`--rule`, `--rule-faint`, `--rule-frame`, `--rule-table*`, `--meter-*`, `--figure`) and the ANSI colors are never adjusted: program output keeps the program's colors, as in the terminal. The adjustment is a pure function in `src/lib/themes/contrast.js`.

**The warning "!" (decision recorded here).** The approved picture draws the "!" dark on the amber triangle (`desktop/.impeccable/mocks/decision/edition.png`), while the built icon draws it in `--paper` (`desktop/src/components/Icon.svelte:60-61`), which is 2.1:1 on `--amber` and fails. The pick: `--on-amber`, whichever of `fg_base` and `bg_base` contrasts more with `warning`. In the edition that is `--ink` `#0f0f0f`, 8.7:1 on `#e0a523`. Why: it passes 4.5:1 with room, it matches the approved picture, it needs no new color (ink is the page's own text color), and in a dark theme the same rule picks the light role, so the glyph stays readable in every theme. S5 changes `Icon.svelte:60-61` to use it (7.1).

**Built-ins.** The Scholar's edition needs no adjustment for any pair in this table (`u:thm.contrast_builtin` fails if one is adjusted); the tightest pairs are `--rule-control` at 3.0:1 and `--vermilion-ink` on `--vermilion-wash` at 4.5:1 (computed with the WCAG formula; inferred arithmetic, the check recomputes it). The terminal's built-ins may be adjusted; for example gruvbox-dark's `fg_most_subtle` `#928374` on `#282828` (4.2:1) becomes slightly lighter for help descriptions. Every theme passes after adjustment.

### 3.7 The two desktop built-ins (for G-2)

G-2 adds two `quickStyleOpts` palettes to `builtinThemes` (`internal/ui/styles/themes.go:460-467`) and one `builtinThemeOverrides` entry, for `scholars-edition`: its inline-code color and the eight syntax classes of 3.1, so the terminal shows the edition's code exactly as the desktop does (the same kind of pure override Charmtone and Gruvbox have, `themes.go:138-154`, `:240-246`). Values marked "sampled" are measured from the approved pictures; values marked "3-4" are the phases 3-4 design's tokens (3-4 design §4.0); the rest are chosen by this design (inferred) and are the coordinator's to change. charcoal-amber's empty diff roles are derived (3.1, "Missing colors" 2).

| Role | `scholars-edition` | `charcoal-amber` |
|---|---|---|
| `primary` | `#e52b0e` (app.css `--vermilion`) | `#f8be43` (sampled, `desktop/.impeccable/build-style2-archive/spec.json:812`, a Git-ignored file in this worktree) |
| `secondary` | `#14323f` | `#c9cbd0` |
| `accent` | `#d9290d` (app.css `--vermilion-ink`) | `#f0b455` (sampled, `spec.json:4415`) |
| `keyword` | `#7c4a1e` | `#e8a77a` |
| `fg_base` | `#0f0f0f` (app.css) | `#f0f0f0` (sampled, `spec.json:56`) |
| `fg_subtle` | `#383634` (app.css `--figure`) | `#b6b8bc` (sampled, `spec.json:60`) |
| `fg_more_subtle` | `#353533` (app.css `--slate`) | `#99999f` (sampled, `spec.json:808`) |
| `fg_most_subtle` | `#6f6e6f` (app.css `--mark`) | `#85858a` (sampled, `spec.json:64`) |
| `bg_base` | `#f9f8ef` (app.css) | `#181818` (sampled, `spec.json:48`) |
| `bg_most_visible` | `#edeee8` (app.css `--meter-track`) | `#3f4042` (sampled, `spec.json:52`) |
| `bg_less_visible` | `#f3f1e6` (app.css `--band`) | `#282828` (sampled, `spec.json:796`) |
| `bg_least_visible` | `#fbfaf2` (app.css `--paper-raised`) | `#1d1e1f` (sampled, `spec.json:154`) |
| `on_primary` | `#f9f8ef` | `#181818` |
| `separator` | `#b0afa9` (app.css `--rule`) | `#3f4042` (sampled) |
| `destructive` | `#a1261b` (3-4 `--error-ink`) | `#f46659` (sampled, `spec.json:107`) |
| `error` | `#a1261b` (3-4 `--error-ink`) | `#f46659` |
| `warning` | `#e0a523` (app.css `--amber`) | `#f6bb41` (sampled, `spec.json:111`) |
| `warning_subtle` | `#8a5a00` (3-4 `--amber-ink`) | `#e0b45c` |
| `attention` | `#a64b00` | `#f0b455` |
| `busy` | `#8a5a00` | `#f8be43` |
| `info` | `#14323f` (app.css `--link`) | `#8fb8de` |
| `info_more_subtle` | `#2f5d73` | `#7aa2c8` |
| `info_most_subtle` | `#4b6b78` | `#6b8aa8` |
| `success` | `#3d6b3a` | `#6cc17a` |
| `success_more_subtle` | `#4f7a3f` | `#8fd19b` |
| `success_most_subtle` | `#557a4d` | `#5aa86a` |
| `diff_insert_fg` | `#2f5a1e` (3-4 `--diff-add-ink`) | derived |
| `diff_insert_code_bg` | `#eaf0dc` (3-4 `--diff-add-bg`) | derived |
| `diff_insert_gutter_bg` | `#dce6c8` (3-4 `--diff-add-gutter`) | derived |
| `diff_delete_fg` | `#a1261b` (3-4: removed counts use `--error-ink`) | derived |
| `diff_delete_code_bg` | `#f6e6df` (3-4 `--diff-del-bg`) | derived |
| `diff_delete_gutter_bg` | `#efd5ca` (3-4 `--diff-del-gutter`) | derived |
| `yolo` | `#8a5a00` | `#f8be43` |
| `plan` | `#2f5d73` | `#8cc7c7` |
| `plan_more_subtle` | `#6f8a96` | `#4f7272` |
| `button` | `#e34524` (app.css `--vermilion-fill`) | `#f0b455` |
| `button_subtle` | `#fbfaf2` | `#282828` |
| `button_inactive` | `#edeee8` | `#3f4042` |
| `button_hovered` | `#f3f1e6` | `#5c5e64` (sampled, `spec.json:800`) |
| `ansi_black` … `ansi_white` | `#0f0f0f`, `#a1261b`, `#3f6b22`, `#8a5a00`, `#1f4f8a`, `#7a3b7d`, `#1d6670`, `#6e6d6e` (3-4 `--ansi-0` to `--ansi-7`; `desktop/src/app.css:102-103`) | `#181818`, `#f46659`, `#6cc17a`, `#f6bb41`, `#7aa2c8`, `#c792ea`, `#7fcfcf`, `#b6b8bc` |
| `ansi_bright_black` … `ansi_bright_white` | `#5f5e5a`, `#c2341f`, `#4a7827`, `#946400`, `#2d63b0`, `#924a95`, `#23727c`, `#4a4947` (3-4 `--ansi-8` to `--ansi-15`; `app.css:104-105`) | `#5c5e64`, `#ff8a7a`, `#8fd19b`, `#f8d27a`, `#9cc3e6`, `#ddb3f2`, `#a3e3e3`, `#f4f4f4` (sampled, `spec.json:804`) |

**The edition's override entry:** inline code `#6a3d12`; syntax `keyword` `#0f0f0f`, `literal_string` `#3f5f22`, `literal_number` `#7a4a08`, `comment` `#5f5e5a`, `name_function` `#14323f`, `keyword_type` `#3e4f7a`, `name_attribute` `#6a3d12`, `comment_preproc` `#5f5e5a` (3-4 design §4.0's `--code-inline` and `--hl-*`). A user file that shadows the edition loses the override, as with the terminal's own pure overrides (`pure`, 3.2).

**Inline code in the edition (decision recorded here).** `#6a3d12`, the phases 3-4 design's `--code-inline`, rather than the terminal's default `destructive` red. Why: the approved picture (`desktop/.impeccable/mocks/decision/edition.png`) shows no inline code and no red apart from the vermilion accents; a red would read as an error and compete with the one vermilion accent; this brown is already the phase 3 token and the attribute color of code blocks, so inline code and code blocks agree; and it is 8.6:1 on `--paper` (3-4 design §4.0).

The Scholar's edition's exact table for its derived tokens is the app.css values: `--rule-frame #aaa9a3`, `--rule-faint #cfcec7`, `--rule-control #908f89`, `--rule-table #7a7973`, `--rule-table-inner #aaa9a3`, `--vermilion-wash #faf5f0`, `--meter #454640`, `--meter-edge #bdbdb9` (`desktop/src/app.css:21-41`), plus 3-4 design §4.0's `--selection #e6d9a8` and `--skill-tint #ece6d2` (`app.css:85-86`). charcoal-amber has no exact table; its formulas apply.

### 3.8 Theme checks

| Check | Fails when |
|---|---|
| `u:thm.token_map` | any token's source differs from 3.1; a derived token ignores the exact-table rule (edit `keyword` in scholars-edition and `--rule-frame` must stay `#aaa9a3`; edit `bg_base` and it must follow the formula); a token of 3-4 design §4.0 takes another edition value than that design's |
| `u:thm.contrast_builtin` (THEME-002) | scholars-edition needs an adjustment; any built-in fails a pair after adjustment; an adjustment changes the theme data passed in; `--on-amber` is not `#0f0f0f` in the edition |
| `w:thm.palette_roles` (THEME-002) | the editor does not list 55 rows in the order of 3.1 |
| `w:thm.color_meanings` (THEME-003) | any "Seen in" place of 3.1 shows a color other than its token under charmtone-panther |
| `x:thm.user_files` (THEME-004) | a theme the terminal creates (`ctrl+p`, "Themes", New Theme…) is missing from the desktop's list after 2 s, or the reverse; the desktop's file differs byte for byte from the one the terminal writes for the same palette |
| `w:thm.switch_redraw` (THEME-005) | any visible element keeps an old color one frame after Enter (screenshot pixel probe at 12 points) |
| `w:thm.light_dark` (THEME-006) | `color-scheme` does not follow `dark` for scholars-edition and charmtone-panther |
| `w:thm.syntax_colors` (THEME-007) | a code block's keyword color differs from G-2's `syntax.keyword` for the desktop's theme, or needs a re-render to change |
| `x:thm.apply_sync` (THEME-011) | after Enter in one app's Themes sheet, the other app's colors change within 3 s (the terminal's background read as the SGR of a blank cell with `capture-pane -e`; the desktop's `--paper` from the probe), in either direction, on the same project and on different projects; or a second desktop window of the same app does not take the theme within 1 s |
| `x:thm.editor_save_sync` (THEME-025) | saving a new `bg_base` for a theme both apps show does not recolor the other app within 2 s, both directions |
| `x:thm.theme_not_shared` (SET-009) | a switch in the desktop changes the global data config's `options.tui.active_theme`, or a switch in the terminal changes the desktop settings file's `theme`; or either app's "current" mark moves after the other app switched |
| `w:thm.first_frame` (LIST-054) | with a dark theme chosen and cached, the first screenshot after start shows the edition's `--paper` anywhere, in the first window or in a second window |
| `w:thm.field[<role>]` (THEME-028 to THEME-082) | after setting `#123456` (`#654321` for `bg_*` roles) and saving, the theme file lacks the value; or, for a role with a "Seen in" place in 3.1, that place's token is not the value after 3.6 |

## 4. Settings sync

The phase 5 done-line: a setting changed in one app shows in the other. Settings live in four places, and each has one way to travel. Three things stay per project by decision (coordinator, pass 2 brief): the model, plan mode and YOLO; the checks below show that a change on one project leaves another project alone. The theme choice is not shared at all (3.3).

**Mechanisms.**

- **M1, engine config.** Every write goes through the engine (2.0 rule 5). Config writes are key-level edits of the file under an in-process mutex and a cross-process file lock (`internal/config/store_fields.go:114-170`), so two apps never overwrite each other's keys; the last write of a key wins. After each write the engine reloads and sends `config_changed` `{workspace_id}` to every app on that project (`internal/backend/ops_config.go:18-31`; `internal/backend/config.go:15-35`).
- **M2, other projects.** Most settings are global, but each project has its own engine, and an engine reloads only after its own writes (`internal/config/load.go:188-217`). G-1 adds the missing watch: every engine checks its config files and the themes folder once a second with the staleness snapshot it already keeps (`internal/config/store_staleness.go:25-101`), reloads on a change, and sends `config_changed`.
- **M3, the desktop's reaction.** On `config_changed` every open surface re-reads `GET …/config` (and, for its own lists, the routes it named in section 2) and keeps its selection by id. THM re-reads its theme's colors (3.5).
- **M4, the terminal's reaction.** The terminal's client refreshes its cached config on `config_changed` (`internal/workspace/client_events.go:284-300`; `internal/workspace/client_workspace.go:109-124`). Options it reads on use then take effect at once: `notifications` (`internal/ui/model/notification.go:25-58`), `composer_focus_only` (`internal/ui/model/editor.go:52-56`), `thought_line` (`internal/ui/model/config_write.go:176-184`), `composer_footer` (`internal/ui/model/composer_footer.go:135`), `diff_mode` (`internal/ui/model/dialogs.go:265`). Two are copied at start and never re-read, `compact_mode` (`internal/ui/model/ui.go:517`) and `scrollbar` (`ui.go:439`), and the terminal loads a theme's colors only at start and in its own theme actions (3.5). G-3 makes the terminal re-apply `compact_mode` and `scrollbar` on `config_changed`, reload its theme when that theme's file changed, and set its mode mark from the main agent (G-26).
- **M5, engine state with its own events.** Skills (`skills_event`), MCP state (`mcp_event`), kept skills and chat records (`session`), memory (`memory_status`), helpers (`helper`). Both apps get them at once.
- **M6, files read on use.** `projects.json`, `subagent-instructions.md` and `remote-password` have no event. Every reader opens the file when it needs it: the Open Project sheet and the folder picker on opening (2.5, 2.21), the engine on every agent call and the editor sheet on opening (2.12), the launcher on every password try (`internal/remote/launcher.go:133-155`) and the Settings row when the page opens (2.27). Nothing to sync.
- **M7, the desktop settings file.** P3.2's `settings.json` (3-4 design §1.2.8): desktop-only settings, never read by the terminal. Other desktop processes see a change within 1 s through P3.2's 500 ms watcher, and windows of the same process at once through `settings-changed`. This design adds two keys through S5: `theme` (THM, 3.3) and `list_collapsed` (LST, 2.20). The first-frame theme cache lives in the web view's local storage (3.3).

**Per group.**

| Group | Settings | Store | Desktop → terminal | Terminal → desktop | Two-app check |
|---|---|---|---|---|---|
| Theme choice | the desktop's `theme`; the terminal's `options.tui.active_theme` | desktop settings file (M7); config | not shared (3.3) | not shared | `x:thm.theme_not_shared`, `x:thm.apply_sync` |
| Theme files | theme colors, new, rename, delete, revert | themes folder | G-2, M2 (G-1), G-3 | M2 (G-1), M3 | `x:thm.editor_save_sync`, `x:thm.user_files` |
| Layout | `options.tui.compact_mode` | config | M1, M2, G-3 | M1, M2, M3 | `x:set.compact_mode`, `x:set.toggle_sidebar` |
| Typing | `composer_focus_only`, `thought_line` | config | M1, M2, M4 | M1, M2, M3 | `x:set.composer_focus_only`, `x:set.toggle_composer_focus`, `x:set.thought_line`, `x:set.toggle_thought_line` |
| Typing | `composer_footer` | config | M1, M2, M4 | M1, M2, M3 | phase 3's footer checks; `w:set.composer_footer` here |
| Scrolling, diffs, completions | `scrollbar`, `diff_mode`, `completions.*` | config (no desktop control; JSON only) | — | a hand edit reaches the desktop by M2 and M3; the terminal by M4 and G-3 | `w:set.scrollbar_mode`, `w:set.diff_mode`, `w:set.completions_limits` (honored, one app) |
| Work | `auto_compact_token_limit`, `check_in_minutes` | config | M1, M2; the engine reads them on use | same | `x:set.autocompact_save` |
| Notifications | `options.notifications` | config | M1, M2, M4 | M1, M2, M3 | `x:set.notify_save`, `x:set.notifications_style` |
| Memory | `memory_limit_gb` | config, applied to the shared memory box by the engine | `PUT /v1/memory/limit` (2.12); the terminal's sheet reads the config when it opens | the terminal's sheet saves and applies in its own process until phase 7 (engine design 5.9); the desktop re-reads on M2 | `w:set.memory_outcome` |
| CPU | `agent_cpus_enabled`, `agent_cpus` | config, applied by the engine (G-16) | M1, M2, G-16 | M1, M2; G-16 also re-applies on `config_changed` | `w:set.agent_cpus` |
| Models (per project) | `models.large`, `models.small` (with effort, `service_tier`, `ultracode`, `think`), `recent_models` | config, held per engine | one engine per project: both apps on a project use the same model at once; a choice made on another project does not travel | same | `x:mod.choose`, `x:mod.effort_confirm`, `x:mod.fast_toggle`, `x:mod.ultracode_toggle`, `x:mod.thinking_toggle`: the same-project legs must show the change, the different-project legs must show none |
| Plan mode (per project) | the main agent, `POST …/agent/main` (`internal/agent/coordinator.go:240-250`) | engine state, one per project | both apps on a project send their prompts to the same main agent; G-26 sends `config_changed` after the switch and names the main agent in `GET …/agent`, and the terminal's mode mark follows it (G-3); another project's engine never changes | the same main agent; the desktop's mode mark follows `agent` on its agent-info read (S5's change to `composer.svelte.js`, 7.1) | `x:set.plan_mode_per_project`: after a switch to plan in one app, the other app on the same project shows its plan mark within 2 s (the desktop's mode mark, TYPE-002; the terminal's plan prompt, `internal/ui/model/editor.go:202-204`), and its next prompt reaches the fake AI server with the plan agent's prompt; the app on the other project shows no plan mark, and its prompt carries the coder's |
| YOLO (per project) | the skip-permissions switch | engine state per project, not config | `POST …/permissions/skip`; the terminal re-reads it within its 0.5 s cache life (`internal/ui/model/workspace_cache.go:40-42`, `:316-329`) | G-4 sends `config_changed` after the switch; the desktop re-reads `GET …/permissions/skip` | `x:pal.toggle_yolo`: the same-project legs must show the change, the different-project legs must show none |
| Accounts | `providers.<id>.api_key`, `oauth`, `chatgpt_models`, `grok_models` | config | M1, M2 | M1, M2, M3 | `x:auth.key_save_once`, `x:auth.oauth_saved`, `x:auth.settings_sign_out` |
| MCP | `mcp.<name>.disabled`, repository overrides, Docker MCP | config, project database | M5; G-4 adds `config_changed` after the two toggle routes, which send none today (`internal/backend/ops_config.go:151-169`) | same | `x:ext.mcp_toggle` |
| Skills | enabled skills, kept skills | config, chat record | M5 | M5 | `x:ext.skills_toggle`, `x:ext.kept_list` |
| Phone | `options.remote_launch_target` (G-17) | config | M1, M2; the launcher reads it at each launch | M1, M2, M3 | `w:share.launcher_setting` (with the terminal's launcher service as the reader) |
| Phone | launcher password | `remote-password` file | M6 | M6 | `w:share.launcher_password` |
| Sub-agent instructions | `subagent-instructions.md` | file | M6 | M6 | `x:set.subagent_instructions_save` |
| Saved projects | `projects.json` | file | M6 | M6 | `x:ses.switch_project`, `w:ses.projects_cli_parity` |
| Desktop only | `theme`, `list_collapsed`, phase 3's closed chats (`closed_chats`), and the column widths (`crush.columns`) and zoom (`crush.zoom`) kept in local storage (`desktop/src/lib/columns.js:11`; `desktop/src/lib/zoom.js:9`) | desktop settings file (M7); local storage | — | — | `w:lst.collapse_remembered`, `x:thm.theme_not_shared`; phase 3's and 4's checks |

**Models, plan mode and YOLO are per engine on purpose.** A config store pins the model this instance chose, so a reload caused by another instance's write never switches models under the user (`internal/config/store_fields.go:230-243`); the main agent and the skip-permissions switch live in the engine's memory. With one engine per project, both apps on a project share one model, one main agent and one YOLO switch, and a choice made on another project does not travel, in either app. This is the terminal's own rule, kept.

**What each two-app check does.** Every `x:set.*`, `x:thm.*`, `x:mod.*` and `x:pal.toggle_yolo` check above runs four legs on the same throwaway home (section 8): change in the desktop, see it in the terminal; change in the terminal, see it in the desktop; first with both apps on the same project (`/tmp/home/proj`), then with the terminal on the second project (`/tmp/home/proj2`, one engine each), so M2 is proven too. "See it" means the other app's screen changed (the terminal's tmux screen text or colors; the desktop's probe file and a screenshot), not only the config file. A leg fails when the change does not show within 2 s. For the per-project groups (models, plan mode, YOLO) and the theme choice, the legs that must show no change fail when the other app's screen changes within 3 s.

## 5. Look proposals (Scholar's edition)

One proposal per surface, for the coordinator to decide and record: look choices in phases 3-6 are the coordinator's, each with a note and a picture (`desktop/PLAN.md:425`), made by a Claude Opus 5.5 max helper (`desktop/PLAN.md:343`). Sizes are picture pixels at 12/12 written as numbers N meaning `calc(N * var(--px))`; "1px" means one real pixel. Tokens are section 3's. Type roles are app.css's (`desktop/src/app.css:53-73`, `:107-111`). In this section a bare "5.N" means look 5.N.

### 5.0 Rules for every proposal

- **Language.** Bone paper, ink, one-pixel hairlines, + marks where frames meet, small-caps labels, the vermilion accent only for focus, the shown or selected row's edge, and the Send fill. No rounded corners anywhere in sheets (radius 0), no shadows, no blur, no gradients, no emoji or glyph icons: every icon is a drawn line icon in `components/Icon.svelte`'s style (1.6 stroke at 12/12, `currentColor`). The terminal's glyph marks (◐◓◑◒, ⚙, ●, ✓) become drawn marks (look 5.2); their text stays.
- **Small text.** Any new role for small text uses `max(11px, …)` like `--t-small` (`desktop/src/app.css:60-70`). Proposals name a role, or a face with size/line where no role fits: "`--face-mono` 16/24" means `400 max(11px, calc(16 * var(--px))) / calc(24 * var(--px)) var(--face-mono)`.
- **Text case.** Small-caps labels are uppercase through `text-transform`; the text in the page stays the terminal's ("Computer", "Recent", "Terminals [2]").
- **Colored text on selected rows.** A selected or hovered row (`--band` or `--control-hover`) draws its colored text (status, detail) in `--ink`, as 3.6 requires.
- **States, one vocabulary.** Rest: as drawn. Hover: `--control-hover` fill for rows and buttons (title underline for chat-list-like rows, as `desktop/src/components/ChatList.svelte:323-328`). Focus: the global 1px `--vermilion` outline 2 outside (`desktop/src/app.css:166-167`). Active (pressed, or selected by keyboard): `--band` fill plus a 4 wide `--vermilion` edge at left 3, between 1px `--rule-faint` lines (the shown chat row, `ChatList.svelte:235-252`). Disabled: text `--mark`, fill `--control-disabled`, outline `--rule-faint`; it stays in place (skill rule). Error: 1px `--error-ink` outline on the field and an error line in `--t-small` `--error-ink` starting at the field's left edge, 8 below it (3-4 design §4.11). Empty: one line `--t-body-small` `--slate` at the list's text start, 20 below the filter. Loading: the working mark (5.2) 16 wide, then the text in `--t-small` `--slate`, 10 apart, on the row's center line.
- **Geometry.** Equivalent rows share heights, insets and action slots across groups (skill rule). Scrollbars sit outside the row content with phase 3's chat scrollbar look (CHAT-094: a 6 wide thumb in `--rule-control`, no track), drawn by KIT's `Scrollbar.svelte` (7.1) with SET-006's modes (2.13). A list that scrolls fades only the edge with more content, 16 deep, inside the sheet frame.
- **Motion.** None, except the steps of the working and helpers marks (5.2); both stand still under `prefers-reduced-motion`.

### 5.1 Sheet frame and kit

- **Veil.** S3's `OverlayHost` covers the window behind the front sheet with `--scrim` (3.1; 3-4 design §4.0); no blur. The docked Themes sheet and theme editor have no veil (`{veil: false}`, 5.14, 7.1).
- **Frame.** Background `--paper`. Four 1px `--rule-frame` lines that stop 11 short of each corner, and a + mark with 8 arms at each corner in `--mark` (the page frame's drawing, `desktop/src/components/Frame.svelte:21-22`, `:32-57`; `PlusMark.svelte:20`). Padding 26 left and right, 22 top, 20 bottom. Widths: small 520, standard 760, wide 1040; at most the window minus 24 per side (2.1).
- **Title line.** Title in `--t-project` `--ink`, 38 high. Title-side controls on the same line, right-aligned, bottom-aligned to the title's baseline: small-caps labels in `--t-label` with `--track-label`, 18 apart; the active one `--ink` with a 2 `--ink` underline 5 below the baseline, the others `--slate`.
- **Rule under the title.** `components/Rule.svelte` with `marks="both"`, 18 below the title line, spanning the frame's inner width plus the padding (the chat list's `under-search` rule).
- **Group headings.** `--t-label` uppercase with `--track-label`, `--ink`, 14 above and 13 below (the chat list's day label, `ChatList.svelte:163-172`).
- **Filter field.** Height 50, 1px `--rule-control` outline, padding 0 12 0 11, the search line icon at left, text `--t-field`, placeholder `--slate` (the chat list's search, `ChatList.svelte:120-157`). 17 below the title rule.
- **List rows.** One-line rows 44 high, text start 23, title `--t-item`, info column right-aligned at 24 from the right edge in `--t-small` `--slate`. Two-line rows 64 high: title, then 5 below the detail in `--t-small` `--slate`. No rules between rows; groups are split by headings. Matched filter letters: 1px `--ink` underline 3 below the baseline.
- **Help footer.** A `Rule` with `marks="both"` 14 above; one line 32 high: keys in `--t-agent-mono` `--ink`, descriptions in `--t-small` `--mark`, " · " in `--rule`; trailing pairs drop with "…" (2.1).
- **Buttons.** Height 44, minimum width 112, padding 0 20, 1px `--rule-control` outline, fill `--control-rest`, label `--t-button` `--ink` centered, its shortcut letter underlined 1px, 4 below the baseline. The keyboard-selected button: 1px `--ink` outline and `--band` fill. Destructive labels (Delete, Kill, Stop, Turn off) stay `--ink`, as the phases 3-4 design's Deny button (its D24): the words carry the meaning, and red stays for errors. Rows of buttons have 12 between them and sit 20 above the help footer.
- **Fields.** As the filter field without the icon; a prompt such as "> " is drawn in `--slate`.

States as 5.0.

### 5.2 Marks

Every mark is an `Icon` name (`components/Icon.svelte`: S3's set, 3-4 design §5.1, plus S5's changes, 7.1), drawn at 18×18 (12/12), stroke 1.6, centered on the text's x-height, not the line box.

- **Working** (left list, strip, tray, sheet loading, starting MCP servers): Icon `working` as S5 changes it: a ring r 7.6 in `--rule-control` and over it a quarter arc in `--busy` (`stroke-dasharray` 12 36) whose offset is `-6 * ticker.frame`, so it steps every 100 ms on the wall clock (`frame = floor(now_ms / 100) mod 8`, 2.20; `src/lib/ticker.svelte.js`). Reduced motion: the quarter arc stands still.
- **Background** (the strip's cells; available to P3.9's row state): the same Icon with `--icon-ring` and `--icon-arc` both set to `--mark`; it steps like the working mark (the terminal's muted spinner, 2.20).
- **Helpers mark** (tray, Sub-agents rows): Icon `half-circle` (S3) in `--busy`, turned 90° × `ticker.second`, one quarter per wall-clock second (the terminal's ◐◓◑◒, 2.15).
- **Job mark** (tray, Background Processes rows): Icon `gear` (S3) in `--slate`, phase 3's background mark (CHAT-081).
- **Waiting**: Icon `clock` (`desktop/src/components/Icon.svelte:55-57`) in `--slate`.
- **Unread ("Done")**: Icon `check` (S3) in `--ink`, phase 3's done mark (CHAT-047; the terminal's `✓`, 2.20).
- **MCP states** (2.9): Icon `dot` (S3) in `--ink` (connected), `--attention` (needs authentication) or `--slate` (disabled); Icon `offline` (S3) in `--slate`; Icon `cross` (S3) in `--error-ink` (error); Icon `working` (starting).
- **Terminal mark** (terminal rows, the strip's new-terminal button): Icon `terminal` (S5), a 6×10 chevron and a 7-long underscore stroke after it, in `--slate` (the terminal's "›" row mark and ">_" button, 2.20).
- **Server arrow** (server rows): Icon `arrow-up-right` (S5), 12×12, in `--slate`.
- **Stop and close crosses** (server rows' stop slot): Icon `close` (S3), 12×12, `--slate`, `--ink` on hover, as the phases 3-4 design's row × (its §4.8).

### 5.3 Command palette and arguments sheet

Standard sheet (760), height at most 640. Title "Commands"; System / User / MCP on the title line (5.1). Filter, then one-line rows: the label left; the shortcut right in `--t-stamp` `--slate` (the whole column hidden past 25 %, 2.2). User skill entries are two-line rows with the description as the detail. "Generating Prompt..." replaces the help footer with the working mark and the text while an MCP prompt loads. The arguments sheet is small (520): title as 2.2, one field per argument with its label above in `--t-label` small caps (a trailing "*" for required), 16 between fields; the required warning is the field's error line.

States: rest as 5.1; hover `--control-hover`; selected the active band; empty: a filter with no match leaves the list empty with no text, as the terminal (`internal/ui/dialog/commands.go` has no empty text); loading as 5.0.

### 5.4 Model picker and effort sheet

Standard sheet (760), height at most 720. Title "Switch Model"; "Large Task / Small Task" on the title line. Provider groups are group headings (5.1) holding the provider name; the "Configured" badge follows the name as a second small-caps label in `--slate` after " · ". Rows are one-line: the model name; in "Recently used" the provider name in the info column. "current" is the info text of the model in use. Onboarding draws the same sheet at the bottom left of the main column, 24 from its edges, with the line "To start, let's choose a provider and model." in `--t-reading` under the title. Effort sheet: small (520), rows in Title Case with "current" in the info column; the Ultracode line sits between the list and the footer: a 16×16 square box with 1px `--rule-control` outline (filled `--ink` with a drawn check in `--paper` when on), then "Ultracode on|off · workflows on every task" in `--t-small`.

States: as 5.1; disabled providers are absent, not dimmed; loading (provider refresh) shows the working mark in the help footer.

### 5.5 Usage sheet

Small sheet (520 wide, 640 tall at most). Under the title, per usage window: a small-caps label (the window name), the figure "<used>%" in `--t-figure`, and the side panel's meter (13 high, 1px `--meter-edge`, track `--meter-track`, fill `--meter`; `desktop/src/components/SidePanel.svelte:167-181`), then its lines ("<left>% left", "Model: …", "Resets: …", "In: …") in `--t-small` `--slate`, one per line. Windows are separated by a `Rule` with `marks="start"`. The "Context" block repeats the pattern. The footer line ("Refreshing usage…", "Refresh failed: …" in `--amber-ink`, "Checked: …") sits above the help footer.

States: loading as 5.0; error the footer line in `--amber-ink`; empty the terminal's texts ("No usage windows reported by this CLI.") in the empty style.

### 5.6 Sign-in screens

Standard sheet (760). Title as 2.4. Method choice: two square boxes side by side, each at least 220 wide and 120 high, 1px `--rule-control`, label `--t-reading` centered on two lines; the selected box has a 2 `--ink` border and `--band` fill (drawn like the side panel's flow nodes, whose border is 2 in `--figure`, `SidePanel.svelte:211-224`); stacked when they do not fit. API key sheet: the state title in `--t-heading`, the field, then the destination notice in `--t-small` `--slate` and the path in `--t-agent-mono`. Device flow: the code in `--face-mono` 28/34, letter-spaced 0.12em, centered in a 1px `--rule-table` box 72 high with + marks (4 arms) at its corners; the link under it in `--link`, underlined; "enter" and other key names in the instructions in `--keyword`. Status lines ("Verifying...", "Waiting for browser authentication...", " Fetching models...") use the loading style. Success "Authentication successful!" in `--ink` after Icon `check`; failure "Authentication failed." in `--error-ink` with the error in `--t-body-small` under it (3-4 design §4.11). AWS SSO sheet: the same frame with the command in `--t-agent-mono`.

States: as 5.0; while verifying, every control is disabled-looking except the spinner (DLG-048).

### 5.7 Sessions and Open Project

Standard sheet (760). Sessions: one-line rows: title left; info "~/short/folder · 3h ago" right (35 % budget). Rename: the row becomes a field of the row's height (44) with "Rename this session?" as a `--t-small` `--slate` line 6 above it, replacing nothing else. Delete: "Delete this session?" in `--t-small` `--ink` above the row, the row keeps the active band. Open Project (small, 520): rows with the short path in `--t-item` and "current" in the info column.

States: empty Open Project never shows (it does not open, 2.5); others as 5.0.

### 5.8 Question forms and secure entry (Q8 slot)

The form takes the typing box's place and width (S3's slot, drawn by P3.7; 3-4 design §4.9). Frame: a 1px `--rule-control` box (the typing box's outline, `desktop/src/components/composer/Composer.svelte:36-42`), no + marks, padding 16 20.

- **Tabs** (batches): small-caps labels in `--t-label`, 24 apart, on one line above the question; the active tab `--ink` with a 2 `--ink` underline, others `--slate`; "Confirm" last. Narrow: only the active label and "N/M" in `--t-small`.
- **Question**: `--t-reading` `--ink`; the description under it in `--t-body-small` `--slate` (sanitized Markdown, P3.4's `MarkdownView`).
- **Choices**: rows 40 high; a drawn mark 16 across at the text start (radio: ring 1.6 `--figure`, on = a filled `--choice` dot r 4; check: square, on = `--choice` fill with a drawn `--paper` check); the label `--t-item`, the description `--t-small` `--slate` on the same line after " · " when it fits, else below. The cursor row has `--band`. "Something else?" is a field (5.1) as the last row. Notes appear as a field under their choice, indented 32.
- **Pictures**: the picture area is 416 tall, to the right of the choices when the form is at least 900 wide, else under them; the picture keeps its aspect, centered, on `--paper` with a 1px `--rule-frame` border; status texts in the loading or empty style.
- **Confirm page**: "Ready to go?" in `--t-heading`, one bullet per answer in `--t-body-small`, the WARN tag (small caps "WARN" in `--paper` on `--attention`, padding 2 6) before "<n> question(s) unanswered", then the buttons "Yup!" (selected) and "Not yet" (5.1).
- **Collapsed**: one 44 high line: the question text in `--t-item`, the count in `--t-small` `--slate`, then a `Rule`.
- **Secure field**: the field (5.1) showing one "•" per character in `--t-field`; the status text right inside the field in `--t-small`: "Enter secret…" `--slate`, "Entered · saved on submit" `--slate`, "Saved" `--ink` after Icon `check`.

States: hover `--control-hover` on choice rows; focus on the form outline turns 1px `--vermilion`; error on the field rule (5.0); loading for pictures as 5.0.

### 5.9 Skills sheet and Toggle skills

Skills: standard (760). Title "Skills"; "Installed / Browse skills.sh" on the title line (5.1; 2.8). Rows one-line: label prefix (`system:`, `user:`, `project:`) in `--t-agent-mono` `--slate`, then the name; the info column "Enabled" in `--ink` or "Disabled" in `--slate`, with disabled names in `--slate`. A detail strip between two `Rule`s under the list, 2 lines of `--t-body-small` `--slate`, shows the selected skill's description (Browse: "<source> · https://skills.sh/<id>"). The sheet's status text sits above the help footer in `--t-small` (`--error-ink` for "Error: …").

Toggle skills: small (520). The note in `--t-body-small` `--slate` under the title. Groups "On", "Toggleable", "Off" as group headings. Each row starts with a 14×14 square: On filled `--plan`, Toggleable half-filled (left half `--plan`), Off empty with a 1px `--rule-control` outline. "↓ <N> more" in `--t-small` `--slate` as the last row.

States: loading and empty as 5.0 with the terminal's texts; error as the status text.

### 5.10 MCP toggles and MCP sign-in

Toggle MCPs: small (520). "Local / Global" on the title line. One-line rows: the status dot (5.2) at the text start, the name 16 after it, the status text in the info column (`--slate`; "error: …" in `--error-ink`). MCP sign-in: small (520): title as 2.9, the body in `--t-reading`, " (<n>/<total>)" in `--slate`, the waiting line in the loading style, the result in `--ink` after Icon `check`, or in `--error-ink`.

States: as 5.0; a toggled row changes in place, never re-sorts while the sheet is open.

### 5.11 Add Image picker

Wide sheet (1040 × 640). Title "Add Image"; the current folder path under the title rule in `--t-path` (uppercase, letter-spaced, as the chat's breadcrumb). Two columns split by a 1px `--rule-frame` line with + marks where it meets the rules: the list (600) and the preview (the rest). Rows 36 high in `--t-item`; folders end with "/"; links end with " →"; files that cannot be chosen and unreadable entries in `--mark`; the selected dimmed row uses `--disabled-band`. The preview is an `<img>` fitted inside a 1px `--rule-frame` box, centered; hidden below 300 of list height (2.10).

States: loading the preview shows a blank box; errors as the field error style under the list ("file too large, max 5MB").

### 5.12 Changes sheet

The phases 3-4 design's (its §4.7, P3.6). This design adds nothing to its look; it takes its colors from the tokens of 3.1.

### 5.13 Settings page and small setting sheets

Wide sheet (1040, as tall as the window minus 48). Title "Settings". Two columns: a group index of 220 on the left (small-caps group names in `--t-label`, 36 apart; the group in view `--ink` with the 4 vermilion edge, others `--slate`; a click scrolls to the group) and the rows on the right, separated by a 1px `--rule-frame` line with + marks at its ends. Rows 56 high: the label in `--t-item` at the left, the control right-aligned at 24 from the edge, a 1px `--rule` between rows. Controls: a switch is a 44×24 box (outer size, its 1px `--rule-control` outline included) holding an 18×18 square knob inset 3 from the box's outer edge on every side it touches: 3 from the top and 3 from the bottom; off, 3 from the left (x 3 to 21), the knob in `--slate` on `--paper`; on, 3 from the right (x 23 to 41), the knob in `--ink` on `--band`. In CSS the knob sits at `calc(3 * var(--px) - 1px)` inside the border; value-and-button rows show the value in `--t-small` `--slate` then a "Change…" button (5.1, height 36); choice rows ("Terminal" / "Desktop app") are two small-caps labels as on the title line. The closing line "Other settings are read from crush.json and crushrc." in `--t-small` `--slate`.

Small setting sheets (Fallback compaction, Memory Limit, Sub-agent Instructions, Notification Style): small (520), the hint under the title in `--t-body-small` `--slate`, the field or editor, the usage line in `--t-small` `--slate`, the error line, the help footer. The sub-agent editor is 3 to 14 lines of `--t-field`, outlined like a field.

States: as 5.0; a switch that is writing shows the working mark in place of the knob until the engine answers.

### 5.14 Themes sheet, new theme form, theme editor

Themes and the editor are docked to the right edge like the phases 3-4 design's Changes sheet (its §4.7), with no veil (`registerDialog(id, C, {veil: false})`, 7.1), so the live preview shows on the real window behind them; width 520 (Themes) and 760 (editor). Themes: "New Theme..." as the first row (with the plus icon), then the groups "System" and "User", one-line rows, "current" in the info column. New theme: small centered sheet (520) with the field and a reserved 24-high error line (THEME-019). Editor: rows 36 high: the role name in `--t-agent-mono` at the text start, a 20×20 swatch with a 1px `--rule-control` outline at 260, the value in `--t-agent-mono` after it (the selected row's value is the field); an invalid value fills the swatch with `--error-ink` (the theme's `error` role). "Save" and "Cancel" buttons (5.1) under the list; Save is disabled while a value is invalid.

States: as 5.0; previewing changes the whole window, the sheet included.

### 5.15 Tray entries, Background Processes and Sub-agents

Tray entries (inside S3's tray, drawn by P3.7): 36 high, the mark (5.2) then the label in `--t-small` `--ink`, 24 between entries; the hint in `--t-small` `--slate` at the right end. Focused entry: `--band` with the 4 vermilion edge. Sheets: standard (760); two-line rows: the mark, the name or command in `--t-item`; the detail ("<cli>/<model> · read-only · 2m05s", "job 12 · 0m41s") in `--t-small` `--info-soft`; a "Stop" or "Kill" button (5.1 at height 32, label `--ink`) in a fixed action slot at the right end, shown on the selected and hovered row and kept in place, never moving the info.

States: empty texts in the empty style; a stopped row leaves at once (no fade).

### 5.16 Helper view

The middle column keeps the chat's head, `ChatHead.svelte` (S3's, which P3.9 draws: `desktop/src/components/ChatHead.svelte:12-40`), and the rule with + marks under it (`desktop/src/components/Conversation.svelte:28`), so nothing jumps when the view opens: the helper's `name` as the title in `--t-title` and the chat's breadcrumb. The transcript is P3.5's `Timeline` with `readOnly`: no vermilion rail and no lettered keys (7.1). In the typing box's place, top to bottom (2.16):

- the read-only band, 44 high: a 1px `--rule-control` box on `--paper`, text start 14 (the typing box's inset, `composer/Composer.svelte:47`), holding "<name> · <status>[ · <model>] · read only" in `--t-small`: the name `--ink`, the status word done `--ink` after Icon `check`, failed `--error-ink`, stopped `--slate`, running `--ink` after the working mark (look 5.2), the rest `--slate`;
- phase 3's footer and the tray line, 8 below;
- the text button "← Return to parent" in `--t-button` `--ink`, then "esc" in `--t-agent-mono` `--slate` 12 after it, starting at the band's text start, 12 below.

States: hover underlines the button's text (1px, 4 below the baseline); focus on it shows the global outline; the band never takes focus.

### 5.17 Left list additions: terminal rows, server rows, strip, folder picker, close box

- **Terminals group**: the heading "Terminals [<n>]" in the day-label style (`ChatList.svelte:163-168`), and a 20×20 plus icon button at the list's right edge minus 19, on the heading's center line. Terminal rows use the chat row's geometry (81 high, `ChatList.svelte:182-206`): the terminal mark (look 5.2) where a chat row's title starts, then "Terminal" in `--t-item`; the folder's base name in `--t-meta` `--slate` on the second line.
- **Server rows** (2.22): under a `Rule` with `marks="both"` at the list's bottom. Every row is 40 high, text start 23, with a 20 wide slot kept free at the right end for the stop cross so nothing moves. A single server: Icon `arrow-up-right` (5.2), the name in `--t-small` `--ink` (underlined on hover, as a link), ":<port>" in `--t-agent-mono` `--slate` right-aligned before the slot. A program with several ports: the disclosure triangle (`Icon.svelte:66-67`, turned 90° when open), the name, "<N> ports" in `--t-small` `--slate` at the right; its servers indented 32 under it. The picked row: `--band`, the 4 vermilion edge, and in the slot the stop cross (5.2). While a row is picked, the hint " ↵ open · x stop · esc back" sits under the rows in the help-footer style (5.1).
- **Strip** (collapsed list): 64 wide; from the top: the expand button (the chevron icon turned to point right), the plus icon, the terminal mark, then one 44-high cell per row with its state mark (5.2) and its number (up to two digits) in `--t-agent-mono` `--slate`; a 1px `--rule` between chats and terminals; the shown row's cell has the 4 vermilion edge.
- **Folder picker**: standard (760). Title "New session in…" or "New terminal in…"; the path field in `--t-agent-mono` with the "Browse…" button right beside it at the field's height (50); matches as one-line rows 36 high; the group heading "Recent"; the error line under the field; the hint as the help footer.
- **Close box**: small (520) centered over the terminal tab: 'Close “Terminal”?' in `--t-heading`, the detail in `--t-body-small` `--slate`, then "Close" (selected) and "Keep".

States: as 5.0; hover on rows underlines the title as the chat list does.

### 5.18 Terminal tabs and the external editor tab

The middle column keeps the chat's head: the tab title ("Terminal", "Editor" or "Remote setup") in `--t-title`, the folder as the breadcrumb in `--t-path`, the head's rule. Below it the terminal fills the column with 12 of padding on `--paper`: text in `--face-mono` at 18/24 (never below 11 real px), colors `--ansi-*`, default text `--ink`, cursor `--ink`, selection `--selection` with the text in its own color (9.4, D14), scrollbar as 5.0. Pictures (sixel, iTerm2) draw inline. Nothing else: no frame around the terminal, no rail.

States: an exited shell closes the tab (2.25); a failed start shows its error in the loading line's place in `--error-ink`.

### 5.19 Quit sheet (PAL)

Small (520) with no title line, everything centered, in the terminal's order (`internal/ui/dialog/quit.go:165-176`): "Are you sure you want to quit?" in `--t-heading`; the buttons "Yep!" and "Nope" in one row 24 below it, "Nope" selected; the hint lines "To quit without confirmation" and "press ctrl+c twice." in `--t-body-small` `--slate`, 20 below the buttons. Help footer "←/→ switch options · enter/space confirm" (2.24).

States: as 5.1.

### 5.20 Phone sharing sheets and launcher password

Setup: small (520). Title "Remote Control"; the step title in `--t-heading`, the detail in `--t-body-small` `--slate`; buttons in one row, stacked full width when they do not fit; while checking, the loading line replaces the detail. Status: small (520): "On. Your phone can see this chat." in `--t-reading`; a two-row table in the side panel's table style (small-caps first column "Computer", "Phones"; `SidePanel.svelte:246-291`); the two notes in `--t-small` `--slate`; "Turn off" (label `--ink`, 5.1) and "Keep on" (selected). The REMOTE badge is P3.7's mode-mark badge (`setBadge`, SIDE-025), in `--link` (3.1). Launcher password: small (520): two masked fields with small-caps labels "New password" and "Type it again" above them, the error line, "Save" and "Cancel".

### 5.21 Update offers

The CLI update offer and the Crush update offers take the Q8 slot as a yes/no form drawn exactly as 5.8's yes/no question; the texts are 2.19's. The upstream notice and the background-build progress are update notices in the notice line (P3.8's look, SIDE-028).

### 5.22 Log viewer, About, crash notice

- **Log viewer**: wide (1040, the window's height minus 48). Title "Logs"; rows in `--face-mono` 16/24: the time in `--slate`, the level in small caps (`--error-ink` ERROR, `--amber-ink` WARN, `--ink` INFO, `--slate` DEBUG), the message in `--ink`, the other keys in `--slate`. The "Showing last 1000 lines…" line in `--t-small` `--slate` above the help footer; while following is on, a small-caps label "Following" (inferred text) sits on the title line in `--ink`.
- **About**: small (520). The title line holds the CRUSH AI wordmark as the first screen draws it (`ChatList.svelte:85-98`) instead of title text; the sheet's accessible name stays "About Crush Desktop" (2.28). Then "Crush Desktop <version>" in `--t-heading`; "Crush <version> · <commit> · <platform>" in `--t-small` `--slate`; a table in the side panel's style with the rows "Config", "Data", "Project", "Log" and "Crash reports" (the last two inferred labels) and the paths in `--t-agent-mono` (each path a link that opens the folder), then "Configs merge from top to bottom" in `--t-small` `--slate`.
- **Crash notice**: small (520). Title "Crush Desktop crashed" in `--t-project`; the body in `--t-reading`; the report file name in `--t-agent-mono` `--slate`; "Open report" (selected) and "Restart".

### 5.23 Debug overlay

A 36×48 square at left 36, top 24 (the terminal's 4 by 2 cells at 4, 1, with 9×24 cells), `position: fixed`, above everything, `pointer-events: none`, filled with a new random opaque color on each changed frame. No border.

## 6. Interfaces

Every name in this section is pinned. Names the phases 3-4 design defines are used exactly as it defines them: `desktop/docs/desktop-design.md`, read at `cc785a12`, cited "3-4 design §n". That is the `d54b0aec` text plus review rounds 2 and 3 (`b79a48ad`: `api_abort`, `callId`, `prompts.callStatus`, `connection::abort` and the rule that request bodies carry `Content-Length`) and the S3 review decisions after it: `http_<status>` for an engine status with no code of its own (its §1.2.2), `probe_request` answering `{request, opened}`, the overlay key rule of its §1.8 (only `menu` or `overlay` while a menu, the viewer or a sheet is open, and Ctrl+C asking `global` first unless the quit sheet is open), the `SvelteSet` closed store, `--ansi-7` `#6e6d6e`, the `children` props of `ItemFrame` and `EngineGate`, and `Frame.svelte` frozen with its `panel` prop following App's panel collapse. `cc785a12` then added two routes to its list (§1.3) for P3.3's stores: `agent/update` for the model restore and `lsps/{lsp}/diagnostics` for the error counts (`desktop/PLAN.md:420`). None of them renames a name used here. Names it lacks are added by scaffold S5 (7.1) or owned by one piece (7.3). Shapes are JSON-like; `?` marks an optional field.

### 6.1 Conventions

- **Engine calls.** `api.<name>(…)` from `src/lib/engine/api.js` (P3.3, 3-4 design §1.5.2; S5 adds the other functions of 6.5). Each goes through the Tauri command `api` (3-4 design §1.3): Rust adds this window's `client_id`, checks the route against the list in `connection.rs` and returns the engine's JSON, or throws `AppError {code, message, field?, status?}` (`error.rs`, 3-4 design §1.2.2). Paths are relative to the window's workspace (`"sessions"`) or absolute (`"/v1/memory"`); in section 2, `…/x` is the relative path `x`. A call made while the window reconnects throws `engine_unavailable` (nothing was sent); a socket that breaks during a call throws `transport` (the engine may have acted). The limit is 30 s. A call whose work runs inside the request (a summary, an MCP sign-in) carries a `callId`: it has no limit, and `api.abort(callId)` stops it (3-4 design §1.2.4, D40).
- **Global routes** (`/v1/memory`, `/v1/updates/*`, `/v1/limits`, G-2's `/v1/themes*`, G-9, G-16) are absolute paths sent through the calling window's own engine. No window calls another window's engine.
- **Engine events.** `onEvent(type, fn(eventKind, payload))` and `onResync(fn)` from `src/lib/engine/events.js` (S3, 3-4 design §1.5.4). `eventKind` is `"created"`, `"updated"` or `"deleted"`; `payload` is the record (2.0 rule 10). `onResync` runs after a batch's `resync` entry and on `connection{resync: true}` (a rejoin); every surface re-reads what it shows then.
- **Event order.** 3-4 design §1.5.3's rule applies: a store keyed by chat drops an event that names a chat not in `sessions.byId`, and the sessions store reads an unseen chat once on its `session` `created` event. The stores of these pieces are keyed by their own ids (a helper's `id`, a form's `form_id`, a job's identity, a remote action's `action_id`), like the links store's `chat_link` (the exception that rule names): they keep an event whenever it comes and look its chat up when they draw. So an event that names a chat this window has not seen yet is kept, not dropped (2.7, 2.15, 2.27).
- **Repeats.** The engine sends `secure_form` again each time a client's view of the chat changes (`secureViewChanged`, `internal/backend/secure.go:125-160`). ASK keeps one form per `form_id` and ignores a repeat (2.7).
- **Desktop settings.** `settings_get()`, `settings_set(key, value)` and the window event `settings-changed` (P3.2, 3-4 design §1.2.8, §1.3, §1.4.1). S5 adds the keys `theme` (string, 3.3) and `list_collapsed` (boolean, 2.20) to `settings::set_key`'s known keys. Other desktop processes learn of a change within 1 s through P3.2's watcher.
- **Rust services.** One `#[tauri::command]` per call in `commands.rs`, registered in `main.rs`, each a short delegate into the owning piece's module, and wrapped in `src/lib/engine/commands.js` with the JS argument names of 6.4 (Tauri maps camelCase to snake_case). Streams use a `tauri::ipc::Channel` argument, never window events. S5 adds two window events, `close-requested` and `window-request` (6.4), to the two of 3-4 design §1.4.1. Failures are `AppError`s; 6.4 lists the new codes.
- **Secrets.** Secret values (secure entry 2.7, API keys 2.4, the launcher password 2.27) never pass through `api`, a log line, the probe, a fixture recording, a notice or an error text. They go only through `api_secret` or `remote_password_set` (6.4).
- **Keys.** `bindKey(scope, combo, handler)` from `src/lib/keys.js` (S3, 3-4 design §1.8) with S3's scopes plus `main` and the combo `"*"` (S5, 7.1). A sheet takes its keys through KIT's frame (2.1), never with its own `bindKey`.
- **Registries.** `registerAction`/`runAction` (`lib/actions.js`), `registerDialog`/`openDialog`/`bringToFront`/`closeDialog`/`closeFront`/`hasDialog`/`frontDialog` (`lib/dialogs.js`), `createGrace` (`lib/grace.js`), `registerTraySection`/`focusTray` (`lib/tray.js`), `ui.openSlot`/`ui.closeSlot` (`stores/ui.svelte.js`) and `probeSection` (`lib/probe.js`), all S3 (3-4 design §1.9, §1.10). Each piece registers from its own `src/lib/plugins/<folder>.js`, which `lib/plugins.js` loads before the app mounts (3-4 design §1.5.1).

### 6.2 What these pieces use from phases 3-4

| Pass 1 name | Pinned name | From | Used by |
|---|---|---|---|
| `chat.current()`, `chat.onChange(fn)` | `sessions.currentId` and `sessions.byId[id]` (the title), `conn.project.cwd` (the folder), read in `$derived` or `$effect` | `stores/sessions.svelte.js`, `stores/connection.svelte.js` (P3.3) | PAL, MOD, ASK, EXT, LIVE, LST, SHARE, SYS |
| `chat.busy()`, `chat.onBusyChange(fn)` | `windowBusy()`: `busy(sessions.currentId)`, or a running `!` command of the shown chat in `composer.shells` (2.0 rule 2) | `src/lib/busy.js` (S5) over `busy` (P3.3) and `composer.shells` (P3.7) | PAL, MOD, SES, SET, LST, TERM, SHARE, SYS |
| `chat.ensureSession()` | `ensureSession()` | `stores/sessions.svelte.js` (P3.3) | EXT, SHARE |
| `chat.markBackgrounded()` | none: after `POST …/sessions/{sid}/background` answers `moved: true`, the engine's `message` event changes the tool result, which P3.6 draws with the `background` status (3-4 design §1.5.6 `ToolCallView.status`; CHAT-061). LIVE only hides its hint at once (2.15) | P3.6 | LIVE |
| `chats.show(sessionId)` | `openChat(id)` | P3.3 | SES, LST |
| `mainColumn.show(kind, C, props)`, `mainColumn.onClose(fn)` | `showMain({kind, component, props})` → `{close()}`; `mainView.current` | `src/lib/mainview.js`, `src/components/MainHost.svelte` (S5) | LIVE, TERM |
| `composerSlot.claim(kind, C, props)` | `ui.openSlot({kind, component, props})` → boolean; `ui.closeSlot(kind)`; `ui.slot` | `stores/ui.svelte.js` (S3; S5 adds the kind `update`, 7.1) | ASK, SYS |
| `composer.*` | `composer.drafts[sessions.currentId \|\| ""].text`; `setText(text)`; the caret from the field element's `selectionStart` while a `composer`-scope handler runs; `composer.mode` (`"normal"`, `"plan"`, `"yolo"`, `"bang"`: pass 1's `"code"` is normal or YOLO, `"shell"` is bang); `runAction("yolo.toggle")`; `runAction("mode.cycle")`; `attachSkill({id, name})`; `addAttachment(attachment)` with `{kind, name, mime, path?, base64}` (3-4 design §1.5.6 `Attachment`); `focusField()` | `stores/composer.svelte.js` (P3.7), `lib/actions.js` (S3) | PAL, VIEW, TERM, SHARE |
| `tray.register(id, entry)`, `tray.focus()` | `registerTraySection({id, order, visible(), header(), component, onKey, downFocus})`; `focusTray({down: true})` | `lib/tray.js` (S3) | LIVE |
| `notices.info\|warn\|error\|success\|update(text)` | `ui.notify(kind, text, ttlMs = 5000)` with kinds `info`, `success`, `warn`, `error`, `update` | `stores/ui.svelte.js` (S3) | all |
| `notices.setSticky(id, kind, text)`, `notices.clearSticky(id)` | `ui.notify(kind, text, 0)` (0: no time limit) and `ui.clearNotice(text)` (clears only while that text shows) | `stores/ui.svelte.js` (S3 already keeps a notice with `ttlMs` 0, `desktop/src/lib/stores/ui.svelte.js:23-38`; S5 adds only `clearNotice`'s `text` argument, 7.1) | LIVE |
| `status.setRemote(on)` | `setBadge("remote", on ? "REMOTE" : null)` | `stores/composer.svelte.js` (P3.7; SIDE-025) | SHARE |
| `keys.register({scope, combo, handler, when?})` | `bindKey(scope, combo, handler)` → `off()`; each terminal level of CMD-061 maps to one scope (2.2) | `lib/keys.js` (S3; S5 adds `main` and the rules of 7.1) | PAL, LIVE, LST, TERM, SHARE, SYS |
| `focus.set(target)` | `ui.setFocus(target)`; S5 adds the target `"main"` | `stores/ui.svelte.js` (S3) | KIT, PAL, LIVE, TERM |
| `markdown.render(text)` | `MarkdownView {text, messageId: "", sessionId, finished: true, plan: "none"}`, or `renderMarkdown(text, {sessionId})` followed by `guardLinks` | `components/render/MarkdownView.svelte`, `lib/render/markdown.js` (P3.4) | ASK |
| `links.open(href)`, `links.openExternal(url)`, `links.openPath(path)` | `openLink(href)` (it calls `open_link`, which in probe runs only appends to `<probe file>.opened.jsonl`) | `lib/render/links.js` (P3.4), `opener.rs` (P3.2) | ASK, AUTH, EXT, LST, SYS |
| `images.open(path)` | `ui.openViewer({path})` | `stores/ui.svelte.js` (S3), the viewer (P3.8) | ASK |
| `Transcript {session_id, readOnly}` | `Timeline {sessionId, readOnly: true}` | `components/timeline/Timeline.svelte` (P3.5; S5 adds `readOnly`, 7.1) | LIVE |
| `engine.switchProject(dir)` | `projectSwitch(projectDir)` (6.4), then `location.reload()` | S5 | SES |
| `windowFlags()` | `app_info()` with S5's fields `first_window`, `launch_id`, `new_chat`, `home` (6.4) | `commands.rs` (S3; S5) | LST, SHARE |
| `desktopSettings.get(key)`, `.set(key, value)`, `.onChange(key, fn)` | `settings_get()`, `settings_set(key, value)`, `listen("settings-changed", …)` | P3.2 | LST, THM |
| palette targets | `runAction(id)` for `chat.new`, `yolo.toggle`, `mode.cycle`, `todos.toggle`, `help.toggle`, `details.toggle`, `composer.customize` (phase 3) and `chat.close`, `tree.open`, `tree.fork`, `tree.clone`, `restore.undo` (phase 4) | `lib/actions.js` (S3); registered by P3.7, P3.8, P3.9, P4.1, P4.4 | PAL |
| probe sections | `probeSection(name, light, heavy)` | `lib/probe.js` (S3) | all |
| clipboard, opener, files | `clipboard_write(text)`, `clipboard_read()`, `open_link(url, cwd)`, `file_info(path, cwd)`, `file_read(path, cwd, maxBytes)` → `{mime, base64}` | P3.2 (3-4 design §1.2.8) | AUTH, EXT, ASK, VIEW, LIVE, TERM, SYS |
| a helper's chat in the stores | `api.getSession(id)`, then `upsertSession(record, {fromEvent: false})`, then `load(id)`: a helper's chat is a child session, which `GET …/sessions` leaves out (`internal/db/sql/sessions.sql:37-41`) | P3.3 | LIVE |
| closed chats | `isClosed(id)`, `reopen(id)` | `stores/closed.svelte.js` (P4.1) | SES |
| the list's order | `listmodel.buildRows(sessions, {parentOf, isClosed})` | `lib/listmodel.js` (P3.9); `parentOf` from `stores/links.svelte.js` (P4.1) | LST |
| the shown chat for the engine | `setCurrentSession(sid)` | `lib/engine/api.js` (P3.3) | LST |
| marks | `ActivityMark {size}` (P3.5); `Icon {name}` (S3; S5 changes two icons and adds two, 7.1); `Rule {marks}` | components | all |
| key grace | `openDialog(id, props, {grace: true})` over `createGrace` | S3 | KIT, AUTH, MOD |

### 6.3 What these pieces give phases 3-4

| Phase 3-4 side | Pinned | Owner | Rows |
|---|---|---|---|
| "/" typed into an empty field (P3.7) | the action `palette.open` (3-4 design §1.10) | PAL | CMD-001 |
| the model button (P3.7); onboarding (P3.9) | `openDialog("models", {onboarding?})` | MOD | DLG-021; START-001 |
| `exit` or `quit` sent (P3.7) | `openDialog("quit")` | PAL | TYPE-007, DLG-288 |
| START-021's errors (P3.9); Ctrl+K (S5's swap, 7.1) | `openDialog("sessions", {query?})` | SES | START-021; 2.5 |
| Ctrl+O in the field (reserved, 3-4 design §1.8) | TERM's `bindKey("composer", "ctrl+o", …)` | TERM | TYPE-015, CMD-030 |
| the tool group's 60-second hint | `registerToolHint(fn)` (`src/lib/toolhints.js`, S5), drawn by ToolGroup (S5's change, 7.1) | LIVE | LIVE-004 |
| P4.4's tree and restore sheets | the sheet frame `components/kit/Sheet.svelte` and the palette entries 6 to 9 of 2.2 | KIT, PAL | 3-4 design §5.2 (P4.4) and its D20 |
| every color | the tokens of 3.1 on `:root` | THM (values); S5 (defaults in `app.css`) | THEME-002 |

The Changes sheet is no longer here: the phases 3-4 design keeps it (its §2.8, P3.6; 2.11).

### 6.4 Rust services and window events

Commands S5 registers (signatures fixed by S5, bodies by the owner):

| Command | JS arguments | Result | Behavior | Owner (file) |
|---|---|---|---|---|
| `api_secret` | `path: string`, `body: object` | the engine's JSON | A POST like `api`, accepted only for `secure-forms/{fid}/submit` (`internal/rpc/routes_share.go:19`), `config/provider-key` (`internal/rpc/routes.go:121`) and G-5's `auth/verify-key`; any other path gives `secret_route`. The body is serialized into a `Vec<u8>` that is overwritten with zeros after the call. It is never logged, probed, recorded or copied into an error (an engine error keeps the engine's `code` and message only). In fixture mode it appends `{"secret": path, "keys": [<the body's top-level key names>]}` to `<probe file>.api.jsonl` and answers `null` | S5 (`connection::call_secret`, `commands.rs`, `fixture.rs`) |
| `window_open` | `projectDir: string`, `sessionId?: string`, `newChat?: boolean`, `launchId?: string` | `{label, created}` | Focuses the window that shows `projectDir` (compared after resolving links), else opens a new window (label `w<n>`) for it. A focused window gets the window event `window-request`. A new window's `app_info` gives that folder, `session` = `sessionId` (else none), `first_window: false`, `new_chat` and its launch ID (LIST-049, REM-009, DLG-088) | S5 (`windows.rs`) |
| `window_close` | — | `null` | Ends the calling window after its close steps (`lib/closing.js`): `connection::close` (`DELETE /v1/clients/{client_id}`, 3-4 design §1.2.6), then the window is destroyed. The last window ends the app (LIST-017) | S5 (`windows.rs`) |
| `project_switch` | `projectDir: string` | `null` | Closes the calling window's connection (`connection::close`) and records `projectDir` as its folder; the page then reloads and `conn.start()` (P3.3) joins that project. YOLO and debug stay, since they are the process's start flags (DLG-117) | S5 (`windows.rs`) |
| `pty_spawn` | `cwd: string`, `argv?: string[]`, `cols`, `rows`, `onData: Channel` | `{id, pid}` | `portable-pty`. No `argv`: `$SHELL -i`, else `/bin/sh -i` (2.25). An `argv[0]` of `"crush"` runs the binary P3.1's `crush_bin::resolve` found (2.27). The environment is `login_env::login_env()` (P3.1) cleaned by 2.25's rule. `onData` receives `{data: <base64>}` batches, at most one per 8 ms per tab (inferred), then `{exit: {code, signal}}` once. Error `pty_failed` | TERM (`pty.rs`) |
| `pty_write` | `id`, `data: <base64>` | `null` | `no_such_pty` after the exit | TERM (`pty.rs`) |
| `pty_resize` | `id`, `cols`, `rows` | `null` | | TERM (`pty.rs`) |
| `pty_cwd` | `id` | path or `null` | `/proc/<pid>/cwd` on Linux, `null` elsewhere (the widget reads OSC 7) | TERM (`pty.rs`) |
| `pty_close` | `id` | `null` once ended | SIGHUP to the shell, then SIGTERM, then SIGKILL to its process group after 5 s (2.25) | TERM (`pty.rs`) |
| `pty_close_all` | — | `null` | every tab of the calling window, at most 6 s | TERM (`pty.rs`) |
| `editor_prepare` | `text`, `line`, `column` | `{path, argv}` | Writes `text` to a new temporary file (mode 0600) and builds 2.26's editor command from `$VISUAL`, else `$EDITOR`, of `login_env::login_env()`; error `no_editor` | TERM (`editor.rs`) |
| `editor_finish` | `path` | `{text}` | Reads and deletes the file; only a path `editor_prepare` made is accepted | TERM (`editor.rs`) |
| `alert_notify` | `title`, `body` | `{sent, error?}` | `tauri-plugin-notification`, app name "Crush" (2.18). With `--probe` it also appends `{"kind": "notify", title, body, at}` to `<probe file>.alerts.jsonl` | SYS (`alerts.rs`) |
| `alert_sound` | `id` | `{player}` (`null` when none worked) | 2.18's player chain, 5 s for the whole chain; `id` `"bell"` is the bell style | SYS (`alerts.rs`) |
| `alert_window_state` | `state: "idle"\|"working"\|"blocked"\|"done"\|"error"`, `projectDir`, `sessionId`, `focused` | `null` | The app-wide table of window states behind the badge count, progress and attention (NOTE-007, NOTE-008), applied in `taskbar.rs`. With `--probe` each taskbar call is appended to `alerts.jsonl` as `{"kind": "taskbar", badge, progress, attention}` | SYS (`alerts.rs`, `taskbar.rs`) |
| `alert_handler` | `projectDir`, `sessionId` | a window label | The window that shows that chat, else the project's lead window (its earliest-opened window still open) (2.18, LIST-052) | SYS (`alerts.rs`) |
| `crash_pending` | — | `{path, kind: "panic"\|"webview"}` or `null` | each report once (2.28) | SYS (`crash.rs`) |
| `crash_restart` | — | `null` | relaunches the app after a panic, or reloads the window after a web view crash | SYS (`crash.rs`) |
| `logs_tail` | `lines: number` | `{lines, truncated, missing}` | the calling window's project log of 2.28; the path comes from the window's `Ensured.data_dir` through `connection::ensured` (S5) | SYS (`logs.rs`) |
| `logs_follow` | `onLines: Channel` | `{followId}` | sends new lines as the file grows | SYS (`logs.rs`) |
| `logs_unfollow` | `followId` | `null` | | SYS (`logs.rs`) |
| `selfupdate_run` | `tag` | `{ok, firstLine, logPath, version?}` | G-19's `crush self-update run --tag <tag> --json` through `crush_bin::resolve` (P3.1); one run per app, else `update_running` (2.19) | SYS (`selfupdate.rs`) |
| `selfupdate_install` | — | the same shape | `crush self-update install --json` | SYS (`selfupdate.rs`) |
| `crush_dirs` | — | `{lines: string[]}` | `crush dirs --cwd <the window's project>` with its output not a terminal, 5 s: one folder per line, Config, Data, then the project folders (`internal/cmd/dirs.go:25-58`; 2.28) | SYS (`crushinfo.rs`) |
| `crush_stats` | — | `{ok: boolean, lines: string[]}` | `crush stats --cwd <the window's project>`, 60 s (inferred bound): it writes the HTML report and opens it in the browser itself (`internal/cmd/stats.go:147-157`); `lines` is its output (2.28) | SYS (`crushinfo.rs`) |
| `docker_mcp_version` | — | `{ok, version?, error?}` | `docker mcp version`, 5 s; the answer is kept 10 s (2.9) | EXT (`docker.rs`) |
| `projects_list` | — | `{projects: [{path, data_dir, last_accessed, saved}]}` | reads `projects.json` (2.5) | SES (`projects.rs`) |
| `project_save` | `path` | `null` | keeps unknown fields; a temporary file and a rename (2.5) | SES (`projects.rs`) |
| `project_remove` | `path` | `null` | | SES (`projects.rs`) |
| `recent_folders` | `limit` | `string[]` | 2.21's rule | SES (`projects.rs`) |
| `subagent_instructions_read` | — | `string` | 2.12 | SET (`configfiles.rs`) |
| `subagent_instructions_write` | `text` | `null` | a temporary file and a rename (2.12) | SET (`configfiles.rs`) |
| `remote_password_exists` | — | `boolean` | presence only (section 4, M6) | SET (`configfiles.rs`) |
| `remote_password_set` | `first`, `second` | `{ok, message}` | `crush remote password --stdin` (G-20) with the two lines on standard input; neither value is logged; the buffers are zeroed | SHARE (`remote.rs`) |
| `dir_list` | `path` | `{path, entries: [{name, is_dir, is_symlink, readable}]}` | `~` expanded, `path` absolute in the answer, `is_dir` follows links; errors `bad_path`, `not_a_folder` (2.10, 2.21) | VIEW (`dirlist.rs`) |
| `folder_pick` | `start` | path or `null` | the system folder dialog (`tauri-plugin-dialog`); with `--probe` and `CRUSH_DESKTOP_TEST_PICK_FOLDER`, that path and no dialog (2.21) | LST (`folderdialog.rs`) |

`app_info` (3-4 design §1.3) gains, through S5: `first_window: boolean`; `launch_id: string | null` (the window's launch ID the first time it is asked, then `null`; REM-009); `new_chat: boolean`; `home: string`. Its `cwd`, `session` and `continue_last` then describe the calling window, not the process.

Window events S5 adds (3-4 design §1.4.1 has `events-ready` and `settings-changed`):

| Event | Payload | Sent when |
|---|---|---|
| `close-requested` | none | the system close button or Cmd+Q asks to close this window. `lib/closing.js` runs the close steps, then calls `window_close`. If the window has not called it 10 s later, Rust closes the window anyway (inferred bound) |
| `window-request` | `{sessionId: string \| null, newChat: boolean, launchId: string \| null}` | `window_open` focused this window instead of opening one. SES opens `sessionId` (2.5), LST starts a new chat for `newChat` (2.21), SHARE starts the quiet share for `launchId` (2.27) |

New error codes (`AppError.code`): `secret_route`, `pty_failed`, `no_such_pty`, `no_editor`, `not_a_folder`, `dialog_failed`, `notify_failed`, `update_running`.

Registries S5 adds (complete files, 7.1):

- `src/lib/mainview.js`: `mainView` (`$state` `{current: null | {kind: "helper" | "terminal", component, props}}`), `showMain({kind, component, props})` → `{close()}` (replaces the current view), `closeMain()`. Showing a view calls `ui.setFocus("main")`; closing calls `focusField()`.
- `src/lib/closing.js`: `registerCloseStep({id, order, run})` (`run` is async), `closeWindow()`: runs the steps by `order`, each awaited, a failing step logged and skipped, then `window_close`. It listens to `close-requested`.
- `src/lib/listsections.js`: `registerListSection({id, order, place: "header" | "rows" | "footer", visible(), component})`, `listSections(place)` (the visible ones by `order`).
- `src/lib/rowstates.js`: `registerRowState(fn)` with `fn(sessionId)` → `{icon, text}` or `null`; `rowState(sessionId)` (the first non-null answer).
- `src/lib/toolhints.js`: `registerToolHint(fn)` with `fn(item)` → text or `null` for a `ToolGroupItem` (3-4 design §1.5.6); `toolHint(item)`.
- `src/lib/busy.js`: `windowBusy()` (2.0 rule 2).
- `src/lib/ticker.svelte.js` (not a registry; shared by every working and helpers mark): `ticker` (`$state {frame, second}`), `frame = floor(now_ms / 100) mod 8`, `second = floor(now_ms / 1000) mod 4` on the wall clock (2.20, look 5.2).

### 6.5 Engine routes and events per piece

Functions marked P3.3 already exist (3-4 design §1.5.2) or come with P3.3: `updateAgent()`, whose route joined the 3-4 design's list at `cc785a12` (`desktop/PLAN.md:420`) and which P3.3's own branch names so (`desktop/src/lib/engine/api.js:114` on `desktop-p3-3` at `758a50a4`). S5 adds the others to `api.js` and their routes to `connection.rs`'s list. "G-n" routes do not exist yet (9.1); their entries go in with S5 and answer 404 until G56's part merges. Route lines are in `internal/rpc/`, read on `desktop` at `65e4250d` (no file under `internal/rpc/` or `internal/proto/` changed from `40405abf` to `cc785a12`).

| Function | Route | Source | Pieces |
|---|---|---|---|
| `config()` (P3.3) | GET `config` | `routes.go:41` | PAL, MOD, SET, SYS |
| `customCommands()` | GET `commands` | G-7 | PAL |
| `mcpPrompts()` | GET `mcp/prompts` | `routes.go:138` | PAL |
| `getMcpPrompt({client_id, prompt_id, args})` | POST `mcp/get-prompt` | `routes.go:139` | PAL |
| `summarize(sid, callId)` | POST `agent/sessions/{sid}/summarize`, with a `callId` | `routes.go:110` | PAL |
| `permissionsSkip()`, `setPermissionsSkip(skip)` (P3.3) | GET, POST `permissions/skip` | `routes.go:86-87` | PAL |
| `initPrompt()`, `initProject()` (P3.3) | GET `project/init-prompt`, POST `project/init` | `routes.go:50`, `:49` | PAL |
| `capabilities()` | GET `agent/capabilities` (G-8 adds three fields) | `routes_live.go:20` | MOD (for PAL and LIVE) |
| `providers()` (P3.3) | GET `providers` | `routes.go:42` | MOD |
| `setConfig(scope, key, value)`, `removeConfig(scope, key)` (P3.3) | POST `config/set`, `config/remove` | `routes.go:117-118` | MOD, AUTH, SET, SHARE |
| `setModel(scope, model_type, model)`, `setCompact(enabled)` (P3.3) | POST `config/model`, `config/compact` | `routes.go:119-120` | MOD, SET |
| `refreshOAuth(scope, provider_id)` | POST `config/refresh-oauth` | `routes.go:123` | MOD |
| `importCopilot()` | POST `config/import-copilot` | `routes.go:122` | MOD |
| `defaultSmallModel()` (P3.3) | GET `agent/default-small-model` | `routes.go:112` | MOD |
| `updateAgent()` (P3.3) | POST `agent/update` | `routes.go:98` | MOD, SET |
| `initAgent()` | POST `agent/init` `{interactive: true}` | `routes.go:97` | MOD |
| `agentSession(sid)`, `queuedCount(sid)` (P3.3) | GET `agent/sessions/{sid}`, `…/prompts/queued` | `routes.go:100`, `:102` | MOD, SYS |
| `limits()`, `refreshLimits(kind)` (P3.3) | GET `/v1/limits`, POST `/v1/limits/{kind}/refresh` | `routes_live.go:37-38` | MOD (the Usage sheet reads P3.3's `limits` store) |
| `refreshProviders()` | POST `/v1/providers/refresh` | G-9 | MOD |
| `startAuthFlow({platform})`, `sendAuthCode(flow_id, code)`, `cancelAuthFlow(flow_id)` | POST `auth/flows`, `auth/flows/{flow_id}/code`, `auth/flows/{flow_id}/cancel` | G-5 | AUTH |
| `apiSecret("auth/verify-key", {provider_id, api_key})` | POST `auth/verify-key` | G-5 | AUTH |
| `apiSecret("config/provider-key", body)` | POST `config/provider-key` | `routes.go:121` | AUTH |
| `savedSessions(selected)` | GET `saved-sessions?selected=` | `routes.go:55` | SES |
| `changeSavedSession(change)` | POST `saved-sessions` | `routes.go:56` | SES, LST |
| `listSessions()`, `getSession(sid)`, `messages(sid)` (P3.3) | GET `sessions`, `sessions/{sid}`, `sessions/{sid}/messages` | `routes.go:57`, `:59`, `:63` | SES, LIVE, LST |
| `answerQuestions(answer)`, `cancelQuestions()` | POST `questions/answer`, `questions/cancel` | `routes.go:89-90` | ASK |
| `pendingQuestion(sid)` | GET `questions/pending?session_id=` | G-12 | ASK |
| `secureForms()`, `cancelSecureForm(form_id)` | GET `secure-forms`, POST `secure-forms/{fid}/cancel` | `routes_share.go:18`, `:20` | ASK |
| `apiSecret("secure-forms/{fid}/submit", submit)` | POST `secure-forms/{fid}/submit` | `routes_share.go:19` | ASK |
| `skills()`, `setKeptSkill(sid, …)`, `mcpStates()` (P3.3) | GET `skills`, POST `sessions/{sid}/kept-skills`, GET `mcp/states` | `routes.go:128`, `:66`, `:140` | EXT |
| `installedSkills()`, `manageSkill(request)` | GET `skills/installed`, POST `skills/manage` | `routes.go:130-131` | EXT |
| `searchSkills(q)` | GET `skills/search?q=` | G-14 | EXT |
| `mcpDisabled()`, `setMcpDisabled({name, disabled})`, `mcpEnabled()`, `setMcpConfigDisabled({name, disabled})` | GET, POST `mcp/disabled`; GET `mcp/enabled`; POST `mcp/config-disabled` | `routes.go:148-151` | EXT |
| `mcpPendingAuth()`, `mcpAuthUrl(name)`, `mcpAuth(name, callId)` | GET `mcp/pending-auth`, GET `mcp/auth-url?name=`, POST `mcp/auth` with a `callId` | `routes.go:141-143` | EXT |
| `dockerMcp(enable)` | POST `mcp/docker/enable` or `mcp/docker/disable` | `routes.go:146-147` | EXT |
| `memory()`, `setMemoryLimit(value)`, `claimMemoryNotice(episode)` | GET `/v1/memory`, PUT `/v1/memory/limit`, POST `/v1/memory/claim-notice` | `routes_live.go:27-29` | SET, LIVE, SYS |
| `setCpuAffinity(enabled)` | POST `/v1/cpu-affinity` | G-16 | SET |
| `themes()`, `theme(name)`, `resolveTheme({base, palette})`, `saveTheme(name, {base, palette})`, `createTheme({name, base})`, `renameTheme(name, new_name)`, `deleteTheme(name)`, `revertTheme(name)` | 3.2's routes under `/v1/themes` | G-2 | THM |
| `helpers(sid)`, `stopHelper(sid, hid)` | GET `sessions/{sid}/helpers`, POST `…/helpers/{hid}/stop` | `routes_live.go:11-12` | LIVE |
| `jobs(sid)`, `killJob(sid, ref)`, `background(sid)` | GET `sessions/{sid}/jobs`, POST `…/jobs/kill`, POST `…/background` | `routes_live.go:17-19` | LIVE, LST |
| `servers()` | GET `servers` | G-18 | LIVE, LST |
| `startRemote({launch_id?})`, `stopRemote()`, `remote()`, `remotePresence(presence)`, `remoteActionDone(aid, done)` | POST `remote/start` (a repeat start from the share's owner with a new `launch_id` updates the running share's launch: G-26b), `remote/stop`; GET `remote`; POST `remote/presence`, `remote/actions/{aid}/done` | `routes_share.go:8-12` | SHARE, LST |
| `cliUpdates()`, `declineCliUpdates(bins)`, `installCliUpdates(bins)` | GET `/v1/updates/clis`, POST `…/decline`, POST `…/install` | `routes_live.go:31-33` | SYS |
| `crushUpdate()`, `declineCrushUpdate(tag)` | GET `/v1/updates/crush`, POST `/v1/updates/crush/decline` | `routes_live.go:34-35` | SYS |
| `version()` (P3.3) | GET `/v1/version` | `routes.go:26` | SYS |

`DELETE /v1/clients/{client_id}` (`routes.go:31`) is sent only by `connection::close` (P3.1) through `window_close` and `project_switch`; the window never calls it.

Events each piece registers with `onEvent` (types at `internal/pubsub/events.go:19-46`; the wire types are 6.7's):

| Event | Pieces |
|---|---|
| `config_changed` | PAL, MOD, AUTH, EXT, SET, THM, LIVE, SYS |
| `session` | PAL, MOD, SES, EXT, LIVE |
| `mcp_event` | PAL, EXT |
| `skills_event` | EXT |
| `limits` | none of mine (P3.3's store; MOD reads it) |
| `agent_event` | AUTH (`aws_sso_auth`, `aws_sso_auth_result`, `re_authenticate`), SYS (`agent_finished`, `error`) |
| `question_batch_request`, `question_batch_notification` | none of mine: P3.3's `prompts` store keeps them and ASK draws `prompts.question`; SYS also listens for the alert |
| `secure_form`, `secure_form_closed` | ASK; SYS (`secure_form`, for the alert) |
| `prompt_pending` | none of mine (P3.3) |
| `helper` | LIVE |
| `message`, `run_complete` | LIVE (`message` for the helper view), SYS (`run_complete`) |
| `memory_status` | LIVE, SYS |
| `remote_status`, `remote_action` | SHARE |
| `permission_request` | SYS (the alert; the prompt is P3.7's) |
| `update_available`, `cli_updates_installed` | SYS |
| `auth_flow` (G-5) | AUTH |

### 6.6 State per piece

Each piece's per-window state is one `$state` object in `src/lib/<folder>/store.svelte.js` with plain functions to change it (3-4 design §1.5.3's store rule), unless the table says otherwise. App-wide state lives in Rust, since every window is its own web view.

| Piece | Per window | App-wide (Rust) | Saved |
|---|---|---|---|
| KIT | none (the stack and the grace times are S3's `lib/dialogs.js` and `lib/grace.js`) | — | — |
| PAL | filter, category, selection, MCP prompt in flight, the summary's `callId` | — | — |
| MOD | task type, filter, selection; Usage countdown; `capabilities` (`lib/models/capabilities.svelte.js`) | — | config (engine) |
| AUTH | flow id and state, the verified key (memory only, cleared on close) | — | config (engine) |
| SES | filter, selection, rename or delete mode | — | `projects.json` |
| ASK | the shown request (`prompts.question`, P3.3, or the G-12 read), live answers, notes, tab, focus side; secure forms by `form_id`; secure field values (cleared on submit, cancel and close) | — | — |
| EXT | tabs, filters, selections, request in flight, the MCP sign-in queue | Docker answer, 10 s (`docker.rs`) | config (engine) |
| SET | open sheet, field text, errors | — | config (engine), `subagent-instructions.md` |
| THM | preview theme, editor values; `theme` (`lib/themes/current.svelte.js`) | — | `theme` (desktop settings), `crush-desktop.theme` (local storage, 3.3) |
| VIEW | folder, selection, preview | — | — |
| LIVE | helpers by `id`, jobs, server jobs, hint state, open sheet, helper view | — | — |
| LST | unread set, strip, picker state, server rows; `leftList` (`lib/leftlist/store.svelte.js`) | windows per project, lead windows (`windows.rs`, S5) | `list_collapsed` (desktop settings) |
| TERM | tabs: pty id, title, folder, scroll, selection (`lib/terminal/tabs.svelte.js`) | pty processes (`pty.rs`) | — |
| SHARE | share state, last `RemoteStatus`, phones, last presence, launch ID (used once) | single-instance hand-off (`windows.rs`, S5) | `options.remote_launch_target` (engine), `remote-password` (launcher) |
| SYS | focus, shown chat, alert state, log viewer follow | window states, badge, progress, running self-update, pending crash report | — |

### 6.7 Wire types S5 adds to `wire.js`

JSON names exactly as the engine sends them (`internal/proto/share.go`, `live.go`, `proto.go`, `requests.go`, `mcp.go`, `agent.go`, `version.go`, `skill_management.go`, on `desktop`); the G-n shapes are this design's contracts (9.1).

```js
/** @typedef {{id:string, session_id:string, child_session_id:string, name:string, cli:string, model:string,
 *   effort?:string, fast?:boolean, read_only?:boolean, status:"running"|"done"|"failed"|"stopped",
 *   started:string, ended?:string, delivered?:boolean, started_by?:string}} Helper          (live.go:13-28) */
/** @typedef {{pid?:number, job_id?:string, command:string, name?:string, started:string, session_id:string,
 *   started_by?:string}} Job                                                                (live.go:51-59)
 *  @typedef {{job_id?:string, pid?:number}} JobRef                                         (live.go:62-65)
 *  @typedef {{moved:boolean}} BackgroundResult                                              (live.go:69-71) */
/** @typedef {{kind:string, can_background:boolean, supports_fast?:boolean, supports_ultracode?:boolean,
 *   supports_thinking_toggle?:boolean}} AgentCapabilities                       (live.go:74-77; the last three G-8) */
/** @typedef {{supported:boolean, percent:number, high:boolean, episode:number, top?:{name:string, bytes:number}[],
 *   message?:string, limit_bytes:number, box_used_bytes:number, total_ram_bytes:number}} MemoryStatus (live.go:98-114)
 *  @typedef {{claimed:boolean}} ClaimResult                                                 (live.go:117-119)
 *  @typedef {{value:string}} MemoryLimitRequest; @typedef {{limit_bytes:number, applied:boolean, notice:string}} MemoryLimitResult
 *                                                                                           (live.go:122-131) */
/** @typedef {{name:string, bin:string, path:string, current:string, latest:string}} CLIUpdate
 *  @typedef {{skipped:boolean, updates:CLIUpdate[], errors?:string[]}} CLIUpdateCheck
 *  @typedef {{bins:string[]}} CLIUpdateBins
 *  @typedef {{name:string, latest:string, error?:string}} CLIUpdateResult
 *  @typedef {{results:CLIUpdateResult[]}} CLIUpdatesInstalled
 *  @typedef {{tag:string, current:string, dir:string}} CrushRelease
 *  @typedef {{has_checkout:boolean, release?:CrushRelease, staged?:string, declined:boolean, error?:string}} CrushUpdateCheck
 *  @typedef {{tag:string}} CrushTag                                                         (live.go:134-185) */
/** @typedef {{current_version:string, latest_version:string, is_development:boolean}} UpdateAvailable (proto.go:54-58) */
/** @typedef {{launch_id?:string}} RemoteStart
 *  @typedef {{state:string, title:string, detail:string, action?:string, button?:string}} RemoteSetup
 *  @typedef {{state:string, host?:string, port?:number, phones?:string[], setup?:RemoteSetup, error?:string}} RemoteStatus
 *  @typedef {{session_id:string, focused:boolean, plan:boolean}} RemotePresence
 *  @typedef {{action_id:string, kind:string, session_id:string, text?:string, uploads?:RemoteUpload[]}} RemoteAction
 *  @typedef {{upload_id:string, name:string, mime:string, size:number}} RemoteUpload
 *  @typedef {{error?:string, session_id?:string}} RemoteActionDone                         (share.go:7-79) */
/** @typedef {{question_id:string, label:string, status:string}} SecureField
 *  @typedef {{form_id:string, session_id:string, request:QuestionRequest, fields:SecureField[]}} SecureForm
 *  @typedef {{form_id:string, reason:"submitted"|"cancelled"|"answered_elsewhere"|"closed"}} SecureFormClosed
 *  @typedef {{answers:QuestionAnswer[], values:Object<string,string>}} SecureSubmit       (share.go:82-113) */
/** @typedef {{id:string, session_id:string, tool_call_id:string, questions:QuestionItem[], confirm_title:string,
 *   confirm_description:string}} QuestionRequest
 *  @typedef {{id:string, type:"yes_no"|"single_choice"|"multi_choice"|"free_text"|"secure_entry", label:string,
 *   question:string, description:string, choices:QuestionChoice[], selected?:string[]}} QuestionItem  (selected: G-13)
 *  @typedef {{id:string, label:string, description:string, image:string}} QuestionChoice
 *  @typedef {{batch_request_id:string, responses:QuestionResponse[]}} QuestionAnswer
 *  @typedef {{request_id:string, selected_ids:string[], fill_in_text:string, yes?:boolean, notes?:Object<string,string>}} QuestionResponse
 *  @typedef {{resolved:boolean}} QuestionAnswerResponse
 *  @typedef {{batch_id:string, session_id:string}} QuestionNotification                    (proto.go:224-281) */
/** @typedef {{id:string, title:string, description:string, prompt_id:string, client_id:string,
 *   arguments:{id:string, title:string, description:string, required:boolean}[]}} MCPPrompt   (mcp.go:150-164)
 *  @typedef {{client_id:string, prompt_id:string, args:Object<string,string>}} MCPGetPromptRequest
 *  @typedef {{prompt:string}} MCPGetPromptResponse
 *  @typedef {{name:string, url:string}} MCPPendingAuthServer; @typedef {{auth_url:string}} MCPAuthResponse
 *  @typedef {{name:string, disabled:boolean}} MCPSetServerDisabledRequest                 (requests.go:135-176) */
/** @typedef {{scope:number, provider_id:string, kind:string, api_key:any}} ConfigProviderKeyRequest
 *  @typedef {{scope:number, provider_id:string}} ConfigRefreshOAuthRequest
 *  @typedef {{token?:object, success:boolean}} ImportCopilotResponse                      (requests.go:53-93)
 *  @typedef {{action:"toggle"|"install", name?:string, enabled?:boolean,
 *   skill?:{id:string, name:string, source:string, skillId:string, installs:number}}} ManageSkillRequest
 *                                                     (skill_management.go:14-19; internal/skills/directory.go:23-28) */
/** @typedef {{version:string, commit:string, build_id:string, go_version:string, platform:string, protocol:number}} VersionInfo
 *                                                                                           (version.go:4-11)
 *  AgentEvent (3-4 design §1.5.5) gains the fields the engine already sends: aws_sso_command?:string, aws_sso_url?:string
 *  (agent.go:29-55), and provider_id?:string (G-10). Its `type` also takes "agent_finished", "re_authenticate",
 *  "aws_sso_auth" and "aws_sso_auth_result" (internal/agent/notify/notify.go:11-27). */
/** G-n contracts (9.1):
 *  @typedef {{name:string, source:"builtin"|"user", overridden:boolean, dark:boolean}} ThemeListItem
 *  @typedef {{name:string, root:string, base:string, source:string, overridden:boolean, pure:boolean, dark:boolean,
 *   revision:string, palette:Object<string,string>, resolved:Object<string,string>, syntax:Object<string,string>,
 *   markdown:Object<string,string>}} ThemeView                                             (G-2, 3.2)
 *  @typedef {{flow_id:string, url:string, user_code:string, code_entry:boolean}} AuthFlow
 *  @typedef {{flow_id:string, state:"waiting"|"saving"|"done"|"failed", error?:string}} AuthFlowEvent (G-5, 2.4)
 *  @typedef {{id:string, name:string, content:string, arguments:{name:string, title:string, required:boolean}[],
 *   skill:{name:string, description:string, skill_file_path:string}|null}} CustomCommand     (G-7, 2.2)
 *  @typedef {{port:number, program:string, name:string, command:string, job_id?:string, pid?:number,
 *   session_id:string}} ServerPort                                                          (G-18, 2.22)
 *  @typedef {{results:{source:string, ok:boolean, error?:string}[]}} ProvidersRefresh       (G-9)
 *  @typedef {{cpus:string}} CPUAffinity                                                     (G-16)
 *  AgentInfo (S3's wire.js:41-45) gains agent?:"coder"|"plan"|"", the main agent's name     (G-26, section 4) */
```

## 7. Build pieces

Fifteen app pieces, the scaffold S5 and the Go piece G56. Paths are relative to `desktop/`, as in the phases 3-4 design. Each app piece is built in its own Git worktree from `desktop` after S5 and owns only the files listed for it here; a piece that needs a change in a file it does not own asks the coordinator, who changes it on `desktop` (3-4 design §5.1's rule).

### 7.1 Scaffold S5

**When.** After S3, after phase 3's pieces (P3.1 to P3.10) and P4.1 are merged into `desktop`, and before any piece of this design starts. S5 edits files those pieces own (the table below says which), so it waits for them. P4.2 and P4.3 may still run: S5 touches none of their files. P4.4 starts after KIT and PAL merge (3-4 design §5.2).

**Who.** The coordinator commits it, as with S3.

**What it holds.** Every name this design shares between its pieces or with phases 3-4, every change phases 5-6 need in a file that S3, a P3 piece or a P4 piece owns, the approved libraries, and empty bodies for the rest, so all fifteen pieces compile from the first commit.

| File | Owner before S5 | S5 change |
|---|---|---|
| `src-tauri/Cargo.toml`, `src-tauri/Cargo.lock` | S3 | adds, each pinned exactly: `portable-pty = "=0.9.0"`, `tauri-plugin-dialog = "=2.8.3"`, `tauri-plugin-single-instance = "=2.5.4"`, `tauri-plugin-notification = "=2.7.0"` (the current releases on 2026-10-18; each needs `tauri` ^2.12, and the lock has 2.12.4); under `[target.'cfg(target_os = "linux")'.dependencies]`, `webkit2gtk = "=2.0.2"` (already in `Cargo.lock` through wry; named so `crash.rs` can watch the web process); `[profile.release]` `strip = false` and `debug = "line-tables-only"` (2.28). The approved clipboard and opener plugins are not added: P3.2's `clipboard.rs` and `opener.rs` already serve every use here (3-4 design §1.2.8) |
| `src-tauri/src/main.rs` | S3 | `crash::install()` as the first statement; when `CRUSH_REMOTE_LAUNCH` is set, the process re-executes itself (`std::os::unix::process::CommandExt::exec`) with `--launch-id <id>` added and the variable removed, before any thread starts, so the single-instance hand-off, which carries only arguments, keeps the launch (REM-017); the flags `--project DIR` (the first window's folder, the same as `--cwd`; PLAN.md:143), `--launch-id ID` and `--new-chat` (the forms a second process hands over, REM-017, LIST-049); the plugins `tauri_plugin_single_instance::init(windows::handoff)` first, then `tauri_plugin_dialog::init()` and `tauri_plugin_notification::init()`; `.manage()` of `windows::Windows`, `pty::Ptys`, `editor::Edits`, `alerts::Alerts`, `logs::Follows` and `selfupdate::Runner`; a `mod` line for every module below; the close hook becomes: prevent the close, emit `close-requested` to that window, and call `windows::close` 10 s later if the window is still open (6.4); `windows::close` keeps S3's order, leaving the engine before it destroys the window (`desktop/src-tauri/src/main.rs:65-75`, `:100-110`); `crash::watch_webview` for the first window; every command of 6.4 in `generate_handler!` |
| `src-tauri/src/state.rs` | S3 | `Launch` gains `launch_id: Option<String>` and `new_chat: bool` (the first window's) |
| `src-tauri/src/error.rs` | S3 | the codes of 6.4 |
| `src-tauri/src/commands.rs` | S3 | one delegate per command of 6.4; `app_info`'s new fields, read per window from `windows::launch_for`; `api_secret` goes to `fixture.rs` in fixture mode, else to `connection::call_secret`; `probe_write` from any window but the first writes `<probe file>.<label>` (for example `/tmp/probe.json.w2`), so two windows of one app never overwrite each other's probe (8.2) |
| `src-tauri/src/fixture.rs` | S3 | `api_secret` appends `{"secret": <path>, "keys": [<the body's top-level key names>]}` to `<probe file>.api.jsonl` and answers `null` |
| `src-tauri/src/connection.rs` | P3.1 | the routes of 6.5's functions not marked P3.3 in the route list (the G-n routes too); `call_secret(app, label, path, body: Vec<u8>)`: a POST through the same `EngineHttp::call` with a `Content-Length` body (3-4 design §1.2.4), the buffer overwritten with zeros when the call ends either way, nothing of it logged; `ensured(app, label) -> Option<Ensured>`; a window other than the first takes its folder from `windows::launch_for(label)` instead of `AppState.launch`, and `-D` applies to the first window only |
| `src-tauri/src/settings.rs` | P3.2 | `set_key` also accepts `theme` (string; `""` means the default, 3.3) and `list_collapsed` (boolean, 2.20) |
| `src-tauri/src/windows.rs` | new; S5, complete | `Windows` (window label → `WindowLaunch {cwd, session, continue_last, new_chat, launch_id, opened_at}`); `open`, `close`, `switch`, `handoff` (reads a second process's `--project`/`--cwd`, `--launch-id` and `--new-chat`, then calls `open`), `launch_for(label)`, `lead_window(project)` (the earliest-opened window of that project still open); the commands `window_open`, `window_close` and `project_switch` (6.4) |
| `src-tauri/src/{pty,editor,alerts,taskbar,crash,logs,selfupdate,crushinfo,docker,projects,configfiles,remote,dirlist,folderdialog}.rs` | new; placeholders | the public signatures behind 6.4's commands, bodies `Err(AppError::not_built(..))`; `crash::install` and `crash::watch_webview` do nothing. Each file belongs to its owner in 7.3 from then on, like the placeholders below |
| `src-tauri/capabilities/default.json`, `src-tauri/tauri.conf.json` | S3 | unchanged: every plugin call is made from Rust, so the web view needs no plugin permission (inferred; S5's check 4 confirms it, and a missing permission is added here, as S3's rule says, 3-4 design §5.1). The CSP already allows `data:` pictures, which ASK and VIEW use (`tauri.conf.json:26`), and scripts from the app itself, which `theme-boot.js` is (`:24`) |
| `package.json`, `pnpm-lock.yaml` | S3 | `@xterm/xterm` 6.0.0, `@xterm/addon-fit` 0.11.0, `@xterm/addon-image` 0.9.0, pinned exactly (npm's current releases on 2026-10-18; none has peer dependencies) |
| `src/app.css` | S3 | the tokens S5 adds (3.1, last table), with the edition's values |
| `index.html` | none (exists today, `desktop/index.html:1-12`) | `<script src="/theme-boot.js"></script>` in `<head>`, before the page draws (3.3) |
| `public/theme-boot.js` | new; S5, complete | a plain script: reads local storage `crush-desktop.theme` (3.3's shape) and, when present, sets each cached token on `document.documentElement.style` and `color-scheme`; any error leaves app.css's defaults |
| `src/components/composer/Composer.svelte` | P3.7 | the Send icon's color becomes `var(--on-accent)` instead of `var(--paper)` (`desktop/src/components/composer/Composer.svelte:89`; the same color in the edition; other themes need it, 3.1) |
| `src/lib/stores/composer.svelte.js` | P3.7 | mode follows `AgentInfo.agent` on every agent-info read, unless in bang mode: `"plan"` gives `plan`; `"coder"` gives `yolo` while the skip switch is on, else `normal` (TYPE-002's order); `""` changes nothing (G-26; the agent info is re-read on every `config_changed`, 3-4 design §1.5.3) |
| `src/components/Icon.svelte` | S3 | `warning`: the "!" stroke and dot fill `var(--on-amber)` instead of `var(--paper)` (`Icon.svelte:60-61`; 3.6); `working`: the ring in `var(--icon-ring, var(--rule-control))` and a quarter arc in `var(--icon-arc, var(--busy))` (`stroke-dasharray` 12 36) whose offset is `-6 * ticker.frame` (look 5.2; a parent sets both properties to `--mark` for the background mark); new `terminal` (look 5.2's terminal mark) and `arrow-up-right` (server rows, look 5.17) |
| `src/App.svelte` | S3 | left column: `leftlist/Strip.svelte` instead of `ChatList` while `leftList.strip`; middle column: `MainHost` over `Conversation`, which stays mounted inside a `hidden` wrapper while `mainView.current` is set, so its draft and scroll survive; `system/DebugOverlay.svelte` when `app_info().ui_debug` |
| `src/components/overlay/OverlayHost.svelte` | S3 | no veil behind a front sheet registered with `{veil: false}` (the docked Themes sheet and theme editor, 2.13); the page behind stays inert |
| `src/lib/dialogs.js` | S3 | `registerDialog(id, Component, {veil = true} = {})` |
| `src/lib/keys.js` | S3 | (1) the scope `main`, between `tray` and `composer`, active while `ui.focus === "main"`; while the main view is a terminal tab, no scope after `main` runs, and a key no binding took is left to the page (the terminal widget). (2) The combo `"*"`: any key, tried after the scope's exact combos. (3) The terminal-tab exception to S3's quit key: while a terminal tab has focus, Ctrl+C does not ask `global` first (`keys.js:52-67`, `:91-96`), so it reaches the shell like any key no binding took. The Ctrl+C binding itself is PAL's, a plain `global` binding (2.24). (4) Ctrl+K opens the Sessions sheet, `openDialog("sessions")`, instead of focusing the chat-list search (3-4 design §1.8: "its results are phase 5"; 2.2); if phase 3 made that binding in P3.9's `plugins/list.js` rather than here, S5 changes it there |
| `src/lib/types.js` | S3 | `SlotEntry.kind` gains `"secure"` (if S3 left it out) and `"update"`; the type `MainViewEntry {kind: "helper" \| "terminal", component, props}` |
| `src/lib/stores/ui.svelte.js` | S3 | the slot rule becomes: `permission`, `question` and `secure` replace `plan`, `customize` and `update`; `update` replaces nothing; `clearNotice(text)`: given a text, it clears the notice only while that text shows (S3's `clearNotice()` takes no argument, `desktop/src/lib/stores/ui.svelte.js:40-43`; S3's `notify` already keeps a notice with `ttlMs` 0 until another replaces it, `:23-38`); the focus target `"main"` |
| `src/lib/engine/wire.js` | S3 | the typedefs of 6.7 |
| `src/lib/engine/commands.js` | S3 | a wrapper per command of 6.4: `apiSecret`, `windowOpen`, `windowClose`, `projectSwitch`, `ptySpawn`, `ptyWrite`, `ptyResize`, `ptyCwd`, `ptyClose`, `ptyCloseAll`, `editorPrepare`, `editorFinish`, `alertNotify`, `alertSound`, `alertWindowState`, `alertHandler`, `crashPending`, `crashRestart`, `logsTail`, `logsFollow`, `logsUnfollow`, `selfupdateRun`, `selfupdateInstall`, `crushDirs`, `crushStats`, `dockerMcpVersion`, `projectsList`, `projectSave`, `projectRemove`, `recentFolders`, `subagentInstructionsRead`, `subagentInstructionsWrite`, `remotePasswordExists`, `remotePasswordSet`, `dirList`, `folderPick` |
| `src/lib/engine/api.js` | P3.3 | the functions of 6.5 not marked P3.3, including `apiSecret(path, body)` over the command |
| `src/components/ChatList.svelte` | P3.9, P4.1 | mounts `listSections("header")` under the search rule, `listSections("rows")` after the chat rows and `listSections("footer")` above the list's end rule; the search box hands off to the Sessions sheet: a typed character, Enter or a click in it opens `openDialog("sessions", {query: <the box's text>})` and empties the box (2.5); ↓, ↑, Tab and Esc in it keep P3.9's behavior, so the focus order of 3-4 design §1.8 still reaches the rows |
| `src/lib/sessionid.js` | P3.9 | START-021's errors open `openDialog("sessions", {query: <the given ID>})` instead of focusing the chat-list search (`desktop/PLAN.md:134`: "with the Sessions search open"; 2.5); this is the file START-021's row names (3-4 design §2.5), and if P3.9 placed that focus call in another of its files, S5 changes it there |
| `src/components/list/ChatRow.svelte` | P3.9, P4.1 | a row that has no state mark of its own from P3.9 draws `rowState(row.id)` (icon and text) in the state slot |
| `src/components/timeline/Timeline.svelte` | P3.5 | the prop `readOnly` (no selection rail, no item letters, no menu item that changes the chat, no focus hand-off to the typing box) and the exported `scrollLines(n)`, `scrollPages(n)` and `scrollEnd()` |
| `src/components/tools/ToolGroup.svelte` | P3.6 | under a working group, the line `toolHint(item)` in `--t-small` `--slate` when it is not `null` |
| `src/lib/{mainview,closing,listsections,rowstates,toolhints,busy}.js`, `src/lib/ticker.svelte.js`, `src/components/MainHost.svelte` | new; S5, complete | the registries of 6.4. `ticker` is `$state {frame, second}` with `frame = floor(now_ms / 100) mod 8` and `second = floor(now_ms / 1000) mod 4` on the wall clock, so every window shows the same frame (2.20); it ticks only while something reads it and stands still under `prefers-reduced-motion`. `MainHost` draws `mainView.current` in the middle column |
| `tests/run-in-sandbox.sh` | S3 | `--no-app`: everything as before except starting the app; the driver starts it through `apps.App` (8.2) |
| `tests/check.sh` | S3 | passes `--no-app` on to `run-in-sandbox.sh` |
| `tests/build-crush.sh` | S3 | `go build -tags crushtest …` (the seams of G-21 to G-24; with `CRUSH_TEST_SEAMS` unset the binary behaves as a normal build, 9.1) |
| `tests/fakes/fake-cmd` | new; S5, complete | a stand-in for system programs: appends `{cmd, args, cwd, at}` to `$FAKE_CMD_LOG`; exits 1 when its name is in `FAKE_CMD_FAIL` (a comma list); prints `$FAKE_CMD_STDOUT_<NAME>` (the name upper-cased, `-` as `_`) when set; otherwise prints nothing and exits 0 |
| `tests/fakes/bin/{canberra-gtk-play,pw-play,paplay,docker,xdg-open,x-www-browser,www-browser}` | new; S5 | links to `../fake-cmd`: the sound players of 2.18, `docker mcp version` of 2.9, and the browser openers a child process runs itself (`crush stats`, 2.28; inferred list: the openers `github.com/pkg/browser` tries on Linux), so no test opens a browser |
| `tests/fakes/notify_catcher.py`, `tests/driver/apps.py`, `tests/driver/p5_selftest.py` | new; S5, complete | 8.2 and 8.6 |

**Placeholders** (S5 creates them; the named piece owns them from then on, as with S3's placeholders, 3-4 design §5.1). Their exports and props are pinned, so the pieces that use them build before the owner merges.

| Path | Owner | Exports or props |
|---|---|---|
| `src/components/kit/Sheet.svelte` | KIT | `{id, title, width: "small" \| "standard" \| "wide", maxHeight?, controls?, help: [{key, text, disabled?}], onKey?, docked?, children}` (2.1, look 5.1) |
| `src/components/kit/HelpFooter.svelte` | KIT | `{pairs: [{key, text, disabled?}]}` |
| `src/components/kit/FilterList.svelte` | KIT | `{items, filterText(item), query, selected (bindable), infoBudget?, groups?, row}` (`row` is a snippet) |
| `src/components/kit/Field.svelte` | KIT | `{value (bindable), placeholder?, prompt?, masked?, error?, reserved?: string[]}` |
| `src/components/kit/Area.svelte` | KIT | `{value (bindable), minLines, maxLines, reserved?: string[]}` |
| `src/components/kit/Buttons.svelte` | KIT | `{buttons: [{id, label, key?, destructive?}], selected (bindable), onPress(id), stack?}` |
| `src/components/kit/Scrollbar.svelte` | KIT | `{target, mode: "default" \| "always" \| "never"}` (SET-006) |
| `src/lib/kit/fuzzy.js` | KIT | `fuzzyFind(query, strings)` → `[{index, score, matched: number[]}]` (2.1) |
| `src/lib/models/capabilities.svelte.js` | MOD | `capabilities` (`$state`, an `AgentCapabilities`), `refreshCapabilities()` |
| `src/lib/signin/accounts.js` | AUTH | `signIn(platform)`, `signOut(platform)` → `Promise<"done" \| "cancelled">` |
| `src/lib/ask/localform.js` | ASK | `askYesNo({label, question, description, onAnswer(yes)})` → `{close()}` |
| `src/lib/themes/current.svelte.js` | THM | `theme` (`$state {name, dark, revision}`) |
| `src/lib/terminal/tabs.svelte.js` | TERM | `terminals` (`$state`, a list of `{id, title, cwd, exited}`), `openTerminal({cwd, argv?, title?})` → `{id, exited: Promise<{code}>}`, `showTerminal(id)`, `askCloseTerminal(id)`, `closeAllTerminals()` |
| `src/lib/leftlist/store.svelte.js` | LST | `leftList` (`$state {collapsed, strip}`) |
| `src/components/leftlist/Strip.svelte` | LST | none (reads the stores) |
| `src/components/system/DebugOverlay.svelte` | SYS | none |

**Frozen after S5.** S3's frozen files (3-4 design §5.1), S5's complete files above, `index.html`, and the phase 3-4 files S5 edited (`connection.rs`, `settings.rs`, `api.js`, `ChatList.svelte`, `list/ChatRow.svelte`, `sessionid.js`, `Timeline.svelte`, `ToolGroup.svelte`, `composer/Composer.svelte`, `stores/composer.svelte.js`, `build-crush.sh`) stay frozen for the rest of phases 5 and 6. A piece that needs a change asks the coordinator, who changes it on `desktop`, and every piece merges it. A phase 3 or 4 fix to one of these files goes the same way.

**S5's own check.**

1. `pnpm test` and `cargo test` pass, including S5's tests: `mainview.test.js` (a new view replaces the old; closing gives focus back to the field), `closing.test.js` (steps run by order; a failing step is logged and skipped; `window_close` comes last), `listsections.test.js`, `rowstates.test.js` (the first non-null answer wins), `toolhints.test.js`, `ticker.test.js` (frames from fixed clocks: 0 → 0, 99 → 0, 100 → 1, 799 → 7, 800 → 0), `keys.test.js` additions (the `main` scope's place; a terminal tab's focus leaves unbound keys to the page, Ctrl+C included, which then asks no `global` binding first; `"*"`), `ui.test.js` additions (the slot table; `clearNotice(text)` clears only while that text shows), and the Rust `windows_open_focuses_existing`, `app_info_is_per_window`, `close_requested_fallback_closes`, `call_secret_never_logs` (a marker in the body appears in no log line, error text or fixture line), `call_secret_zeroes_body`, `call_secret_sends_content_length`, `secret_route_refused`, `route_list_has_5_6_routes`.
2. `tests/build.sh` and `tests/build-crush.sh` build.
3. `tests/check.sh --no-app p5_selftest.py` passes (8.6).
4. Every command of 6.4 answers through `invoke` (`not_built` is a valid answer).
5. Every phase 3 and phase 4 driver of 3-4 design §6.5 still passes on `desktop` with S5 merged, and the look checks' scores do not drop (the warning icon's "!" moves toward the approved picture's dark mark, 3.6).

### 7.2 Order

1. Engine pieces A to D are merged into `desktop` (piece A: lifecycle and wire; B: live work; C: tree actions; D: phone sharing and secure entry; `engine-design.md:2682-2751`). B's follow-ups are merged too (`d3aa5502`, `desktop/PLAN.md:410`); they changed behavior, not shapes, and this design depends only on shapes.
2. S3; phase 3 (P3.1 to P3.10); P4.3; then P4.1 and P4.2 (3-4 design §5.2).
3. S5, once phase 3 and P4.1 are merged.
4. The fifteen app pieces at once, and G56 beside them. KIT merges first (every sheet uses its frame), PAL second; the others in any order. A piece whose checks need a gap (9.1) builds against that gap's contract (6.5, 6.7) and runs those checks after G56's part merges.
5. P4.4 starts when KIT and PAL are merged (3-4 design §5.2).
6. Fixture checks run in the piece's worktree; engine and two-app checks run on `desktop` after the merges (3-4 design §5.2).

### 7.3 Pieces

Folders: each piece's files sit under one folder name, so ownership is visible in every path.

| Piece | Phase | Folder | Driver |
|---|---|---|---|
| KIT | 5 | `kit` | `tests/driver/p5_kit.py` |
| PAL | 5 | `palette` | `tests/driver/p5_palette.py` |
| MOD | 5 | `models` | `tests/driver/p5_models.py` |
| AUTH | 5 | `signin` | `tests/driver/p5_signin.py` |
| SES | 5 | `sessions` | `tests/driver/p5_sessions.py` |
| ASK | 5 | `ask` | `tests/driver/p5_ask.py` |
| EXT | 5 | `extensions` | `tests/driver/p5_extensions.py` |
| SET | 5 | `settings` | `tests/driver/p5_settings.py` |
| THM | 5 | `themes` | `tests/driver/p5_themes.py` |
| VIEW | 5 | `pictures` | `tests/driver/p5_pictures.py` |
| LIVE | 6 | `live` | `tests/driver/p6_live.py` |
| LST | 6 | `leftlist` | `tests/driver/p6_leftlist.py` |
| TERM | 6 | `terminal` | `tests/driver/p6_terminal.py` |
| SHARE | 6 | `remote` | `tests/driver/p6_remote.py` |
| SYS | 6 | `system` | `tests/driver/p6_system.py` |

Every app piece also owns `tests/fixtures/<folder>-*.json` and `tests/fakes/scripts/<folder>-*.json` if it makes any, and its unit tests sit beside its library files as `src/lib/<folder>/*.test.js`. Every piece needs S5, the stores of P3.3 and the registries of S3; the lists below name everything else, with the piece that provides it. Row counts come from section 1.

**KIT** (20 rows: DLG-001 to DLG-020)
- Owns: `src/components/kit/{Sheet,HelpFooter,FilterList,Field,Area,Buttons,Scrollbar}.svelte` (S5's placeholders); `src/lib/kit/{fuzzy,sheetkeys,fieldkeys,areakeys,helpfit}.js` and their tests; `src/lib/plugins/kit.js` (one `bindKey("overlay", "*", …)` that hands the key to the front sheet's `onKey`, then closes the front sheet on Esc and Alt+Esc when the sheet did not take them, DLG-003); `tests/driver/p5_kit.py`.
- Needs: S3's `lib/dialogs.js` and `OverlayHost.svelte` (the stack, the veil, `inert`), `lib/grace.js` (`{grace: true}`), `Rule.svelte`; S5's `"*"` key rule.
- Done-checks: the row map's checks; `u:kit.fuzzy_parity` (scores equal `sahilm/fuzzy` v0.1.3's on a fixed table of 40 pairs), `u:kit.help_truncate`.

**PAL** (44 rows: CMD-001 to CMD-013, CMD-019 to CMD-023, CMD-033, CMD-035 to CMD-037, CMD-050, CMD-056 to CMD-064; DLG-258 to DLG-263, DLG-288 to DLG-293)
- Owns: `src/components/palette/{Palette,Arguments,Quit}.svelte`; `src/lib/palette/{entries,customcommands,globalkeys,actions,store.svelte}.js` and their tests; `src/lib/plugins/palette.js` (the action `palette.open`; the dialogs `commands`, `arguments`, `quit`; the global keys of 2.2; Ctrl+C's plain `bindKey("global", "Ctrl+C", …)`, 2.24); `tests/driver/p5_palette.py`.
- Needs: KIT (frame, `fuzzyFind`); MOD's `capabilities`; the actions of P3.7 (`yolo.toggle`, `mode.cycle`, `todos.toggle`), P3.8 (`help.toggle`, `details.toggle`, `composer.customize`), P3.9 (`chat.new`) and P4.4 (`tree.open`, `tree.fork`, `tree.clone`, `restore.undo`, hidden until registered); the other pieces' dialog and action ids (7.4); P3.7's `attachSkill`, `setText` and `send` for user entries and MCP prompts; S5's `closeWindow()` (Quit, CMD-056); engine B (`agent/capabilities`); gaps G-4, G-7, G-8.
- Runs for phases 3-4: TYPE-007's window step (3-4 design §6.5).
- Done-checks: the row map's checks; `u:pal.entry_order` (labels, order and conditions of 2.2's table); `u:pal.args_longest_first` (2.2, Q-P4); `u:pal.quit_key` (Ctrl+C with no selection opens `quit`, over an open sheet too; with a text selection the handler returns `false` and opens nothing).

**MOD** (35 rows: DLG-021 to DLG-040, DLG-281 to DLG-287, CMD-016, CMD-017, CMD-024 to CMD-027, MISC-011, MISC-019)
- Owns: `src/components/models/{ModelPicker,Effort,Usage}.svelte`; `src/lib/models/{groups,choose,toggles,providers,capabilities.svelte,store.svelte}.js` and their tests; `src/lib/plugins/models.js` (dialogs `models`, `reasoning`, `usage`; actions `model.fast`, `model.ultracode`, `model.thinking`, `providers.refresh`); `tests/driver/p5_models.py`; `tests/fakes/seams/models.json`.
- Needs: KIT; AUTH's `auth_method` sheet (by id); P3.3's `limits` store and `refreshLimits`; P3.7's model button (it opens `models`); P3.9's onboarding (it opens `models` with `{onboarding: true}`); engine B (`agent/capabilities`, `/v1/limits`); gaps G-8, G-9; seam G-24 `limits`.
- Runs for phases 3-4: START-001's window step.
- Done-checks: the row map's checks; `u:mod.groups` (the six group rules of 2.3).

**AUTH** (37 rows: see section 1)
- Owns: `src/components/signin/{AuthMethod,ApiKey,OAuth,AwsSso}.svelte`; `src/lib/signin/{flows,accounts,store.svelte}.js` and their tests; `src/lib/plugins/signin.js` (dialogs `auth_method`, `api_key_input`, `oauth`, `aws_sso`; the `agent_event` listener for `aws_sso_auth`, `aws_sso_auth_result` and `re_authenticate`); `tests/driver/p5_signin.py`; `tests/fakes/seams/signin.json`.
- Needs: KIT; S5's `apiSecret`; P3.2's `clipboard_write`; P3.4's `openLink`; gaps G-5, G-6, G-10; seam G-24 `auth`.
- Done-checks: the row map's checks.

**SES** (24 rows: see section 1)
- Owns: `src/components/sessions/{Sessions,Projects}.svelte`; `src/lib/sessions/{match,store.svelte}.js` and their tests; `src/lib/plugins/sessions.js` (dialogs `sessions`, `projects`; action `project.save`; `window-request`'s `sessionId`); `src-tauri/src/projects.rs`; `tests/driver/p5_sessions.py`.
- Needs: KIT (`fuzzyFind`); P3.3's `listSessions`, `openChat` and sessions store; P4.1's `isClosed` and `reopen`; S5's `project_switch`; engine A (session records); gap G-11.
- Done-checks: the row map's checks; `r:ses.projects_keep_unknown` (unknown fields survive a save).

**ASK** (61 rows: see section 1)
- Owns: `src/components/ask/{QuestionForm,Tabs,Choices,FreeText,Confirm,Picture,SecureForm,SecureField,YesNo}.svelte`; `src/lib/ask/{form,answers,secure,localform,store.svelte}.js` and their tests; `src/lib/plugins/ask.js` (opens the slot `question` while `prompts.question` names the shown chat; `onEvent("secure_form")`, `onEvent("secure_form_closed")` and `onResync` for secure forms); `tests/driver/p5_ask.py`.
- Needs: P3.3's `prompts` store (`prompts.question`); S3's slot; P3.4's `MarkdownView` and `openLink`; the viewer through `ui.openViewer` (S3, P3.8); P3.2's `file_read` for pictures (shown as `data:` URLs); S5's `apiSecret`; engine D (secure forms); gaps G-12, G-13.
- Runs for phases 3-4: CHAT-010's and CHAT-072's window steps.
- Done-checks: the row map's checks; `u:ask.answer_shape` (the `QuestionAnswer` body for every type); `u:ask.secure_repeat` (a repeated `secure_form` for a known `form_id` changes nothing).

**EXT** (38 rows: see section 1)
- Owns: `src/components/extensions/{Skills,KeptSkills,McpToggles,McpAuth}.svelte`; `src/lib/extensions/{skills,mcp,store.svelte}.js` and their tests; `src/lib/plugins/extensions.js` (dialogs `skills`, `kept_skills`, `mcp_toggles`, `mcp_auth`; actions `docker.enable`, `docker.disable`); `src-tauri/src/docker.rs`; `tests/driver/p5_extensions.py`; `tests/fakes/fake_mcp_extra.py` (8.4); `tests/fakes/seams/extensions.json`.
- Needs: KIT; P3.3's `skills`, `mcpStates`, `setKeptSkill` and `ensureSession`; P3.4's `openLink`; P3.10's `fake_mcp.py` (read only); gaps G-4, G-14; seam G-24 `skills_search`.
- Done-checks: the row map's checks.

**SET** (59 rows: see section 1)
- Owns: `src/components/settings/{Settings,AutoCompact,MemoryLimit,SubagentInstructions,NotificationStyle}.svelte`; `src/lib/settings/{rows,store.svelte}.js` and their tests; `src/lib/plugins/settings.js` (dialogs `settings`, `autocompact`, `memory_limit`, `subagent_instructions`, `notifications`; actions `sidebar.toggle`, `composer.focusOnly`, `thoughtline.toggle`, `cpus.toggle`); `src-tauri/src/configfiles.rs`; `tests/driver/p5_settings.py`.
- Needs: KIT; AUTH's `signIn` and `signOut`; THM's `theme` sheet and SHARE's `launcher_password` sheet (by id); P3.3's `config`, `setConfig`, `removeConfig` and `setCompact`; engine B (`/v1/memory`); gaps G-1, G-3, G-9, G-16, G-17, G-26.
- Done-checks: the row map's checks; the settings-sync checks of section 4 that no row carries, `x:set.plan_mode_per_project` among them.

**THM** (85 rows: see section 1)
- Owns: `src/components/themes/{Themes,NewTheme,ThemeEditor}.svelte`; `src/lib/themes/{tokens,apply,contrast,current.svelte,cache}.js` and their tests; `src/lib/plugins/themes.js` (applies the cached theme when imported, before the app mounts, 3.3; dialogs `theme` and `theme_editor` with `{veil: false}`, and `theme_new`); `tests/driver/p5_themes.py`.
- Needs: KIT (`fuzzyFind`); P3.2's `settings_get`, `settings_set` and `settings-changed`; S5's `public/theme-boot.js`, which reads THM's cache (3.3); gaps G-1, G-2, G-3.
- Done-checks: the row map's checks (3.8 lists the theme-wide ones, `u:thm.token_map` and `u:thm.contrast_builtin` among them).

**VIEW** (15 rows: see section 1)
- Owns: `src/components/pictures/ImagePicker.svelte`; `src/lib/pictures/{picker,store.svelte}.js` and their tests; `src/lib/plugins/pictures.js` (dialog `filepicker`); `src-tauri/src/dirlist.rs`; `tests/driver/p5_pictures.py`.
- Needs: KIT; P3.7's `addAttachment`; P3.2's `file_info` and `file_read` (`maxBytes` 5 MB, `too_big` past it).
- Done-checks: the row map's checks; `r:view.dir_list` (links followed for `is_dir`, unreadable entries marked, `~` expanded, the errors `bad_path` and `not_a_folder`).

**LIVE** (32 rows: see section 1)
- Owns: `src/components/live/{HelpersSection,JobsSection,Background,SubAgents,HelperView}.svelte`; `src/lib/live/{helpers.svelte,jobs.svelte,memory,ctrlb,hint}.js` and their tests; `src/lib/plugins/live.js` (tray sections `helpers` (order 30) and `jobs` (order 40), both `downFocus`; dialogs `background`, `sub-agents`; Ctrl+B; the tool hint; the memory notice); `tests/driver/p6_live.py`; `tests/fakes/seams/live.json`.
- Needs: KIT; S3's `lib/tray.js`; S5's `showMain` and `registerToolHint`; P3.5's `Timeline` with `readOnly`; P3.3's `getSession`, `upsertSession` and `load`; MOD's `capabilities` (`can_background`); engine B (helpers, jobs, background, memory); gap G-18; seam G-21 `memory`.
- Runs for phases 3-4: CHAT-009's, CHAT-061's and CHAT-062's window steps.
- Done-checks: the row map's checks.

**LST** (33 rows: see section 1)
- Owns: `src/components/leftlist/{Strip,TerminalRows,ServerRows,StartFailure,CollapseButton,FolderPicker}.svelte` (`Strip` is S5's placeholder); `src/lib/leftlist/{store.svelte,unread,states,picker,servers,numbers,windowclose}.js` and their tests; `src/lib/plugins/leftlist.js` (list sections `terminals` (`rows`) and `servers` (`footer`); the row states of 2.20; the dialog `folder_picker`; the close step `draft`; `window-request`'s `newChat`; Alt+W, Ctrl+Shift+W and the other list keys of 2.20 and 2.23); `src-tauri/src/folderdialog.rs`; `tests/driver/p6_leftlist.py`.
- Needs: TERM's `terminals`, `openTerminal`, `showTerminal` and `askCloseTerminal`; SES's `recent_folders`; VIEW's `dir_list`; P3.9's chat rows (through S5's list sections and row states) and `listmodel.buildRows`; P4.1's `parentOf`, `isClosed` and the `chat.close` action; P3.3's `openChat` and `setCurrentSession`; S5's `window_open`, `closeWindow()` and `app_info`; P3.2's settings (`list_collapsed`); engine A (session records) and B (jobs); gap G-18.
- Done-checks: the row map's checks.

**TERM** (17 rows: see section 1)
- Owns: `src/components/terminal/{TerminalTab,CloseBox}.svelte`; `src/lib/terminal/{tabs.svelte,xtermtheme,editor,keys}.js` and their tests; `src/lib/plugins/terminal.js` (the action `editor.open`; the dialog `terminal_close`; Ctrl+O in the `composer` scope; the `main` scope's terminal keys; the close step `terminals`); `src-tauri/src/{pty,editor}.rs`; `tests/driver/p6_terminal.py`; `tests/fakes/fake-editor` (8.4).
- Needs: S5's `showMain`, `registerCloseStep` and the xterm packages; THM's `theme`; P3.7's `composer.drafts`, `setText` and `composer.mode`; P3.1's `login_env::login_env()`.
- Done-checks: the row map's checks; `r:term.env_clean`, `r:term.editor_args` (each editor of 2.26 step 3).

**SHARE** (20 rows: see section 1)
- Owns: `src/components/remote/{RemoteSetup,RemoteStatus,LauncherPassword}.svelte`; `src/lib/remote/{share,actions,presence,store.svelte}.js` and their tests; `src/lib/plugins/remote.js` (the action `remote.open`; dialogs `remote`, `remote_setup`, `launcher_password`; `onEvent("remote_status")`, `onEvent("remote_action")`; the close step `remote`; the quiet start from `app_info().launch_id` and `window-request`); `src-tauri/src/remote.rs`; `tests/driver/p6_remote.py`; `tests/fakes/phone.py` (8.4); `tests/fakes/seams/remote.json`.
- Needs: KIT; TERM's `openTerminal` (the setup step); P3.7's `setBadge`, `composer.mode` and the actions `yolo.toggle` and `mode.cycle`; P3.3's `ensureSession` and `openChat`; S5's `app_info` and `window-request`; engine D (remote routes and events); gaps G-17, G-20, G-26b; seam G-23 `remote`.
- Done-checks: the row map's checks.

**SYS** (27 rows: see section 1)
- Owns: `src/components/system/{Logs,About,CrashNotice,DebugOverlay}.svelte` (`DebugOverlay` is S5's placeholder); `src/lib/system/{alerts,title,badge,updates,usagereport,focus}.js` and their tests; `src/lib/plugins/system.js` (dialogs `logs`, `about`, `crash`; actions `updates.clis`, `updates.crush`, `usage.report`; the alert listeners of 2.18); `src-tauri/src/{alerts,taskbar,crash,logs,selfupdate,crushinfo}.rs`; `tests/driver/p6_system.py`; `tests/fakes/fake-self-update`; `tests/fakes/seams/system.json`.
- Needs: KIT; ASK's `askYesNo`; P3.3's `version`, `agentSession`, `queuedCount` and sessions store; P3.4's `openLink` (reports and folders); P3.1's `crush_bin::resolve` (self-update, `crush dirs`, `crush stats`); engine B (updates, memory notice claim); gap G-19; seam G-22.
- Done-checks: the row map's checks; `r:sys.sound_chain` (order, one 5 s limit for the whole chain, missing players skipped), `r:sys.crash_report_name`.

**G56** (no rows of its own; it serves the rows above)
- Owns: the Go files named per gap in 9.1, and their tests.
- Must not touch: P4.2's files (`internal/agent/cliagent/fork*.go`, the hook in `internal/agent/cli_turn.go`); no gap needs them.
- Needs: engine A to D merged, with B's follow-ups (merged, `d3aa5502`, `desktop/PLAN.md:410`); G-26 touches `internal/agent` (the `Coordinator` interface).
- Done-checks: each gap's Go test (9.1), then the whole suite through `tests/go-test.sh`.

**Every app piece's done-line** also needs: `pnpm test` and `cargo test` pass; `tests/check.sh` with the piece's driver passes twice in a row (with `--fixture` first where the driver has fixture steps); its engine and two-app steps pass on `desktop` after the merge; the outside check that `check.sh` runs finds nothing changed (8.5).

### 7.4 Interfaces between these pieces

| Interface | Shape | Owner | Used by |
|---|---|---|---|
| `openDialog("auth_method", {providerId, reAuthenticate?, onboarding?, onDone(result)})` | opens the right sign-in sheet for the provider (2.3 steps 2-5); `result` is `"done"` or `"cancelled"` | AUTH | MOD |
| `signIn(platform)`, `signOut(platform)` | `src/lib/signin/accounts.js` (Settings → Accounts, 2.4) | AUTH | SET |
| `capabilities`, `refreshCapabilities()` | `src/lib/models/capabilities.svelte.js`; re-read after `config_changed` | MOD | PAL, LIVE |
| `askYesNo({label, question, description, onAnswer})` | `src/lib/ask/localform.js` → `{close()}`; a yes/no form in the slot with the kind `update`, never sent to the engine (2.19) | ASK | SYS |
| `theme` | `src/lib/themes/current.svelte.js` `{name, dark, revision}`; the tokens themselves are on `:root` | THM | TERM |
| `terminals`, `openTerminal`, `showTerminal`, `askCloseTerminal`, `closeAllTerminals` | `src/lib/terminal/tabs.svelte.js` (2.25) | TERM | LST, SHARE |
| `openDialog("folder_picker", {mode: "session" \| "terminal"})` | 2.21's picker | LST | PAL |
| `runAction("remote.open")`, `openDialog("launcher_password")` | 2.27 | SHARE | PAL, SET |
| `openDialog("theme")` | 2.13 | THM | PAL, SET |
| `runAction("editor.open")` | 2.26 | TERM | PAL |
| `recent_folders(limit)` | 6.4 | SES | LST |
| `dir_list(path)` | 6.4 | VIEW | LST |
| `fuzzyFind`, the kit components | `src/lib/kit/fuzzy.js`, `src/components/kit/*` | KIT | every piece with a sheet |
| `leftList` | `src/lib/leftlist/store.svelte.js` | LST | `App.svelte` (S5) |
| every other palette target | the dialog ids of 2.1 and the action ids of 7.3, by name | each owner | PAL |

### 7.5 File ownership

- Every path in 7.1 and 7.3 is listed once. These paths are disjoint from S3's files and placeholders (3-4 design §5.1), from P3.1 to P3.10 and P4.1 to P4.4 (3-4 design §5.2), and from each other; a script checked this on the text (section 10).
- No phase 5-6 piece edits a file owned by S3, S5, a P3 piece or a P4 piece. Everything such a file needed for phases 5 and 6 is in S5.
- A placeholder that S5 creates belongs to the named piece from then on, as S3's placeholders do (3-4 design §5.1).
- Fake scripts and fixtures carry their piece's folder name as a prefix, so they never collide with phase 3's (`chat-`, `tools-`, `composer-`, `side-`, `list-`, `unsafe-`); any piece may read another's (3-4 design §5.2).

## 8. Tests and harness

The checks use the phases 3-4 harness as S3 builds it (3-4 design §6) and add only what 8.2 lists. Nothing here starts a window on the owner's screen: every app run is inside `tests/sandbox.sh` on its hidden X screen.

### 8.1 What phases 3-4 provide

- `tests/check.sh [--fixture FILE] [--size WxH] DRIVER.py [args]` (S3, 3-4 design §6.1): fails when the app or (without `--fixture`) `tests/out/bin/crush` is missing; runs `DESKTOP_CRUSH=… DESKTOP_ARGS=… tests/sandbox.sh -s WxH tests/run-in-sandbox.sh DRIVER.py args`; runs `tests/outside-check.py` before and after; exits with the driver's code. PASS and FAIL lines and screenshots land in `tests/out/<driver>/`.
- `tests/sandbox.sh` (bubblewrap: the computer read-only except `tests/out/`, the owner's home hidden, its own `/tmp` and `/run` with no session bus and no sound socket, loopback-only network, `HOME=/tmp/home`, the fakes first on `PATH`, `env -i`, a hidden X screen through `xvfb-run`; today `desktop/tests/sandbox.sh:2-41`).
- `tests/run-in-sandbox.sh` (S3): copies `tests/project/` to `/tmp/home/proj`, runs `git init`, writes its `crush.json` with the fake provider's port (models `fake-small`, `fake-large`; `options.notifications: "disabled"`), sets `CRUSH_DESKTOP_CRUSH` and `CRUSH_DESKTOP_ENV_MARK`, starts the fake AI server, then the app with `--probe /tmp/probe.json --cwd /tmp/home/proj $DESKTOP_ARGS`, then the driver.
- `tests/driver/harness.py` (S3): `probe_section`, `probe_heavy`, `word_rect`, `select_words`, `right_click`, `type_text`, `key`, `wait_text`, `engine_get` (curl over the socket), `fake_ai_requests`, `step(row)` (names the screenshot by row ID).
- Fixture mode (S3, 3-4 design §6.3): `--fixture FILE` answers GETs from the file's `get` map, appends every other call to `<probe>.api.jsonl` and plays timed events.
- The fake AI server with scripts and the fake AI tools (P3.10, 3-4 design §6.2); `tests/fakes/fake_mcp.py`, `fake_lsp.py`, `seed_chat.py`; `tests/driver/clip.py` (reads and sets the hidden screen's clipboard).
- The terminal app in a hidden tmux (3-4 design §6.4): `tmux -L crushtest -f /dev/null new-session -d -s tui -x 200 -y 50 "env CRUSH_CLIENT_SERVER=1 $DESKTOP_CRUSH --cwd /tmp/home/proj -s $SID"`, read with `capture-pane -p`, driven with `send-keys`, ended with `tmux -L crushtest kill-server`.
- The probe (3-4 design §1.9): `probeSection(name, light, heavy)`, the fields `clipboard.last` and `opened`, and `<probe>.opened.jsonl`, where `open_link` records every open in probe runs instead of starting a program (`opener.rs`, P3.2).
- `tests/go-test.sh` for Go tests on the committed tree.

### 8.2 What S5 adds

1. **`--no-app`** in `run-in-sandbox.sh` and `check.sh`: the project, the fake AI server and the environment are set up as before, but the app is not started; the driver starts it through `apps.App` with the arguments and environment each check needs.
2. **Per-window probe files.** The first window writes `/tmp/probe.json`; every other window writes `/tmp/probe.json.<label>` (7.1, `commands.rs`). `<probe>.request.json` stays shared: each window answers a heavy request in its own file.
3. **`tests/driver/apps.py`**, used by every `p5_*` and `p6_*` driver next to `harness.py`:
   - `App(args=(), env={}, probe="/tmp/probe.json")` starts `$DESKTOP_APP --probe <probe> --cwd /tmp/home/proj <args>` with run-in-sandbox's app environment plus `env`, its output in `tests/out/<driver>/app-<n>.log`; `.window(label)` reads that window's probe file; `.stop()`; `.wait_exit(timeout)`.
   - `second_project()` makes `/tmp/home/proj2` the way run-in-sandbox makes `/tmp/home/proj` (same template, same fake provider), for the different-project legs.
   - `config(options)` merges keys into the project's `crush.json` before the app starts (for example a notification style other than `"disabled"`).
   - `Term(name="tui", cwd="/tmp/home/proj", args=())` uses 3-4 design §6.4's command on `tmux -L crushtest`; `.keys(*keys)`, `.text()` (`capture-pane -p`), `.sgr()` (`capture-pane -p -e`, for colors), `.wait_text(text, timeout)`, `.stop()`; the driver ends with `tmux -L crushtest kill-server`.
   - `Bus()` starts `dbus-daemon --session --fork --print-address=1 --address=unix:path=/tmp/crushtest-bus` and `notify_catcher.py` on it; `.env` gives `DBUS_SESSION_BUS_ADDRESS`; `.notifications()` reads `/tmp/crushtest-notifications.jsonl`.
   - `focus_away()` opens a 10×10 GTK window (Python `gi`, present in `/usr`) and focuses it with `xdotool windowfocus`, so the app's window loses focus; `.close()` ends it.
   - `calls(name)`: the `$FAKE_CMD_LOG` lines of one stand-in; `alerts()`: the lines of `<probe>.alerts.jsonl` (notifications and taskbar calls, 6.4); `opened()`: the lines of `<probe>.opened.jsonl`; `api_log()`: the lines of `<probe>.api.jsonl` in fixture runs.
   - `seams(dict)` writes `/tmp/crushtest-seams.json` and returns `{"CRUSH_TEST_SEAMS": <path>}` for `App` and `Term` (G-21 to G-24).
   - Clipboard checks use `clip.py` (P3.10) and the probe's `clipboard.last`, as phase 3's do.
4. **`tests/fakes/notify_catcher.py`**: owns `org.freedesktop.Notifications` on the private bus and answers `GetCapabilities` (`["body"]`), `GetServerInformation` (`"crushtest-catcher", "crush-tests", "1", "1.2"`), `Notify` (appends `{app_name, summary, body, at}` to `/tmp/crushtest-notifications.jsonl` and returns a growing id) and `CloseNotification`. It shows nothing and plays nothing. Python with `gi` (Gio).
5. **`tests/fakes/fake-cmd`** and its links `tests/fakes/bin/{canberra-gtk-play,pw-play,paplay,docker,xdg-open,x-www-browser,www-browser}` (7.1): every sound player, `docker` and browser-opener call is recorded, nothing plays and no browser opens; `FAKE_CMD_FAIL` walks the sound chain; `FAKE_CMD_STDOUT_DOCKER` makes `docker mcp version` answer.
6. **`tests/build-crush.sh -tags crushtest`** (7.1), for the seams of G-21 to G-24.
7. **`tests/driver/p5_selftest.py`**: the harness self-test (8.6).

Harness files the pieces own (7.3): `tests/fakes/fake_mcp_extra.py` (EXT), `tests/fakes/phone.py` (SHARE), `tests/fakes/fake-self-update` (SYS), `tests/fakes/fake-editor` (TERM), `tests/fakes/seams/<folder>.json` (MOD, AUTH, EXT, LIVE, SHARE, SYS), and each piece's driver, fixtures and scripts.

**Probe sections.** Each piece adds one section named by its folder (`kit`, `palette`, `models`, `signin`, `sessions`, `ask`, `extensions`, `settings`, `themes`, `pictures`, `live`, `leftlist`, `terminal`, `remote`, `system`) with `probeSection` (S3). Light fields hold what the checks read: the open sheet's id, rows, buttons and help pairs; the slot kind; the tray entries; list rows and their states; terminal tabs with the last 30 lines of text; the applied theme's name and five sampled tokens. Rects are heavy fields. A section never holds a secret value, a key, a token, or a config value other than the ones a check names (SET-035).

### 8.3 Commands

All commands run from the piece's worktree (`<WT>`) in the background through `crush bg`, inside `systemd-run` on CPUs 6-11 and 18-23 with a time limit, exactly as 3-4 design §6.1 writes them. The ones this design adds:

```sh
# Build the engine with the test seams (S5's build-crush.sh)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=900 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/build-crush.sh'

# One piece's driver against the engine (the driver starts the app itself)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=1200 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/check.sh --no-app p5_palette.py'

# One piece's driver against a fixture (no engine)
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=600 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/check.sh --fixture tests/fixtures/palette-entries.json p5_palette.py'

# The harness self-test
crush bg -- 'systemd-run --user --wait --pipe --collect -p AllowedCPUs=6-11,18-23 -p RuntimeMaxSec=600 --working-directory=<WT>/desktop -E PATH="$PATH" bash tests/check.sh --no-app p5_selftest.py'

# Go tests for G56 (committed work only)
crush bg -- 'bash <WT>/desktop/tests/go-test.sh ./internal/...'
```

Unit and Rust tests, the app build and the look checks use 3-4 design §6.1's commands unchanged. A driver takes check names as arguments to run only those (`p5_palette.py summarize toggle_yolo`) and runs them all by default. Every check writes its screenshots and logs to `tests/out/<driver>/` (one screenshot per checklist row, named by row ID) and nowhere else.

### 8.4 Stand-ins per piece

| Piece | Stand-in | How the check uses it |
|---|---|---|
| all `x:` checks | `Term` on `/tmp/home/proj`, and on `/tmp/home/proj2` for the different-project legs | the terminal app shares the throwaway home and, per project, the engine with the desktop app; its screen is read with `capture-pane` |
| SYS (NOTE rows) | `Bus` with the catcher; the sound players' stand-ins; `focus_away()`; `alerts()` | notifications are catcher lines, sounds are `calls()`, badge, progress and attention are taskbar lines |
| SYS (UPD rows) | seams `cli_updates` and `crush_release` (G-22); `fake-self-update` through `CRUSH_DESKTOP_SELF_UPDATE_BIN` (honored only with `--probe`) | offers appear and their answers reach the engine or the fake updater, with no network |
| SYS (MISC-017) | the `xdg-open` stand-in (S5, 7.1) | `crush stats` writes the report and `calls("xdg-open")` holds its path; no browser opens |
| SYS (MISC-025, START-020) | `CRUSH_DESKTOP_TEST_PANIC=<ms>` (honored only with `--probe`): the app panics after that many milliseconds | the report file appears and the next start shows the notice |
| LIVE | scripts `live-*.json` whose bash tool calls run `crush spawn …`, `crush bg -- …` and the bash tool's `run_in_background` (`internal/agent/tools/shell_types.go:7`); a listening job `crush bg --service -- 'python3 -m http.server 0'`; seam `memory` (G-21) | the engine's shell environment carries the tasks folder (`internal/agent/turn.go:192-199`), and the sandbox puts the test build's `crush` on PATH (the app drops `CRUSH_DESKTOP_CRUSH` from the engine's environment on purpose, `login_env.rs:18`; S5's step 6 found the earlier guess wrong), so helpers and jobs start in the engine as a real agent starts them |
| MOD (Usage) | seam `limits` (G-24) | windows, resets and refresh |
| AUTH | seams `auth.verify_key` and `auth.flows` (G-24); `opened()`; `clip.py` | each sign-in state with no network; the browser open is an `opened.jsonl` line |
| EXT | `fake_mcp_extra.py`: an MCP server in stdio mode (one prompt with two arguments, one tool) and in HTTP mode with `--needs-auth` (401 with an authorization URL on `127.0.0.1`, then success after the driver fetches the URL), added to the project's `crush.json` with `config()`; the `docker` stand-in; seam `skills_search` (G-24) | MCP toggles, prompts and sign-in; the Docker entries; skills.sh results |
| SHARE | seam `remote` (G-23): the share listens on `127.0.0.1`, and the fake tailnet names the user `tester@example.com` and the computer `crushtest-pc`; `phone.py` connects, reads the event stream, posts actions and records the answers | REM rows with no Tailscale and no real phone |
| SHARE (REM-017) | while the app runs with a `Bus`, the driver runs `App(args=("--project", "/tmp/home/proj2"), env={"CRUSH_REMOTE_LAUNCH": "crushtest-launch-1"}, probe="/tmp/probe2.json")` | the second process hands over and exits; a second window opens on `proj2` and shares quietly, and `phone.py` sees `launch_id` `crushtest-launch-1`. Then the same call with `crushtest-launch-2`: no third window opens, and the `proj2` window, already sharing, reports `crushtest-launch-2` (G-26b) |
| LST (Browse…) | `CRUSH_DESKTOP_TEST_PICK_FOLDER=/tmp/home/proj2` (honored only with `--probe`) | the picker gets that folder with no dialog |
| TERM | `SHELL=/bin/sh` in the app's environment; `VISUAL=<WT>/desktop/tests/fakes/fake-editor`, which writes "edited at <line>:<column>" into the file it is given and exits; the `terminal` probe section | typed commands, their output and the tab's folder; the editor's result in the typing box |

### 8.5 What makes a check fail

**Every check** fails when its row's behavior, as section 2 and the checklist row state it, does not happen; when a text differs from the cited terminal text (or the 9.4 difference); when the probe does not reach the expected state within 5 s (2 s for two-app legs, section 4); when the app logs a panic; or when the outside check finds a change.

**Two-app checks** (`x:`) fail when the other app's screen does not show the change within 2 s, in either direction; for the four-leg checks of section 4, on the same project or on the other project. `x:lst.window_close_q2` fails when closing the desktop window stops a helper or job of a chat the terminal still shows, or leaves running one that no app shows after the 10 s grace. `x:ask.form_replaces_composer` fails when the second app's form stays open after the first app answers. `x:live.stop_kill_client_parity` fails when a helper stopped in one app still shows as running in the other after the next refresh.

**Specific failure conditions:**

| Check | Fails when |
|---|---|
| `w:sys.focus_rule` | a notification reaches the catcher while the window is focused and shows the chat; none arrives within 2 s after `focus_away()`; a sound is missing while focused |
| `w:sys.backends` | with `disabled`, any catcher line or player call appears; with `bell`, a catcher line appears or no player call with the sound id "bell"; with `auto`, `native` or `osc`, no catcher line |
| `w:sys.sounds` | the first player call is not `canberra-gtk-play --id <sound> --description Crush`; with `FAKE_CMD_FAIL=canberra-gtk-play`, the next call is not `pw-play /usr/share/sounds/freedesktop/stereo/<sound>.oga`; a later player is called after one succeeded |
| `w:sys.finished_alert`, `w:sys.notification_kinds` | a catcher line's summary or body differs from 2.18's table; a finished alert arrives while the chat is busy or has queued prompts |
| `w:sys.badge_status`, `w:sys.taskbar_progress`, `w:sys.focus_clears` | the last taskbar line's `badge` is not the number of windows blocked, done or in error; `progress` is not `indeterminate` exactly while a window works; focus does not clear done and error |
| `w:sys.hidden_chat_notifies` | a finished turn of a chat no window shows produces no catcher line, or two (one per window of the project) |
| `w:live.memory_warning` | the notice line lacks the seam's `message` while high; two catcher lines arrive for one episode across two windows; a line arrives with the style `disabled` |
| `w:sys.crash_notice` | no `crash-desktop-*.log` with mode 0600 in `/tmp/home/.local/state/crush/crashes`; the next start shows no notice; "Open report" adds no `opened()` line with the report path |
| `w:ask.secure_*`, `w:auth.key_*`, `w:share.launcher_password` | the typed secret (a fixed marker string) appears in any probe file, `<probe>.api.jsonl`, `opened.jsonl` or `alerts.jsonl`, the app log, the engine log, the fake AI log, `$FAKE_TOOL_LOG`, `$FAKE_CMD_LOG`, the tmux screen or a process's arguments (`/proc/*/cmdline`) |
| `w:ask.secure_write_on_submit` | the target file exists before submit, or after cancel |
| `w:term.env_shown` | `env` typed in a tab prints any name 2.25 removes, or lacks `TERM=xterm-256color` and `COLORTERM=truecolor` |
| `w:term.close_kinds` | a tab's shell still runs 6 s after its window closed |
| `w:term.theme_follow` | after a theme switch, the tab's background or an ANSI color sample differs from the new tokens one frame later |
| `w:share.tailnet_only` | the share answers on an address the seam did not name |
| `w:share.phone_actions` | an action answer differs from 2.27's texts, or comes later than 25 s |
| `w:share.launcher_desktop_window` | no second window on `proj2`; the share's `launch_id` is not `crushtest-launch-1`; a later window reuses it; after the second launch for `proj2`, a third window opens, or the share does not report `crushtest-launch-2` within 2 s |
| `w:lst.spinner` | two working marks in two windows show different frames in the same screenshot |
| `w:mod.models_list_matches_cli` | the picker's model set differs from `crush models --json` run in the sandbox on the same home |
| `w:ses.cli_parity`, `w:ses.projects_cli_parity` | the Sessions sheet's chats or the Open Project rows differ from `crush session list --json` or `crush projects --json` |
| `w:sys.debug_overlay` | with `CRUSH_UI_DEBUG=true`, the square's color does not change after a page change, changes without one, or takes a click |
| `w:thm.*`, `x:thm.*` | as 3.8 |

### 8.6 Harness self-test (`p5_selftest.py`)

S5's check 3. It passes only when all of these hold, in one sandbox run:

1. Without `Bus`, `DBUS_SESSION_BUS_ADDRESS` is unset and a Gio session-bus connection fails; `App()` still starts and connects (the single-instance plugin then has no bus to use; inferred harmless, and this step proves it).
2. With `Bus`, one Gio `Notify` call produces exactly one catcher line with the summary and body sent.
3. `command -v` finds `canberra-gtk-play`, `pw-play`, `paplay`, `docker`, `xdg-open`, `x-www-browser` and `www-browser` in `tests/fakes/bin/`; each call adds one `$FAKE_CMD_LOG` line; with `FAKE_CMD_FAIL=paplay`, `paplay` exits 1; `FAKE_CMD_STDOUT_DOCKER=x` makes `docker` print `x`.
4. `Term` starts `tests/out/bin/crush`, and `capture-pane` shows its screen within 10 s.
5. A seams file with a `memory` seam makes `GET /v1/memory` report `high: true` on the crushtest build.
6. A fake script whose bash step prints `$CRUSH_TASKS_DIR`, `$CRUSH_DESKTOP_CRUSH` and `command -v crush` shows the tasks folder set, `CRUSH_DESKTOP_CRUSH` empty (the app hides it), and `crush` resolving to the sandbox's build (LIVE's scripts rely on it, 8.4).
7. `window_open` for `/tmp/home/proj2` opens a second window, and `/tmp/probe.json.w2` shows its `connection.state` as `connected`, while `/tmp/probe.json` still shows the first window's project.
8. A check written to fail (`--broken`) is reported as a failure.
9. The outside check that `check.sh` runs finds nothing changed.

### 8.7 Phase 3-4 window steps these drivers run

The phases 3-4 design defers these window steps to surfaces built here (3-4 design §6.5); the rows stay that design's, and the coordinator counts their window step as passed when the step below passes:

| Row (3-4 design) | Window step here | Driver |
|---|---|---|
| CHAT-010, CHAT-072 | a question form opens in the slot and its answer reaches the engine | `p5_ask.py` |
| TYPE-007 | sending "exit" or "quit" opens the quit sheet (2.24) | `p5_palette.py` |
| START-001 | onboarding opens the model picker (2.3) | `p5_models.py` |
| CHAT-009, CHAT-062 | a background job shows in the tray and in Background Processes (2.15) | `p6_live.py` |
| CHAT-061 | Ctrl+B moves the running command to the background (2.15) | `p6_live.py` |

TYPE-015 and START-020 are rows of this design now (2.26, 2.28; section 1's last block). TYPE-031 to TYPE-034 stay deferred until the owner's terminal work is merged, and SIDE-013's Hyper credits line stays hidden until the engine has a route (G-25); neither is built here.

## 9. Engine gaps, risks, open questions

### 9.1 Engine gaps (G56)

Each gap names the rows that need it, what is missing (read at `40405abf` and again at `65e4250d`; G-26 and G-26b at `773344e0`), and the smallest addition. New Go files where possible. The JSON names are this design's contracts (6.5, 6.7): G56 builds them as written, and a change goes through the coordinator, who updates 6.5 and 6.7 with it. Every gap's Go test is a G56 done-check.

| Gap | Rows | Missing today | Smallest addition | Go test |
|---|---|---|---|---|
| G-1 | the sync rows of section 4; THEME-025 | An engine reloads its config only after its own writes; `ConfigStaleness` exists but only the `crush_info` tool calls it (`internal/config/load.go:188-217`; `internal/config/store_staleness.go:25-74`) | `internal/backend/config_watch.go`: once a second per engine, compare `ConfigStaleness()` (`store_staleness.go:29`) and the themes folder's entries (name, size, modification time); on a change, `ReloadFromDisk` (`internal/config/load.go:130`) and `config_changed` to every workspace of the engine | `TestConfigWatchPublishesOnOutsideWrite`, `TestConfigWatchSeesThemeFile` |
| G-2 | THEME-001, THEME-004, THEME-008 to THEME-082, SET-009 | No theme routes: the terminal reads, resolves and writes themes in its own process (`internal/ui/model/theme.go:154-399`); no `scholars-edition` or `charcoal-amber` | the routes and `ThemeView` of 3.2 (`internal/rpc/routes_themes.go`, `internal/server/handlers_themes.go`, `internal/backend/themes.go`, `internal/proto/theme.go`); the two palettes and the edition's override entry of 3.7 in `internal/ui/styles/themes_desktop.go`, listed in `builtinThemes` and `builtinThemeOverrides` (`internal/ui/styles/themes.go:460-467`; the override map holds Charmtone's and Gruvbox's entries, `:138-154`, `:240-246`). No route reads or writes a theme choice, except that a rename moves the terminal's `options.tui.active_theme` with its file, as the terminal's own rename does (3.2) | `TestThemeRoutesWriteTerminalBytes` (a saved file equals `SaveThemeFile`'s bytes), `TestThemeResolveMatchesLoadTheme`, `TestRenameMovesTerminalActiveTheme`, `TestDeleteThemeChangesNoSetting`, `TestDesktopBuiltinsResolve` |
| G-3 | SET-006, SET-007, THEME-025, TYPE-002 (terminal legs) | The terminal's client refreshes its config silently (`internal/workspace/client_events.go:291-293`; `internal/workspace/client_workspace.go:111-124`). `scrollbar` and `compact_mode` are copied at start (`internal/ui/model/ui.go:439`, `:517`). The theme's colors are loaded at start (`internal/ui/common/common.go:143-156`) and afterwards only by the terminal's own theme actions (`internal/ui/model/theme.go:173-201`, `:203-260`, `:262-297`, `:370-399`); `ui.go:490-495` only seeds the theme key. The mode mark changes only with the terminal's own switches (`internal/ui/model/input_mode.go:62-114`) | after `refreshWorkspace`, send a config-refreshed message to the UI; `internal/ui/model/config_refresh.go` re-applies the two options and reloads the theme when `active_theme` names another theme (a hand edit, or G-2's rename) or the theme's file revision changed; each tab's status to the session host carries the theme name and revision, so the host's list follows (`internal/ui/sessionhost/host.go:110-129`). The terminal's own mode mark follows too: on the same message it reads `GET …/agent` (`internal/client/agent.go:16`) and sets its input mode from `agent` (G-26: `plan` → plan mode, `coder` → code mode), redrawing the prompt with `setEditorPrompt` (`internal/ui/model/editor.go:197-211`), without a banner and without calling `AgentSetMain` | `TestConfigRefreshReappliesTheme`, `TestConfigRefreshReappliesCompactAndScrollbar`, `TestConfigRefreshFollowsMainAgent` |
| G-4 | CMD-035, DLG-235 | `SetPermissionsSkip` and the two MCP toggles publish nothing (`internal/backend/permission.go:20-27`; `internal/backend/ops_config.go:151-169`) | call the existing `configChanged` hook after each (`ops_config.go:18-26`). The main-agent switch is G-26 | `TestSkipAndMCPTogglesPublishConfigChanged` |
| G-5 | DLG-041 to DLG-070, MISC-012 | No sign-in route; the flows run in the terminal process (`internal/ui/dialog/auth/oauth.go:613-633`) | the routes and the `auth_flow` event of 2.4 over `internal/login` (`login.go:49-66`, `:125-138`), unix socket only, never logged: `internal/backend/auth_flows.go`, `internal/server/handlers_auth.go`, `internal/rpc/routes_auth.go`, `internal/proto/auth.go`, one payload type in `internal/pubsub/events.go` | `TestAuthFlowSavesLikeCrushLogin`, `TestVerifyKey`, `TestAuthRoutesRefuseTCP` |
| G-6 | DLG-046 | The workspace record has no config path (`internal/proto/proto.go:15-34`) | `config_path` (the global data config file) on `proto.Workspace`, filled by the server | `TestWorkspaceCarriesConfigPath` |
| G-7 | CMD-057, CMD-058, DLG-258 to DLG-262 | No route lists custom commands; the terminal loads them in its own process | `GET …/commands` with the JSON of 2.2, from `commands.LoadCustomCommands` (`internal/commands/commands.go:60`) and the user skills | `TestCommandsRouteMatchesLoader` |
| G-8 | CMD-024 to CMD-026; palette entries 11 to 13 | `proto.AgentCapabilities` has only `kind` and `can_background` (`internal/proto/live.go:73-77`) | add `supports_fast`, `supports_ultracode`, `supports_thinking_toggle` from `config.SupportsFastMode`, `SupportsUltracode`, `SupportsThinkingToggle` for the coder model | `TestCapabilitiesReportModelSupport` |
| G-9 | MISC-019 | No route runs `crush update-providers` | `POST /v1/providers/refresh` → `{results: [{source, ok, error}]}`, the same code, then a reload | `TestProvidersRefreshRoute` |
| G-10 | DLG-075 | The `re_authenticate` agent event carries no provider id over the server (`internal/ui/model/agent_events.go:195-209`) | `provider_id` on `proto.AgentEvent` for that type | `TestReAuthenticateCarriesProvider` |
| G-11 | DLG-076, MISC-024 | Listing saved sessions deletes the workspace's empty drafts older than 5 minutes, even one another app is showing (`internal/sessioncatalog/catalog.go:89`, `:111-118`) | skip drafts with `attached_clients > 0` | `TestDraftCleanupSkipsShownDrafts` |
| G-12 | DLG-141 | No route returns a pending question request, and missed events are not replayed | `GET …/questions/pending?session_id=` → `proto.QuestionRequest`, or 404 `no_pending_question` | `TestPendingQuestionRoute` |
| G-13 | DLG-163 | `proto.QuestionItem` lacks `selected` (`internal/proto/proto.go:234-241`); the domain type has it (`internal/question/question.go:61-63`) | add `selected` (`json:"selected,omitempty"`) and copy it | `TestQuestionItemCarriesSelected` |
| G-14 | DLG-203 to DLG-208 (Browse) | The skills.sh search runs in the terminal process (`internal/skills/directory.go:39-47`) | `GET …/skills/search?q=` with the same JSON (`directory.go:23-28`) | `TestSkillsSearchRoute` (G-24 seam) |
| G-15 | — | withdrawn in pass 2: the Changes sheet is the phases 3-4 design's, and it reads the existing `GET sessions/{sid}/messages/{mid}/review` (its §2.8) | — | — |
| G-16 | SET-004, CMD-055 | The terminal pins its own process tree (`internal/cpuaffinity/cpuaffinity_linux.go:24-46`); agents now run in the engine | `POST /v1/cpu-affinity` `{enabled}` → `{cpus}`, errors in the terminal's texts; the engine applies the saved setting at start and again after `config_changed` | `TestCPUAffinityRoute` (Linux) |
| G-17 | REM-015, REM-017 | The launcher always runs the terminal command (`internal/remote/launcher.go:77-101`) | `options.remote_launch_target` (`"terminal"` default, `"desktop"`), read at each launch; `"desktop"` runs `crush-desktop --project <home>` with `CRUSH_REMOTE_LAUNCH=<id>` | `TestLauncherTargetDesktop` |
| G-18 | LIST-028 to LIST-032, LIST-047; LIVE-001 (servers leave the tray) | Which jobs listen on ports is found in the terminal process (`internal/agent/servers.go:36-61`) | `GET …/servers` → `[{port, program, name, command, job_id, pid, session_id}]` for every chat of the project | `TestServersRoute` |
| G-19 | UPD-005 to UPD-007 | `crush self-update` has `merge`, `build` and `install`, no AI-fix step, and `merge` stops on clashes (`internal/cmd/self_update.go:51-73`) | `crush self-update run --tag <tag> --json`: the whole background run of 2.19 with the AI fixes, as the terminal does in its own process (`internal/ui/model/selfupdate.go:142-170`); `--json` output for `install` | `TestSelfUpdateRunJSON` (a fake checkout) |
| G-20 | REM-016 | `crush remote password` prompts on the terminal (`internal/cmd/remote.go:65-75`) | `--stdin`: two lines from standard input, the same checks and texts | `TestRemotePasswordStdin` |
| G-21 | LIVE-018, LIVE-019 (checks) | A real engine cannot be made to report high memory | seam `memory` `{percent, top: [{name, bytes}], total_ram_bytes}` replacing the readers of `memwatch.Default` (`internal/memwatch/memwatch.go:141`) | `TestSeamMemory` |
| G-22 | UPD-001 to UPD-008 (checks) | The update checks need the network (`internal/cliupdate/cliupdate.go:56`, `:105`, `:179`) | seams `cli_updates` `[{name, bin, path, current, latest, install: "ok"\|"<error>"}]` and `crush_release` `{tag, current, dir, staged}`, the latter through the existing `SetCrushUpdateSourceForTest` (`internal/backend/live_updates.go:143-146`) | `TestSeamUpdates` |
| G-23 | REM-001 to REM-019 (checks) | The share needs Tailscale (`internal/remote/tailnet.go:20-46`) | seam `remote` `{listen: "127.0.0.1", host, user, phones}`: a fake `tailnetAPI` (`tailnet.go:42-46`) that names the loopback address as this computer's tailnet address | `TestSeamRemoteLoopback` |
| G-24 | DLG-281 to DLG-287, DLG-041 to DLG-070, DLG-203 to DLG-208 (checks) | Usage limits, sign-in and skills.sh need real accounts or the network | seams `limits` (through the existing `SetLimitFetchersForTest`, `internal/agent/cliagent/limits.go:108-111`), `auth` `{verify_key: {<provider>: "ok"\|"<error>"}, flows: {<platform>: {url, user_code, result, after_ms, code, late}}, timeout_ms}` (`code`: the sign-in takes a pasted code and finishes when it is sent; `late`: a cancelled or timed-out sign-in still hands over a token, to prove it is not saved; `timeout_ms`: a shorter sign-in time limit; saving fetches no model catalog) and `skills_search` `{<query>: [results]}` | `TestSeamLimits`, `TestSeamAuth`, `TestSeamSkillsSearch` |
| G-25 | SIDE-013's Hyper credits line (the phases 3-4 design's row, hidden until this route exists: its O3, `desktop/PLAN.md:309`) | The terminal fetches the credits in its own process: an OAuth refresh when the token expired, then `hyper.FetchCredits`, every 60 s while idle (`internal/ui/model/polling.go:13-15`, `:38-82`) | `GET /v1/hyper/credits` → `{credits}` (the value `hyper.FetchCredits` returns; `null` when Hyper is not configured), running the terminal's sequence in the engine; the line itself is turned on by a coordinator change to phase 3's files once this merges | `TestHyperCreditsRoute` (a fake Hyper server) |
| G-26 | TYPE-002 and SIDE-025 in the other app (plan mode per project, section 4) | A main-agent switch publishes nothing: `Ops.SetMainAgent` only checks and switches (`internal/backend/ops_agent.go:76-87`; `internal/agent/coordinator.go:240-250`). `proto.AgentInfo` names no agent (`internal/proto/proto.go:122-128`), `GetAgentInfo` fills none (`internal/backend/agent.go:157-175`), and the `agent.Coordinator` interface has no getter for the name the coordinator keeps (`internal/agent/coordinator.go:40-64`, `:224-232`) | `agent` on `proto.AgentInfo` (`json:"agent"`: `"coder"` or `"plan"`, the names of `internal/config/agents.go:8-9`; `""` while no coordinator runs), filled by `GetAgentInfo` (`GET …/agent`, `internal/rpc/routes.go:95`) from a new `MainAgentName() string` on `agent.Coordinator` (the name `activeAgent` returns). After a successful switch, `Ops.SetMainAgent` calls the existing `configChanged` hook (`internal/backend/ops_config.go:18-26`), so every app on the project gets `config_changed` and re-reads its agent info (3-4 design §1.5.3). A refused switch (busy, unknown agent) publishes nothing | `TestMainAgentSwitchPublishesAndNamesAgent` (server, two apps on one project: after `POST …/agent/main` with `plan`, both apps' streams get `config_changed` and `GET …/agent` answers `"agent": "plan"`; a switch refused as busy sends no event) |
| G-26b | REM-017: a phone launch for a folder whose window already shares (2.27) | A start from the owner of a running share answers that share's status and drops the new `launch_id` (`internal/backend/share.go:259-266`). The hub takes its launch ID only when the share starts (`internal/remote/remote.go:175`) and reports it as `Share.Launch` (`internal/remote/hub_state.go:224-234`); `backend.ShareServer` has no call that changes it (`internal/backend/share.go:130-143`) | `POST …/remote/start` from the owner of a running share with a new `launch_id` sets the share's reported launch to it and answers `on`: `SetLaunch(id string)` on `remote.Server` (it sets the hub's `launch` and wakes the hub, as `setPresence` does, `internal/remote/hub.go:154-162`, so connected phones get the changed `share` field) and on `backend.ShareServer`; `StartRemote` calls it on a running share when `launch_id` is not empty, then answers `running.status()`. An empty `launch_id` changes nothing | `TestRemoteRestartUpdatesLaunch` (backend, fake share: the second start answers `on` from the same share, starts no second share, and the share reports the new ID; an empty ID keeps the old one) |

**The seams (G-21 to G-24).** One file with the build tag `crushtest`, `internal/cmd/seams_crushtest.go`: when `CRUSH_TEST_SEAMS` names a JSON file, `crush` reads it once at start (the engine and the terminal app alike) and installs each seam it names; `internal/cmd/seams_other.go` (no tag) does nothing. Packages without a setter yet (`memwatch`, `cliupdate`, `remote`, G-5's flows, G-14's search) get one exported `…ForTest` function each, like the existing ones (`internal/config/memory_limit_save.go:83-85`; `internal/backend/share.go:173-175`). Release builds never carry the tag. Go test: `TestSeamsIgnoredWithoutTag`.

### 9.2 Risks

1. **`projects.json` race.** The terminal writes the file in place (`internal/projects/projects.go:75`); the desktop writes a temp file and renames it (2.5). Two apps saving or removing projects at the same moment can lose one change; the last writer wins. Mitigation: the desktop re-reads right before each write. No change to the terminal is planned.
2. **Two Crush updates at once.** "Crush is already updating" is kept per process (`internal/ui/model/selfupdate.go:60-65`) and `internal/selfupdate` takes no lock, so the terminal app and the desktop app can both start an update of the same checkout. G-19 could add a lock file both apps honor; it is listed here, not designed, because the terminal's in-process run would need the same change.
3. **Editors that return at once** (`code` without `--wait`) give the text back unchanged, as in the terminal (2.26).
4. **ANSI colors are not contrast-adjusted** (3.6): a theme with dark ANSI colors on dark paper is as hard to read in a terminal tab as in the terminal.
5. **Derived tokens of edited built-ins.** The exact table of 3.1 rule 3 holds only while the roles a token derives from keep the built-in's values; editing one of them moves its derived tokens to the formulas, which differ by a few levels (3.1).
6. **An engine with two workspaces.** When one workspace shuts down, `app.go` stops every background shell and MCP client of the whole engine (`desktop/PLAN.md:335`). Workspaces are shared by path, so this matters only if one engine holds two workspaces; `x:lst.window_close_q2` would catch it.
7. **Timing of two-app legs.** G-1 checks once a second, so a cross-project change shows in 1 to 2 s; on a busy machine a leg can pass 2 s (busy-machine timing failures are recorded in `desktop/PLAN.md:330-331`, `:357`). S5's self-test measures the delay; the limit changes only with the coordinator's approval.
8. **Dependencies.** `@xterm/xterm`, its two add-ons, `portable-pty` and three Tauri plugins (dialog, single instance, notification; 7.1) add code to review and keep current. The pins are the current releases on 2026-10-18; a pin that does not build against S3's lock (for example a newer `tauri` minimum) changes only with the coordinator's approval.
9. **Single instance without a session bus.** On Linux the single-instance plugin hands a second process's arguments over the session D-Bus (inferred from the plugin's Linux backend). Without a bus, a second process cannot hand off and runs as its own app: the launcher's window still opens and shares, from that second process. `p5_selftest.py` step 1 proves that the app starts without a bus (8.6).
10. **A theme the terminal renamed or deleted.** The desktop's `theme` key can then name a theme that no longer exists. THM shows the edition, as the terminal falls back to its default (`internal/ui/styles/themes.go:534-545`), and keeps the key until the user picks again (inferred fallback).
11. **The first frame without a cache.** On a first start, or after the web view's storage is cleared, app.css's edition draws the first frame and the saved theme follows once THM's plugin reads it (3.3): one visible switch when the saved theme is not the edition.
12. **Day groups in the live list.** The chat list's day groups are the first screen's markup (`desktop/src/components/ChatList.svelte:29-31`), which P3.9 fills from live data. If P3.9's list drops them, LIST-010's check fails, and the fix is a coordinator change to `ChatList.svelte` (7.1, "Frozen after S5").
13. **Phase 3 files named before they exist.** S5's swaps for START-021 and the list's search box name `sessionid.js` and `ChatList.svelte` from the phases 3-4 design's rows; if P3.9 put that code in other files of its own, S5 changes those instead (7.1).

### 9.3 Open questions

None. The coordinator answered pass 2's three (`desktop/PLAN.md:414`, `:418`):

1. **Look proposals.** Accepted as written (section 5), the working icon's stepping arc in `--busy` (5.2, LIST-013) and the warning icon's "!" in `--on-amber` (3.6) included.
2. **A phone launch for a folder a window already shows.** The app focuses that window, and the window starts sharing again with the new launch ID, which the engine reports on its running share (2.27; G-26b; 9.4, D22). The other option, always opening a second window for that folder, was not taken.
3. **Plan mode in the other app.** The other app's mode mark follows the switch: G-26 publishes `config_changed` after a main-agent switch and names the main agent in `proto.AgentInfo`; the desktop's mode mark follows it through S5's change to `composer.svelte.js` (7.1), and the terminal's through G-3 (section 4, 9.1).

### 9.4 Differences from the terminal

| ID | Where | Terminal | Desktop | Why |
|---|---|---|---|---|
| D1 | every text field (2.0 rule 7) | key messages and terminal paste messages are separate | Ctrl+V, Shift+Insert and middle-click paste everywhere | a desktop field has one paste |
| D2 | sheets (2.0 rule 8) | dialog text cannot be selected | static sheet text is selectable and copyable | DLG-182 asks for copy |
| D3 | palette entry 42, Ctrl+, (2.2, 2.13) | no settings screen | a Settings sheet | SET-031; PLAN section 10 |
| D4 | Ctrl+K (2.2, 2.5) | moves up a line in the chat and deletes to the line's end in the typing box (`internal/ui/model/keys.go:248-251`; 3-4 design §1.8) | opens the Sessions sheet with the cursor in its filter | PLAN.md:103 names Ctrl+K the Sessions search; the left list's search box shows "⌘K" (`desktop/src/components/ChatList.svelte:24`) |
| D5 | Settings → Accounts (2.4) | `crush login` prints its own lines | info "Signed in to <name>" | the sign-in runs without a model choice |
| D6 | secure paste (2.7) | a 500 ms clipboard read | the paste event's data, read at once | the browser gives the data with the event |
| D7 | secure forms (2.7) | a lone terminal never sees "answered elsewhere" | the desktop form closes when the terminal app answered | both apps get the form (engine design 5.8) |
| D8 | Toggle MCPs (2.9) | the list clamps without scrolling | the list scrolls | normal desktop list behavior |
| D9 | style `auto` (2.18) | off where the terminal sends no focus reports | always notifies | a window always knows its focus |
| D10 | New chat (2.21) | stops language servers and background processes | both keep running | they belong to the whole project in the shared engine (PLAN section 15) |
| D11 | terminal tabs (2.25) | — | also remove `CRUSH_REMOTE_LAUNCH` | a `crush` in a tab must not take the launcher's ID |
| D12 | crash notice (2.28) | "Crush crashed. If metrics are enabled, we were notified about it. …" | title "Crush Desktop crashed"; body without the first two sentences | the desktop sends no metrics (SET-032) |
| D13 | themes (3.2-3.5) | one theme setting, `options.tui.active_theme`; default charmtone-panther: "current" when unset, the base of a new theme, the fallback after deleting the active theme; provider theme swap | its own `theme` key in the desktop settings file, never shared with the terminal; default `scholars-edition` in the same three places; no provider swap | PLAN Q9; coordinator decision, `desktop/PLAN.md:377` |
| D14 | text selection (3.1) | several selection colors | one: `--selection` behind text that keeps its own color, terminal tabs included | one look for one action |
| D15 | Skills sheet title (2.8) | "Skills · Installed" / "Skills · Browse skills.sh" | "Skills" with the tab as a title-side selector | clickable tabs, the same control as the palette's categories |
| D16 | Ctrl+C (2.24) | always opens Quit | copies when text is selected; otherwise opens Quit | the desktop's copy key |
| D17 | code colors (3.1) | about twenty-five highlight classes (`internal/ui/styles/quickstyle_markdown.go:126-208`) | eight classes, the ones phase 3's highlighter maps (3-4 design §4.0) | one class set for both designs |
| D18 | Sessions sheet id (2.1) | `session` (`internal/ui/dialog/ids.go`) | `sessions` | the id the phases 3-4 design already opens (its §1.10) |
| D19 | Initialize Project while busy (2.2) | "Agent is busy, please wait before summarizing session..." (`internal/ui/model/dialog_actions.go:150`) | "Agent is busy, please wait before initializing the project..." | coordinator decision, `desktop/PLAN.md:374`; the terminal is left for the owner |
| D20 | custom command arguments (2.2) | placeholders replaced in map order, so `$FOO` can cut `$FOOBAR` | the longest names first | coordinator decision, `desktop/PLAN.md:375` |
| D21 | the tray (2.15) | Tab moves between the entries (`internal/ui/model/tasks.go:219-222`) | Tab and Shift+Tab leave the tray; ← and → move between the entries | the window's focus order (3-4 design D26) |
| D22 | a phone launch (2.27) | the launcher always opens a new terminal window (`internal/remote/launcher.go:77-101`) | a launch for a folder a desktop window already shows focuses that window, which starts sharing again with the new launch ID (G-26b) | windows are per project (PLAN section 7); coordinator decisions, `desktop/PLAN.md:414`, `:418` |

### 9.5 What the code contradicts in the plan and checklist

1. **The phase 5 done-line** ("a setting changed in one app shows in the other") cannot hold today across projects, nor for the terminal's `compact_mode` and `scrollbar`, nor for theme files: engines reload only after their own writes, the terminal copies those two options at start, and it loads a theme's colors only at start and in its own theme actions (3.5, section 4). G-1 and G-3 fix it.
2. **Models** chosen in one project's app never show in another project's app: each engine pins its model on purpose (`internal/config/store_fields.go:230-243`). The done-line holds for apps on the same project only (section 4).
3. **PLAN UPD-006:** the CLI has no AI-fix step for Crush updates (G-19).
4. **PLAN section 7, "Phone opens new chats in":** the launcher has no such option (G-17).
5. **CHECKLIST LIVE-001** "linger 8 s": the helper records linger 8 s, the count does not (`internal/ui/model/tasks.go:21`, `:56-69`, `:92-99`).
6. **CHECKLIST LIST-039** "saved project folders": the code lists every entry of `projects.json` (`internal/ui/sessionhost/picker.go:36-43`; `internal/cmd/session_host.go:50-61`).
7. **SET-016:** the schema text says 60 s, the code waits 2 minutes (`internal/config/options.go:198-212`); PLAN section 12 already fixes the text.

### 9.6 Not in scope

Optional ideas, not designed and not to be built without a plan change:

- a note in the theme editor when a token was adjusted for contrast;
- a strip of color swatches per theme in the Themes list;
- clicking a notification to bring its chat forward;
- Kitty graphics in terminal tabs;
- saving terminal output, or bringing terminal tabs back after a restart;
- a search field in Settings, and filtering the log viewer by level;
- a manual order for chats;
- a theme per window.

## 10. Changes in pass 2

- **Names.** Every name pass 1 marked provisional is now the phases 3-4 design's pinned name (its §1.10 table and §1.2 to §1.10) or one that S5 or a piece of section 7 adds; section 6 lists them all. The command list, the `api.js` functions, the window events, the stores, the `wire.js` types with the engine's JSON names, the Rust modules, the registries (actions, dialogs, grace, tray, slot, probe), the probe sections and the harness (`--fixture`, drivers, the hidden-tmux terminal app) are pinned. Phase 3's services are reused: settings, clipboard, opener and files (P3.2), so the approved clipboard and opener plugins are not added, and pictures load as `data:` URLs through `file_read`, not through an asset protocol.
- **S5 (7.1)** is new: every change phases 5 and 6 need in a file that S3, a P3 piece or a P4 piece owns, listed as the phases 3-4 design lists S3, plus the pinned libraries, the stub Rust modules, the placeholders and the harness additions. No piece of section 7 edits a file owned by S3, S5, a P3 piece or a P4 piece (7.5).
- **Pieces and ownership (7.3 to 7.5).** Fifteen app pieces and G56, each with its exact files, its needs (engine pieces A to D, P3 and P4 pieces, other pieces of this design) and its done-checks. A script on this file's text checked that every owned path is listed once, that S5's new files and the pieces' files are disjoint from each other and from S3's and the phase 3-4 pieces' files (3-4 design §5.1, §5.2), that each file S5 edits belongs to the owner its table names, and that each placeholder is in its named piece's list.
- **Rows settled with the phases 3-4 design (section 1).** That design keeps the Changes sheet (DLG-294 to DLG-311, its §2.8), LIST-012's phase 3 states and LIST-033 to LIST-037 (its §2.6) and LIST-016 (its §3.3); this file points to them, keeps LIST-012's phase 6 states for LST, drops VIEW's Changes sheet, withdraws G-15 and reduces 2.11 and look 5.12 to pointers. TYPE-015 (the external editor, TERM, 2.26) and START-020 (the crash guard, SYS, 2.28) moved in. LIST-054 moved from LST to THM, and the quit sheet from LST to PAL. Every row has one owner: 634 rows, 547 of them this design's.
- **Coordinator decisions applied** (`desktop/PLAN.md:372-379`). Q-P1 and Q-P2: the terminal's behavior stays (2.2). Q-P3: the corrected busy text (2.2; 9.4, D19). Q-P4: the longest placeholder names first (2.2; 9.4, D20). The approved libraries are pinned by S5 at exact versions (7.1). The warning icon's "!" is `--on-amber`, ink in the edition at 8.7:1, with the reason in 3.6. The edition's inline code is `#6a3d12`, with the reason in 3.7. The theme choice is not shared: the desktop keeps its own `theme` key, the edition is its default (section 3, 3.3, section 4, SET-009's row; 9.4, D13), and pass 1's open question on it and its "contradiction" item are gone. "Yep!" closes the window and the app ends with its last window (2.24). Inferred texts stay, marked inferred.
- **Engine facts folded in.** `secure_form` comes again for each view change, so ASK keeps one form per `form_id` and ignores repeats (2.7, 6.1). Events carry no order guarantee, so the phases 3-4 design's rule (its §1.5.3) applies, and stores keyed by their own ids keep early events (6.1). The tree entries of the palette are P4.4's and follow engine build items 13 to 16 (2.2). The model, plan mode and YOLO stay per project, and section 4's checks prove it in both directions. The Hyper credits line stays hidden until G-25. The `$` skill mentions wait for the owner's terminal work (`desktop/PLAN.md:306`).
- **The code wins.** Every route of 6.5 was checked against the line it cites in `internal/rpc/` at `65e4250d` (65 paths, all found), every event of 6.5 against `internal/pubsub/events.go:19-46`, and the JSON fields of 6.7 against `internal/proto/`. Names here are the code's.
- **Other fixes found in this pass.** `window_open` gains `sessionId`, so a chat of another project opens in that project's window (2.5, 6.4). `crush_dirs` and `crush_stats` return their output lines (6.4). `crush stats` opens the browser itself, so S5's stand-ins gain `xdg-open`, `x-www-browser` and `www-browser` (7.1, 8.2). A process started with `CRUSH_REMOTE_LAUNCH` re-executes itself with `--launch-id`, because the single-instance hand-off carries only arguments (2.27, 7.1). Ctrl+K, the list's search box and START-021's errors open the Sessions sheet through S5 (2.5, 7.1). The tray's Tab follows the phases 3-4 design (9.4, D21). MCP marks, error and success colors and destructive labels follow 3-4 design §4.11 and its D24 (2.9, section 5). The window close runs as ordered close steps (2.23). The look-choices citation is `desktop/PLAN.md:425`.

## 11. Changes in the review fixes

Review t41 (Claude Fable, read-only) of pass 2 (`a482b00e`) said "build after these fixes"; the coordinator's decisions are `desktop/PLAN.md:418`.

1. **A phone launch for a window that already shares.** New gap G-26b (9.1, `TestRemoteRestartUpdatesLaunch`): a start from the owner of a running share with a new `launch_id` sets the share's reported launch and answers `on`. A focused window always calls the start route again with the new ID, even while sharing or while a start is in flight (2.27). 6.5, SHARE's needs (7.3), the REM-017 stand-in and check (8.4, 8.5) and D22 (9.4) follow.
2. **The other app's plan-mode mark follows.** New gap G-26 (9.1, `TestMainAgentSwitchPublishesAndNamesAgent`): `config_changed` after a main-agent switch, and the main agent's name in `proto.AgentInfo`. S5 makes `stores/composer.svelte.js` follow `AgentInfo.agent` (7.1, also frozen), G-3 makes the terminal's mark follow (`TestConfigRefreshFollowsMainAgent`), section 4's plan-mode row and `x:set.plan_mode_per_project` expect the mark to follow, and 6.7 adds the field. The "does not follow" sentences are gone (section 4, G-4, 9.3, section 10).
3. **The phases 3-4 design at the desktop branch's head.** It is cited at `cc785a12`, with the S3 review decisions listed (sections 0 and 6). That commit's two new routes for P3.3 make `updateAgent()` a P3.3 function (6.5, 7.1), so S5 adds neither it nor its route. `--ansi-7` is `#6e6d6e` (3.7). The forced strip of 2.20 now states App's panel collapse: the panel hides first, below 1,060 picture px, and the strip is forced below 740.
4. **Ctrl+C.** PAL binds the quit key, a plain `bindKey("global", "Ctrl+C", …)` in `plugins/palette.js`, with the test `u:pal.quit_key` (2.24, 7.3). S5 adds only the terminal-tab exception (7.1, key rule 3, and its own check). 2.1, 2.2, 2.15 and 6.1 now describe S3's router (3-4 design §1.8).
5. **Linux only.** The jobs list and the listening ports are Linux only (PLAN Q11); 2.15 and 2.22 say so, and the check legs that read them run on Linux only.
6. **Sound.** One 5 s limit covers the whole player chain (2.18, 6.4, `r:sys.sound_chain`).
7. **Quit sheet.** Hover shows the hover look only and leaves the selection; on narrow widths the frame's side padding shrinks and the buttons go inline (2.24).
8. **Desktop citations.** Every `desktop/` citation was re-cited at `773344e0` (the code is the same at `cc785a12`), where S3 moved lines: `app.css` (the focus outline is `:166-167`), `ChatList.svelte`, `SidePanel.svelte`, `Icon.svelte`, `Frame.svelte`, the typing box (now `composer/Composer.svelte`), the chat head (now `ChatHead.svelte`, named in 5.16), `tests/sandbox.sh`, and the look-choices line, `desktop/PLAN.md:425`. The sweep also fixed one Go citation that B's follow-ups moved (`internal/agent/tasks_spawn.go:281-297`) and the `spec.json` line of charcoal-amber's `bg_least_visible` (`:154`).
9. **Other citations.** `main.rs:29-31` (with `app_info().version`), `Cargo.toml:28-29`, "the two passwords don't match" at `internal/cmd/remote.go:73-74`, the 15 s ping at `internal/remote/remote.go:303-316`, `internal/ui/model/editor.go:164-174`, and full `draw.go` paths in 2.18, 2.20 and 2.22. `ui.go:490-495` only seeds the theme key; the terminal loads a theme's colors at start and in its own theme actions (3.5, section 4, G-3, 9.5). 2.0 rule 10 lists every event of `internal/pubsub/events.go:19-46`, `permission_notification` included. S3's `notify` already keeps a notice with `ttlMs` 0, so S5 adds only `clearNotice`'s `text` argument (2.17, 6.2, 7.1).
10. **Settings switch.** Exact geometry: a 44×24 box and an 18×18 knob inset 3 on every side (5.13).
11. **Recheck of `97f3212e` ("ready to build").** The option `{overSheet: true}` is gone: its only user was Ctrl+C, which S3's router already asks `global` first over every sheet except the quit sheet (`desktop/src/lib/keys.js:52-67`, `:91-96`). PAL binds Ctrl+C plainly; 2.1, 2.24, 6.1, 7.1 (whose key rules are renumbered, the terminal-tab exception now being rule 3), 7.3 and S5's check no longer name the option. 2.27 now says the engine runs one start per workspace at a time, not per app (`internal/backend/share.go:199-200`, `:256-257`).

9.3 now records the coordinator's answers to pass 2's questions, so none is open, and 7.2 and G56 note that engine B's follow-ups are merged (`d3aa5502`).
