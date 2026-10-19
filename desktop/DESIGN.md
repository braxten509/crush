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

## Type

Four faces, bundled in `src/assets/fonts/`:

- **Charis SIL**: large lettering (titles, project name, Markdown h1 and h2).
- **Spectral**: reading text, labels, buttons, fields.
- **Inconsolata**: authors, times, keys, code.
- **Science Gothic**: the CRUSH wordmark only.

Roles are whole `font` shorthands (`--t-title`, `--t-reading`, `--t-label`, `--t-key`, …).
Components use a role, never a raw size. Labels are small caps by letter spacing
(`--track-label: 0.04em`), paths more so (`--track-path: 0.15em`).

## Layout

- Three columns in a frame: the chat list (left), the conversation (middle), the side panel
  (right). Column dividers are one-pixel rules with `+` marks where they cross a section
  rule. The left column can collapse to a strip.
- The conversation reads at a fixed measure in the middle column; the typing box sits on the
  bottom rule.
- Side-panel sections stack with a rule between them; the rule carries a small `^` caret
  pointing at the heading above (`Rule.svelte`, `caret`).

## Sheets (dialogs)

Every dialog is a `kit/Sheet`:

- Widths: small 520, standard 760, wide 1040 picture pixels, never wider than the window.
- A one-pixel frame with `+` marks at the four corners; no shadow. The page behind is covered
  by `--scrim` (paper at 92%, no blur), except sheets opened with `veil: false` (themes,
  changes), which leave the page visible so its look can be judged.
- Title line, a rule with marks at both ends, the body, then the help line.
- Content sits 26 picture pixels in from the frame on both sides; the title text, section
  labels, fields, lists and the help line share that left edge.
- Building blocks: `Field` (one line, optional icon or prompt), `Area` (several lines),
  `FilterList` (fuzzy list with groups, matched letters underlined), `Buttons` (a row or a
  stack; the underlined letter is the key), `HelpFooter` (key and text pairs that drop from
  the end with an ellipsis when the line is too short).

## Interaction

- Keyboard first: every action has a key, shown in the help line or the command palette
  (Ctrl+P). Underlined letters in buttons are their keys.
- Focus is a one-pixel vermilion outline 2 picture pixels out, never a glow.
- Hover and selection fill a row's whole frame with `--control-hover`; a selected row in a
  picker uses `--band`, a disabled selected row `--disabled-band`.
- Motion is short and never decorative: sheets slide in over 180 ms (`ease-out`), small
  movements take 120 ms, the busy marks fade on a 1.2 s loop. Nothing moves the layout when
  a state changes (no show or hide on a flag).

## Checks

- `tests/driver/look_real.py` compares the first screen with the approved picture in the
  real window (WebKitGTK).
- `tests/look/align_app.py` with `tests/driver/look_align.py` measures icon, label and edge
  alignment on the first screen and every dialog in the real window, with the align skill's
  checker (see README.md, "Check").
