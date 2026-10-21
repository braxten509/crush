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

## Layout

- Three columns in a frame: the chat list (left), the conversation (middle), the side panel
  (right). The frame and column dividers are one-pixel rules that meet at the frame's
  corners and run into its top and bottom lines; section rules inside a column keep 20 clear
  of the left divider and 19 of the right. The left column can collapse to a strip.
- The conversation reads at a fixed measure in the middle column; the typing box sits on the
  bottom rule. The red rail shows only under an open chat, never on a start screen.
- The notice line over the composer starts its words 66 in, where reply text starts, with
  its icon hanging to their left.
- Side-panel sections (Folder, To-do, Background, Servers) stack with space between them;
  To-do, Background and Servers show only while they have rows (owner, 2026-10-20).
- A chat and a terminal tab have no head of their own: the chat's title, folder and Git
  branch are in the title bar (owner, 2026-10-20). Only the init and onboarding screens
  keep the wordmark head and its rule.
- New chat and New terminal share one row at the list's top in equal halves. Both (and
  the strip's "+" and ">_", the Terminals "+", Alt+N, Alt+T) start in this window's folder
  with no picker; the folder picker opens only from the panel's folder name.
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
- The typing box never scrolls under 15 lines, so its words stay on the cursor's line.
- The terminal falls back to installed Nerd Font faces for prompt symbols, and the space
  under its last row is `--paper` with no native scroll bar.
- Settings rows stop at 840 picture pixels, so each switch sits near its name.

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
  wholly rolled away (the panel slides out before its column goes; Settings slides away over
  the chat; the scrim fades; the chat list's width glides as it collapses to its strip and
  back; a typing-box form unrolls up and rolls back down over the returning row). Opening pieces use `lib/motion.js` transitions, not on-mount
  keyframes, so each has its close; `close_motion.py` checks them. The busy marks loop over 1.2 s. Nothing moves the layout when
  a state changes (no show or hide on a flag).
- The Send square shows Stop (a filled square) while the shown chat works and nothing is
  typed or attached; typing turns it back into Send, which queues the message. Esc stops
  too. One button throughout, so nothing moves.
- Usage meters (panel and the strip under the typing box) turn amber under 30 % left and
  rose under 10 %, bar and number together; there is no warning triangle. The strip's
  words sit midway between the typing box and the frame line under it.
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
  model, the busy dots are whole pixels, and Send turns into Stop and back.
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
