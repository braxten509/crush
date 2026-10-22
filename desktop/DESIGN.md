# Crush Desktop design system

What the shipped app does, taken from its code (`src/app.css`, `src/components/kit/`,
`src/lib/themes/`) at the end of phase 7. Where this file and the code disagree, the code
wins and this file is out of date. The approved reference picture is
`.impeccable/mocks/decision/edition.png` ("the Scholar's edition").

## Character

A printed page, not a dashboard: bone paper, iron-black ink, book serifs, one-pixel rules
(the outer boxes closed at their corners, the rules inside them stopping just short; no `+`
marks, owner 2026-10-20), and one vermilion accent. No shadows, no glows, no
blur, no rounded cards. Hierarchy comes from type size, rules and white space.

## Zoom and units

- Every size is written in **picture pixels** times `--px` (`calc(21 * var(--px))`). The
  picture is 12/12; the window zooms in twelfths (`lib/zoom.js`, Ctrl = and Ctrl -). Compact,
  8/12, is the default.
- Hairlines stay one real pixel at every zoom (`1px`, never `--px`).
- Small functional text never drops below 11 real pixels: `max(11px, …)` in the type roles.
- Tiny repeated marks (the busy dots) round their size and gaps to whole real pixels
  (`round(calc(4 * var(--px)), 1px)`), so at Compact zoom every dot draws the same width.

## Color

The default theme is `scholars-edition`. Other themes come from the terminal's 55 color
roles (`lib/themes/tokens.js`): direct tokens copy a role, derived tokens mix two roles, and
picked tokens take whichever of two roles reads better. A derived token keeps the built-in
value exactly while its roles still hold the built-in values, so the approved picture stays
exact.

| Token | Value | Use |
| --- | --- | --- |
| `--paper` | `#f9f8ef` | the page |
| `--paper-raised` | `#fbfaf2` | fields, sheet buttons at rest |
| `--band` | `#f3f1e6` | code, tool bodies, hover |
| `--ink` | `#0f0f0f` | text |
| `--slate` | `#353533` | secondary text |
| `--rule` / `--rule-frame` / `--rule-faint` | `#b0afa9` / `#aaa9a3` / `#cfcec7` | rules, from the frame down to the faintest box |
| `--vermilion` | `#e52b0e` | the one accent: the rail, focus hairline, Send |
| `--vermilion-ink` | `#d9290d` | vermilion letters (4.5:1 on paper and wash) |
| `--amber` / `--amber-ink` | `#e0a523` / `#8a5a00` | warning icon / warning text |
| `--error-ink` | `#a1261b` | errors, failed counts (7.0:1) |
| `--choice` | `#14323f` | selected marks, the caret |

Text colors are checked at 4.5:1 or better against the paper they sit on; the 16 program
colors (`--ansi-*`) are 4.5:1 or better on both `--band` and `--paper`.

Dark mode (Settings > Appearance > Mode: Light, Dark or Same as system; desktop setting
`appearance`) turns a light theme dark in `lib/themes/dark.js`, in the Graphite look the
owner picked to keep the light edition's softness: graphite paper `#232321` with no color
cast, and the light paper dimmed to a soft linen ink (`#dad9d2` for the edition, about
11:1 rather than 18:1). Every gray keeps its place between them by how light it looks
(OKLCH), so quiet text stays quieter (about 7:1) and rules stay faint; bands and controls
sit half a step farther from the paper so they still show. Colors keep their hue but calm
down: at least 0.65 lightness and 70 % of their saturation (the vermilion becomes
`#de6550`); colors nearly as dark as the ink (the navy links) become light with a trace of
their hue. Text on the red fill is the paper. The frame, faint, inner table and meter-edge
rules are mixed at three quarters of their light amount; control borders keep theirs. The
program colors are calmed and lifted to read on the dark band, and the diff tints become
faint versions of their hue. A theme that is already dark is left as it is. Diagrams,
drawn in the light colors, flip with `invert(1) hue-rotate(180deg) contrast(0.767)
brightness(1.02) saturate(0.5)` when the root's `data-mode` is `dark`, which lands their cream
fill on the graphite paper with no tint and their black text on the soft ink.
Components never write a color directly; everything goes through the tokens, so both modes
follow.

Icons are drawn SVGs in `components/Icon.svelte` (stroked, `currentColor`), never emoji or
symbol characters standing in for pictures.

## Type

Three faces (owner, 2026-10-20), bundled in `src/assets/fonts/`:

- **Spectral**: all text: titles, reading text, labels, buttons, fields, names, times, paths.
- **Inconsolata**: code and keys only (code blocks, tool commands and output, diffs, logs,
  key hints, typed secrets and colour values). Never for names or times.
- **Science Gothic**: the CRUSH wordmark only (a picture, so it keeps its own sizes).

Five text sizes, in picture pixels: **42** (chat and page titles), **28** (sheet titles,
figures, Markdown h1), **21** (reading text, rows, the typing box, authors), **19**
(secondary text, fields, sub-rows, help, Markdown h3), **17** (small text: times, hints,
keys, buttons, section labels, code). Small text never drops below 11 real pixels.

Roles are whole `font` shorthands (`--t-title`, `--t-reading`, `--t-label`, `--t-mono`, …).
Components use a role, never a raw size, face or weight. Labels are small caps by letter
spacing (`--track-label: 0.04em`), paths more so (`--track-path: 0.15em`). Message prose
stops at 80 letters a line; code, tables and pictures use the column's full width.
Titles use a capital on the first word only ("Switch model"); the app's own confirmations
name their action ("Quit / Cancel"), agent questions keep Yes / No. Things are "chats",
never "sessions".

A separator dot (·, •) always has a space on each side: "Claude 1 · Claude Code", never
"Claude 1· Claude Code" (owner, 2026-10-21, a rule for every project). Svelte drops the
spaces at the edges of an element's or a block's text, so `<span> · x</span>` draws "· x";
write the dot as `{" · "}` or between pieces. `src/dot-spaces.test.js` reads every
component and fails where a space beside a dot would be lost.

## Layout

- Three columns in a frame: the chat list (left), the conversation (middle), the side panel
  (right). The frame and column dividers are one-pixel rules that meet at the frame's
  corners and run into its top and bottom lines; section rules inside a column keep 20 clear
  of the left divider and 19 of the right. The left column can collapse to a strip. While
  its width glides, the list inside keeps its open width, so the column's edge covers and
  uncovers it and no word squeezes; the strip slides in once the list has gone and leaves
  at once when the list opens (look A, owner 2026-10-21; `list_fold.py`,
  `close_motion.py`). A sub-chat's tree line stays drawn over the open or hovered row's
  band (owner, 2026-10-21).
- The conversation reads at a fixed measure in the middle column; the typing box sits on the
  bottom rule. The red rail shows only under an open chat, never on a start screen.
- The notice line over the composer starts its words 66 in, where reply text starts, with
  its icon hanging to their left.
- Side-panel sections (Folder, To-do, Background, Servers) stack with space between them;
  To-do, Background and Servers show only while they have rows (owner, 2026-10-20).
- A chat and a terminal tab have no head of their own: the chat's title, folder and Git
  branch are in the title bar (owner, 2026-10-20). Only the init and onboarding screens
  keep the wordmark head and its rule.
- The folder is per chat (owner, 2026-10-20): the window's folder is the shown chat's or
  terminal's, and the title bar and panel follow it. Picking a chat or terminal of another
  folder switches the window to it in place (`project_enter`, no reload; terminals and the
  list stay). The search (Sessions sheet) shows the 5 newest chats of the 10 most recently
  used folders under "Recent" (each with its folder and time; the engine's
  `saved-sessions?folders=10`), then the shown folder's other chats under "Current folder ·
  ~/path" with only their time, past a rule; the folder's newest 10 first, 10 more per
  "Show more" at the list's end (a search finds them all); picking another folder's chat (there, in the list or with
  Alt+number) switches the window there in place.
- The left list is the open chats shared with the terminal's session list (owner,
  2026-10-20: "the two should be in sync"): one file, `$XDG_STATE_HOME/crush/
  open-chats.json` (internal/openchats, src-tauri/src/openchats.rs). A chat opened in
  either app is listed in both; its × (or closing it in the terminal) takes it off both.
  The list is flat, in the file's order, each chat's folder as its second line; no
  headings, no "All chats" line, no cap. The open chats close only when every app is
  closed. Unsent new chats are never listed. In the terminal, a listed chat it has not
  started yet starts when picked.
- New chat and New terminal share one row at the list's top in equal halves. Both (and
  the strip's "+" and ">_", the Terminals "+", Alt+N, Alt+T) open the folder picker on this
  folder; another folder switches the window first. Ctrl+N is a new chat here at once.
- Your messages sit on the right, at most 70% of the measure wide, in a box on the band's
  fill with the time and then YOU above it, also on the right, YOU at the edge (owner,
  2026-10-21); the AI's replies stay on the left, unboxed (owner's pick C, 2026-10-20).
- Switching chats keeps the last 6 chats left loaded (events keep them current), so going
  back shows one at once and reads it again behind it, even when it is in another folder
  (the folder's engine joins behind the shown chat); words typed while a chat opens go
  into that chat's box, as a send then does (owner, 2026-10-20).
- Alt+Backspace in the chat box deletes the word before the caret (owner, 2026-10-20).
- A message never draws a focus ring (owner, 2026-10-20). A reply still being written
  makes "Branch from here" say "Wait for the reply to finish", whatever is selected.
- Under the typing box is the same 15 as above it; the meter strip (panel hidden) brings 18
  more of its own that grows and shrinks with it, so the space glides both ways (owner,
  2026-10-20).
- The notice line unrolls up from the composer rule and rolls back down to it; while an
  update runs, its sync mark turns about its own middle (the whole <svg> turns, centre
  50% 50% = the ring's centre) (owner, 2026-10-20).
- The right panel's foot shows the build the app runs and one button that changes state,
  in the 19 of Settings at the left foot (owner, 2026-10-20 and 2026-10-21): Check for updates → Checking… → Up to date, or Update (vermilion,
  "old → new" beside it) → Updating… → Restarting…. Update runs `update.sh` (shipped beside
  the helper): a fresh copy of the fork's main branch in the cache is built (screens,
  helper, terminal crush), then the terminal crush (backed up) and the app (install.sh
  while it runs) are installed and the app starts again with its open chats held
  (`open-chats.restart`). Each window checks quietly once when it connects. One branch
  only (owner, 2026-10-22): `main`.
- One ring when the terminal and the desktop are both open, from the desktop (owner,
  2026-10-20). The helper (watch.rs) listens to the folders of the shared open chats that no
  window shows, through each running engine's socket, with a `watch=1` stream that holds
  no claim (the engine still closes when its own apps go), and rings "complete" for a
  finished, idle turn and "message-new-instant" for new questions. The folders it hears
  (shown or listened to) go into its entry in `open-chats.json` (`dirs`); a terminal in
  one of them skips those two sounds (internal/openchats DesktopHears). A probe run writes
  the ring to `alerts.jsonl` instead (tests/driver/ring_once.py).
- "Your answers to the questions" is a button: it opens "Your answers", each question with
  only the picked answers (✓, ink 600) and notes in italics (owner's pick A, 2026-10-20).
  Labels come from the newest `crush ask` call before the line (lib/answers.js); without
  one, the ids the result names (tests/driver/answers_popup.py).
- Dragging the chat's scroll bar never shows a blank chat (owner, 2026-10-20): Electron runs
  with `run-all-compositor-stages-before-draw`, so a frame shows only once it is painted
  (tests/driver/scroll_drag.py records the real pixels with ffmpeg; without it 5-8 of 333
  frames were empty paper; p3_speed still passes).
- The chat's scroll range ends on a whole pixel (an invisible bit of room under the last
  item), so a wheel at the bottom never nudges it (owner, 2026-10-20).
- Clock times read "9:05 AM" (bylines and the list's times today and yesterday).
- Sending a message or a `!` command brings the chat to its bottom, following.
- The title bar's icon is the heart bit alone (no tile). The title, folder and branch share
  one baseline, set so the title's capitals sit on the bar's middle with the heart and the
  window icons (within half a pixel; the branch's smaller capitals sit 1 px lower), and
  they are evenly spaced word to word (tests/driver/titlebar_align.py
  with tests/look/titlebar_manifest.py, the align skill's checker).
- No column gets so narrow that its words are cut or overlap: the list stops where both
  buttons fit whole, the panel where every meter's label, figure and reset time fit, both
  measured from the words themselves at the current zoom.
- A panel icon beside the title bar's pin hides and shows the right panel (filled while
  it shows; the same switch as Settings and Ctrl+P); it is the only control for it there. The open chat's band, its faint lines and its
  vermilion edge reach one pixel past the row at each end, and the rules beside it go
  clear. The vermilion rail starts at the chat column's top.
- The typing box never scrolls under 15 lines, so its words stay on the cursor's line. The
  line before its model button runs the box's height and grows with it (owner,
  2026-10-21).
- The terminal falls back to installed Nerd Font faces for prompt symbols, and the space
  under its last row is `--paper` with no native scroll bar.
- Settings rows stop at 840 picture pixels, so each switch sits near its name.
- Settings > Accounts lists each tool's own login, its added accounts and "Another …
  account"; a rule parts the tools, not a tool's own rows. Every account, the own logins
  too (`options.own_account_names`), can be renamed; one not renamed shows the email it
  signed in with (Claude's `.claude.json`, Codex's ID token), else "Claude 2". Where both
  tools' accounts are named together (the fallback order, a chat's switch notes), a
  shared name adds the tool, "you@gmail.com (Codex)", or the number within one tool
  (owner, 2026-10-21; `internal/accounts` Every).
- Settings > Fallback order is one sentence per block (look C, owner 2026-10-21): numbered
  blocks as wide as the switch row, each with a drag handle, its sentence and ×, a small ↓
  in the room above every block but the first. The sentence's dotted-underlined words
  open a menu under them (`settings/WordMenu.svelte`: unfolds from the word, Esc closes);
  every pick saves at once. A dragged block follows the pointer while the others slide out
  of its way, then settles into its place; Alt+Up/Down slides the same way; added and
  removed blocks grow and shrink with their room. "+ Add block" is a dashed row.

## Title bar

On Linux the window has no system title bar; the page draws its own (owner, 2026-10-20,
design D; `components/system/TitleBar.svelte`). It is 48 picture pixels tall on `--paper`
with a `--rule-faint` hairline under it. From the left: the app icon, the chat's title as a
running head (`--t-item`, weight 500), the folder after it in italic `--slate`, then the
Git branch in the path style (`--t-path`, capitals, `--slate`), or on a branch "Branched
from <parent>" with the parent's title as a link; on the
right four square-cornered buttons 54 picture pixels wide: the pin (`--mark`), minimize,
maximize or restore, close (hover: `--control-hover`; close: `--vermilion-fill` with
`--on-accent`). Pinned, only the pin changes: it fills in `--vermilion-ink`; the bar's
rule stays the same (owner, 2026-10-20). The buttons are not in the Tab order, as on the system bar.
The bar is a drag area, so moving, edge snapping, double-click and the right-click window
menu stay the compositor's. The pin keeps the window above the others (KWin's "keep
above" on KDE Wayland) and is remembered per window. The page and its overlays sit under
the bar (`--titlebar`); in full screen the bar steps aside.

## Sheets (dialogs)

Every dialog is a `kit/Sheet`:

- Widths: small 520, standard 760, wide 1040 picture pixels, never wider than the window.
- A one-pixel frame closed at its corners; no shadow. The page behind is covered
  by `--scrim` (paper at 92%, no blur), except sheets opened with `veil: false` (themes,
  changes), which leave the page visible so its look can be judged.
- Title line (title, title-side controls, a close cross that does what Esc does), a rule
  with clear ends, the body, then the help line. Controls that do not fit beside
  the title move to a line under it; they never hide.
- Messages (notices) show where the person is: inside the front sheet, on the Settings
  page, or over the conversation. Errors stay until dismissed, replaced or fixed.
- Content sits 26 picture pixels in from the frame on both sides; the title text, section
  labels, fields, lists and the help line share that left edge.
- Building blocks: `Field` (one line, optional icon or prompt), `Area` (several lines),
  `FilterList` (fuzzy list with groups, matched letters underlined; rows 44 high, 64 with a
  second line, hover and selection drawn 8 in from the frame, a 4-pixel vermilion edge on
  the selected row), `Buttons` (a row or a stack, 36 high, the one button size everywhere;
  the underlined letter is the key), `HelpFooter` (key and text pairs that drop from the end
  with an ellipsis when the line is too short). Every list in a sheet uses FilterList's row
  look and the sheet's 26 gutter. A FilterList under a filter `Field` starts 12 below it
  with a faint 1-pixel line as wide as the field, then 12 more, so the selected row's band
  never touches the field.

## Interaction

- Keyboard first: every action has a key, shown in the help line or the command palette
  (Ctrl+P). Underlined letters in buttons are their keys. Every key action also has
  something to click (Save / Cancel in forms, Rename / Delete beside the Chats title).
- Small controls keep a click area of about 24 real pixels, whatever their drawn size.
- Focus is a one-pixel vermilion outline 2 picture pixels out, never a glow.
- Hover fills with `--control-hover`; a selected row uses `--band` between faint rules, a
  disabled selected row `--disabled-band`. A selected button or level is `--band` inside an
  ink hairline. No drop shadows; menus sit on one layer (30).
- Motion uses `--motion` (220 ms) and `--motion-quick` (140 ms) on `--ease-paper`; movement
  distances scale with `--px`. Every animation that ends, CSS keyframes and the Svelte
  transitions in `lib/motion.js` alike, also runs the invisible `--paper-tick`, or WebKitGTK
  draws one stale frame at its end (a settings page jumped back after settling). Everything
  that opens also closes with motion: the opening backwards in 140 ms, easing in, ending
  wholly rolled away (Settings slides away over the chat; the scrim fades; a typing-box form
  unrolls up and rolls back down over the returning row). The room a piece takes moves with
  it both ways, so nothing beside or below it jumps once it has gone: the chat list's and the
  panel's column widths glide as they collapse and come back, and pieces in the flow (the
  usage strip, a tool group's list, a tool's details) fold their height (`settle`'s `fold`). Opening pieces use `lib/motion.js` transitions, not on-mount
  keyframes, so each has its close; `close_motion.py` checks them. The busy marks loop over 1.2 s
  on one beat (`busyBeat`, counted from the page's start), so every mark on the page pulses
  together, the to-do list's step in progress too (owner, 2026-10-21). Nothing moves the layout when
  a state changes (no show or hide on a flag).
- The Send square shows Stop (a filled square) while the shown chat works and nothing is
  typed or attached; typing turns it back into Send, which queues the message. Esc stops
  too. One button throughout, so nothing moves.
- Usage meters (panel and the strip under the typing box) turn amber under 30 % left and
  rose under 10 %, bar and number together; there is no warning triangle. The strip's
  words sit midway between the typing box and the frame line under it.
- Remote control belongs to a chat (owner, 2026-10-21: "One remote per chat"): while the
  phone shares the shown chat, whichever app turned it on, the strip under the typing box
  shows a whole-pixel vermilion dot and REMOTE in ink, with nothing after it (the owner's
  status line, its subtext removed). It slides in and away with the strip; with the panel
  hidden it follows the meters in the same strip, so there is only ever one line. Nothing
  inside the typing box says REMOTE any more. A click on it opens the Remote control sheet;
  Turn off there, Turn off in the terminal, or closing the chat stops the share. Several
  chats can be shared at once; another chat shows no line, and the phone stays on its chat
  whatever the window shows (tests/driver/remote_line.py).
- A Codex account's free refills show as a REFILLS count with a "Use one" button, in the
  panel when it is open and in the strip under the typing box when it is hidden; only
  while the count is above 0. Claude has none, so nothing shows for it.
- "/" typed as the first letter in the typing box opens the command menu: the `@` popup's
  look over every Ctrl+P command (System, User, MCP), name left and shortcut right, best
  match lowest. Enter runs the command and clears the "/word"; Esc closes it and keeps the
  text. Ctrl+P keeps the full palette.
- Scroll bars are thin and show their thumb only while scrolling (about 2 s after the last
  move). A column grip next to a scroll bar sits on the far side of the divider, so the bar
  stays clickable. Only the user's scrolling shows it: the page moving the view (a send
  shrinking the typing box) does not. WebKitGTK redraws the bar through `--bar-shown` on
  the scroller (app.css), never by flipping `overflow`, which redrew the whole chat.

## Checks

- `tests/driver/owner_fixes.py` checks, at Compact zoom: the to-do steps stack, the
  strip's "›" cell and the Send square are centred, reply footers name the configured
  model, the busy dots are whole pixels and keep the page's one beat (the to-do step's
  too, and after Settings hides the chat), and Send turns into Stop and back.
- `tests/driver/dark_mode.py` turns dark mode on and off in the real window, checks the
  graphite paper, the soft ink (10 to 13:1), that the ink reads on every dark surface, that
  quiet text stays quieter and the frame rules faint, that a diagram's box fills with the
  paper (part `diagram`, on the themed chat), and that the chat's scroll bar hides after
  scrolling and is not covered by the column grip; dark pictures go to `tests/out/dark_mode/`.

- `tests/driver/look_real.py` compares the first screen with the approved picture in the
  real window (WebKitGTK).
- `tests/look/align_app.py` with `tests/driver/look_align.py` measures icon, label and edge
  alignment on the first screen and every dialog in the real window, with the align skill's
  checker (see README.md, "Check").
