# Crush Desktop design system

What the shipped app does, taken from its code (`src/app.css`, `src/components/kit/`,
`src/lib/themes/`) at the end of phase 7. Where this file and the code disagree, the code
wins and this file is out of date. The approved reference picture is
`.impeccable/mocks/decision/edition.png` ("the Scholar's edition").

## Character

A printed page, not a dashboard: bone paper, iron-black ink, book serifs, one-pixel rules
with small `+` marks where they meet, and one vermilion accent. No shadows, no glows, no
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
  (right). Column dividers are one-pixel rules with `+` marks where they cross a section
  rule. The left column can collapse to a strip.
- The conversation reads at a fixed measure in the middle column; the typing box sits on the
  bottom rule.
- Side-panel sections (Folder, To-do, Background, Servers) stack with space between them;
  an empty section keeps its heading in `--mark` and shows no "None" line.
- Settings rows stop at 840 picture pixels, so each switch sits near its name.

## Sheets (dialogs)

Every dialog is a `kit/Sheet`:

- Widths: small 520, standard 760, wide 1040 picture pixels, never wider than the window.
- A one-pixel frame with `+` marks at the four corners; no shadow. The page behind is covered
  by `--scrim` (paper at 92%, no blur), except sheets opened with `veil: false` (themes,
  changes), which leave the page visible so its look can be judged.
- Title line (title, title-side controls, a close cross that does what Esc does), a rule
  with marks at both ends, the body, then the help line. Controls that do not fit beside
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
  look and the sheet's 26 gutter.

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
  distances scale with `--px`. The busy marks loop over 1.2 s. Nothing moves the layout when
  a state changes (no show or hide on a flag).
- The Send square shows Stop (a filled square) while the shown chat works and nothing is
  typed or attached; typing turns it back into Send, which queues the message. Esc stops
  too. One button throughout, so nothing moves.
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
