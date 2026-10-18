# Phase 2 evidence: the first screen (Scholar's edition)

All captures were made on a hidden screen; nothing was shown on the real desktop.

- `real-window-picture-size.png`, `real-window-compact.png`: the real app window
  (WebKitGTK, what the app ships with) at 1600 x 992, at picture size (12/12)
  and at the default Compact zoom (8/12). Made by
  `tests/sandbox.sh -s 1700x1100 tests/run-in-sandbox.sh look_real.py`.
- Match against the approved picture: the design tool's hero comparison of the
  real-window capture, 85% "match" (the tool's bar is 72%); its region crops are
  in `.impeccable/review/diff/hero/` (not committed; rebuilt by
  `impeccable build-phase record hero`). The parts it still flags, and why
  each is left as it is, are listed in PLAN.md section 15, "Phase 2 record".
- `align-compact.txt`, `align-picture.txt`: alignment measured on the
  real-window captures with the align skill's checker (`tests/look/align_real.py`,
  marks in `tests/look/align-marks.js`). One item remains at each size, both
  accepted with reasons in PLAN.md section 15: the search icon 1 px low at
  Compact (a third-of-a-pixel box), and PROJECT and VISUALS starting 1 px apart
  at picture size (different letter side spaces).
- `detector-*.json`: the design detector on the built page (JSON output), in a
  1600 x 992 and a 1280 x 800 window, at the smallest zoom (7/12), the default
  (8/12) and picture size (12/12): no findings, advisories included. The
  bone-paper warning is switched off for this project with its reason
  (`.impeccable/config.json`); `detector-1600x992-zoom-8-12-no-config.json`
  is the same scan without the project's settings and shows that this one
  warning is the only thing switched off.
- Waiting for the owner: the side-by-side first-screen review (review session
  `959ce8fa`), started in the morning.
