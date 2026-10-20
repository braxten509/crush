# Debugging: whole layers flash see-through for a frame (2026-10-19)

## Symptom
Expected: sheets, pages and rows appear and stay solid. Actual (owner's screen
recordings Screencast_20261019_191819.webm and _192600.webm, 60 fps): when the
Themes sheet opens it shows partly see-through for ~9 frames, then goes almost
fully see-through for exactly one frame (3.65 s in the second video) and comes
back. The owner sees flashing "all over the app", more since the paper motion
landed. Not reproducible in the hidden Xvfb window (software drawing).

## Suspects (ranked)
1. WebKitGTK 2.54's GPU drawing (Skia GPU) on Mesa radv/radeonsi 26.3-devel
   (git-74b69717c5) on the RX 9070 XT (RDNA4, gfx1201). For: the video shows
   alpha the page never asks for; Buzz #2643 (RDNA4, CachyOS, Plasma Wayland,
   radv "not conformant") had see-through/unpainted webviews fixed by
   WEBKIT_SKIA_ENABLE_CPU_RENDERING=1; this PC's journal shows the same radv
   warning. More animation = more redraws = more chances, matching "worse since
   the motion". Against: no direct capture of the compositor output yet.
2. DMA-BUF renderer buffer handoff (WEBKIT_DISABLE_DMABUF_RENDERER=1 fixes
   flicker on NVIDIA per Tauri docs). Less likely on AMD.
3. The 170 Hz path (libdrm-exact-size + vblank monitor): frames paced to DRM
   vblank while GTK presents on its own clock. In use since 2026-10-14 for
   other apps; untested here.
4. CSS animations themselves: ruled out as the direct cause. no_flash.py shows
   no opacity animation anywhere, and the Themes sheet has no fade code, yet
   the recording shows it fading.

## Attempts
### 1. Removed every opacity fade and colour easing (commit 77af7e7d)
- Suspect tested: 4. Result: no_flash.py clean; owner still sees flashing.
- Conclusion: not the cause; the flashes are below the page.

## Research
- https://bugs.webkit.org/show_bug.cgi?id=305758: GTK/WPE glitches with
  UseDamagingInformationForCompositing + PropagateDamagingInformation and a
  semi-transparent background; filed 2026-01-19, resolved fixed (PR 56825),
  newer than or not in our 2.54.1? Matches the damage-feature cause.
- https://blogs.igalia.com/plampe/introduction-to-damage-propagation-in-wpe-and-gtk-webkit-ports/:
  damage propagation trades CPU for GPU; a destroyed layer can leave what it
  painted on screen (the snap after an animation ends).
- Bugs 277337 (damage used in composition, late 2024), 283425, 307426.
- https://github.com/block/buzz/issues/2643: RDNA4 + Mesa 26.1 radv + Plasma
  Wayland, webview transparent / stops repainting; WebKitGTK 2.52; fixed by
  WEBKIT_SKIA_ENABLE_CPU_RENDERING=1 (DMA-BUF off alone did not).
- https://v2.tauri.app/develop/debug/linux-graphics/: flicker mostly NVIDIA +
  DMA-BUF renderer; WEBKIT_DISABLE_DMABUF_RENDERER=1.
- https://forums.developer.nvidia.com/t/geforce-rtx-4070-flickering-issue-when-using-the-dmabuf-renderer-in-webkitgtk/274741

### 2. CPU drawing (WEBKIT_SKIA_ENABLE_CPU_RENDERING=1), owner test
- Where: temporary menu entry ~/.local/share/applications/crush-desktop-cpu-test.desktop
  ("Crush (CPU drawing test)"), same preload as the normal entry plus the variable.
- Suspect tested: 1.
- Predicted: no see-through frames → suspect 1 confirmed; still flashing → ruled out,
  test DMA-BUF off (2) and 60 Hz (3) next.
- Actual result, try 1: owner answered "same as before", but the log shows the
  only start since the entry was made (19:42:38) ran without the variable: the
  normal entry or a taskbar pin was opened. Not a valid test; repeated.
  Since then the app logs its drawing settings at start ("drawing settings: …").

### 3. Hidden GPU test display (set up 2026-10-19 19:50)
- Why: Xvfb draws in software and can't show GPU faults; the owner's two CPU
  tests never got the variable (both starts logged "drawing settings: default",
  the second at 19:44:16; most likely the normal entry or a taskbar pin was
  opened instead of the test entry).
- What: `kwin_wayland --virtual` (no `--drm`: that flag picks the DRM backend,
  which gets no screen) under its own `dbus-run-session`, own
  XDG_CONFIG_HOME/XDG_CACHE_HOME in tests/out/gpu, socket wayland-crushtest.
  It draws on renderD128 with the amdgpu driver (fdinfo `drm-driver: amdgpu`,
  libgallium 26.3.0-devel mapped): the same Mesa path as the owner's screen.
  Idle cost measured: ~185 MB memory, ~0% CPU.
- Recording: tests/out/gpu/cast (asks KWin over zkde_screencast_unstable_v1 for
  a PipeWire stream of the output), then gst pipewiresrc → lossless x264.
- Runner: tests/out/gpu/run-gpu.sh NAME [VAR=VALUE…] runs the app in a bwrap
  sandbox (throwaway home, fixture model-menu.json, /dev/dri and the test
  socket passed in, same LD_PRELOAD as the menu entry) and gpu_flash.py drives
  it through the probe (Themes sheet ×6, settings sections, tool rows, model
  menu). analyze.py VIDEO finds one-frame blinks and frames darker than both
  neighbours.
- Difference from the owner's screen: 60 Hz virtual output (owner: 170 Hz),
  no Plasma shell behind the window (black), probe clicks instead of a mouse.
- Since 19:58 the screen and recorder are the reusable `~/.local/bin/gpu-screen`
  (memory note gpu-test-screen.md); run-gpu.sh uses socket wayland-gpu-test.
- Recorder check on a known answer: a made-up clip with a black frame at 30
  and a 40% frame at 45 → analyze.py finds exactly frames 30 and 45. A moving
  test picture (videotestsrc ball, 60 fps) → 169 frames in 3 s, 0 suspects.

### 4. First GPU-screen recording (base1, default settings + menu preload)
- Actual: 5 suspect frames, all at 1.19–1.29 s, while the window opens (KWin's
  fade-in). Frames 72, 74, 75, 77 show the window area black with the test
  picture behind it faintly visible, alternating with real light frames: the
  window handed KWin buffers that were never painted. No suspects during the
  six Themes opens, settings sections, tool rows or the model menu (60 Hz).
- Conclusion: a GPU-only blank-buffer fault is reproduced (same class as the
  owner's see-through frames). Next: repeat to measure noise, then A/B.

### 5. Repeat runs (noise) and 170 Hz
- base2 (60 Hz): 0 suspects, window opening clean. hz170a (screen at
  169.74 Hz via kscreen-doctor custom mode, now gpu-screen's default): again
  blank alternating frames only while the window fades in (frames 251-252);
  nothing during Themes/settings/tools/model menu. So: window-opening blanks in
  2 of 3 runs; the owner's sheet-open flash is not reproduced yet.
- Recorder speed at 170 Hz: 442 frames in 3 s of a 170 fps test picture (~147/s).
- Side effect found: the very first nested KWin (with --drm, no own config
  folder) rewrote the owner's ~/.config/kwinoutputconfig.json at 19:49:52.
  Its content is still correct (DP-1 2560x1440@170.071, enabled). gpu-screen
  now always uses its own config folder.

### 6. A/B on window-opening blanks (ab.sh, 4 rounds × base/cpu/nodmabuf/nopreload, alternating)
- Predicted: a setting with 0 blanks in all 4 while base shows them in ≥2
  points at that layer (cpu → Skia GPU; nodmabuf → DMA-BUF handoff;
  nopreload → 170 Hz helper). All settings showing blanks → it's below WebKit
  (GTK/KWin), go there next.
- Actual (16 runs): blanks at window opening in all four settings (base 2/4,
  cpu 1/4, nodmabuf 4/4, nopreload 3/4), so not the Skia GPU path.

### 7. The blanks are recorder echoes, not the app
- Plain GTK3 and bare WebKitGTK control windows (ctl.py): recorder missed most
  of KWin's fade frames (4 real frames per run), so those controls were weak.
- Real-frame brightness in ab-base-2: 0, 2, 0, 34, 2, 59, 34, 235: each dip is
  an exact copy of the frame two before. Same in base1 (…39, 0, 80, 39, 120…),
  ab-cpu-4 and the rest: KWin's screen share resends an old buffer during its
  window animation.
- The owner's video 2: frame 121 (3.674 s), the one "see-through" frame,
  matches frames 90–113 (before the sheet opened, diff 0.2–0.5/255) while
  frames 120 and 122 show the sheet fully: an echo. Frames 114–120 are the
  sheet unfolding (blend fit error 16–38, so a moving/clipped reveal, not a fade).
- gpu-screen blinks now labels REAL vs ECHO (checked: the made-up clip's two
  blinks stay REAL). All 19 app recordings: 0 real blinks. Owner videos: 0 real.
- Conclusion: no see-through frame from the app has been captured anywhere.
  What the owner sees by eye is still unknown (asked).

### 8. Owner's answers (2026-10-19 ~20:20)
- Seen with own eyes on the screen; looks like "text or colors flicker"; most
  when opening sheets (Themes) and switching settings sections.

### 9. Recorder problems found and fixed
- KWin's screen share also missed app updates: a window covering the whole
  hidden screen is handed straight to the screen, and KWin skipped redraws
  for the app's changes (8 frames in 34 s; KWin screenshots showed every
  change within 50 ms). A 16x16 heartbeat window fixed it (3853 frames).
- Owner installed sway + wf-recorder. gpu-screen now defaults to a headless
  sway screen (2560x1440@170) recorded with wf-recorder (wlr-screencopy, no
  echoes). Color test: all 4 changes recorded (350 frames).

### 10. The Themes sheet fades in from see-through, then snaps (REAL, sway)
- sway1.mkv frames 49-62: the Themes sheet cross-fades over the side panel
  for ~7 frames (both texts ghosted), then frames 56-57 jump back to nearly
  the start (side panel solid, sheet faint), then frame 58 the sheet is solid.
- Variant test (var-gpu.mkv, 3 opens each, animation swapped via the probe):
  A unfold as is, B clip only, C slide only, D no sheet animation. All four
  show ~130 ms of gradual change; snap pairs (two big steps 16 ms apart) in
  A×1, B×2, C×2, D×1. So neither comes from the sheet's own unfold.
- Cause found in code: OverlayHost.svelte puts every sheet INSIDE .veil, and
  .veil has `animation: paper-fade` (opacity 0→1, 140 ms). For the docked
  Themes sheet the veil is "bare" (no scrim) but the opacity animation still
  fades its children, i.e. the whole sheet. no_flash.py skipped `.veil`
  targets, so it missed this (my mistake in attempt 1).
- Snap: the frames right after the veil's opacity animation ends show an older
  picture for 1-2 frames (WebKit dropping the animated layer). Test running:
  .veil animation off (variant F) vs A.
- Fix 1 (OverlayHost.svelte): the scrim fades on its own `.veil::before`
  layer; sheets no longer fade. no_flash.py now opens Themes and allows only
  that layer's fade: FAILS on the old build (`div.veil.bare|paper-fade`),
  passes on the new. sway: Themes opens 5/5 clean (a rising 0.19→0.88,
  blend ≤0.11), no snap-back (monotone.py: old build snapped on every open).

### 11. The snap-back itself: WebKit damage-only compositing (claimed root cause; NOT confirmed in the app, see 12)
- With fix 1, the About sheet (scrim fade) still snapped back in 2 of 3 opens
  (progress 0.94→0.27→0.99): the snap follows the END of any opacity
  animation, not the sheet.
- About ×6 per setting (snap-backs / 7 groups incl. window start): base 4,
  DMA-BUF renderer off 0 (reruns: 2, 0), CPU drawing 7, FORCE_SHM 2, no
  preload 3. CPU drawing makes it worse, so not the Skia GPU path.
- Bare WebKit control (ctl2.py, no app code: scrim fade + unfolding sheet,
  8 cycles, sway, same preload): default 8/8 snap-backs;
  PropagateDamagingInformation=off 0/8; UseDamagingInformationForCompositing
  =off 0/8; both off 0/8.
- Root cause: WebKitGTK 2.54.1's damage-based compositing (on by default)
  redraws only the regions it believes changed; when an animated layer ends,
  the frame shown is an older buffer with partial updates. Reproduced in bare
  WebKit, so it is upstream, independent of this app's code.
- Fix 2 (src-tauri/src/refresh.rs): turn UseDamagingInformationForCompositing
  off in every web view (same run-time feature switch as the near-60 fix);
  the app log's frame-rate line reports it.

### 12. Fix 2 does not work in the app
- Suspect tested: damage-only compositing (attempt 11).
- Predicted: if it is the cause, turning it off in the app gives ~0 snap-backs
  on About, as in the bare control.
- Actual (About ×6 per run, snap-backs): UseDamagingInformationForCompositing
  off from refresh.rs (late, after the view exists) 5, 4; off from a test
  preload hooking webkit_web_view_get_settings (early) 3, 6 (same library:
  0/8 in the bare control); will-change on .veil::before + sheet 4, 5, 3, 5
  vs 3, 2 without. test3.sh, 3 rounds alternating: base 4, 4, 4; DMA-BUF
  renderer off 1, 1, 1; all three damage features off (lib/all3.c) 2, 3, 5.
- Conclusion: the damage features are not the app's cause (or are not the
  whole cause). DMA-BUF renderer off is the only setting that clearly helps.
  Base is steady (4/4/4), so one run per setting is enough to see a change of
  2 or more.
- Reverted: refresh.rs damage switch still in the tree, to be removed if no
  later test justifies it.

## Sub-agent reviews
- 2026-10-19: Codex gpt-6.1-sol (high, read-only): attack on attempt 11.
  Says "layer teardown" is unproven and the control/app split may come from
  different layer trees. Ranked leads: (1) separate the scrim and sheet
  animation ends by changing durations; (2) the probe polls every 200 ms and
  reads layout, close to the flash timing; (3) check the missing scrim with a
  fixed patch outside the sheet, not the whole-window score; (4) recorder
  damage path (wf-recorder -D); (5) read back the feature switches on the real
  view, try PropagateDamagingInformation in the app; (6) About fetches three
  things on mount and grows while it unfolds; freeze its content; (7) bridge
  the control and the app (same scrim colour, same nesting).
  Action: testing (6) first (test4.sh: fixed-content Quit sheet vs About).

### 13. Does the About sheet's growth cause it? No
- Suspect: reviewer lead (6), About adds rows while it unfolds.
- Predicted: a fixed-content scrim sheet (Quit) stays clean if growth is the
  cause.
- Actual (test4.sh, ×6 per run, alternating): Quit 5, 5, 2; About 5, 5, 4.
- Conclusion: ruled out. The flash sits on the second-to-last frame of every
  opening (monotone.py now prints its time: 134-167 ms after the first
  changed frame), i.e. just before the final picture.
- Queued: test5.sh (scrim 500 ms / sheet 600 ms / no scrim fade / no sheet
  unfold: which animation end it follows), test6.sh (open 100 ms late so the
  probe's next 200 ms tick moves mid-animation), test7.sh
  (WEBKIT_SKIA_GPU_PAINTING_THREADS=0).

### Suspect: tile painting on worker threads races the layer swap
- When an animation ends, the animated layer stops being its own layer and
  its content must be painted into the parent's tiles. WebKit 2.54 paints
  tiles on Skia worker threads (WEBKIT_SKIA_GPU_PAINTING_THREADS /
  WEBKIT_SKIA_CPU_PAINTING_THREADS exist in this build). If the new layer
  tree is shown before those tiles are painted, one frame lacks the scrim.
- Fits: CPU drawing (more painting threads) 7/7 worst; DMA-BUF renderer off
  changes it (1, 1, 1). Prediction for test7: ~0 with painting threads = 0.

## Research (added 2026-10-19)
- https://github.com/xnmp/tauri-explorer/issues/1062 (Oct 2026, Tauri +
  WebKitGTK, version not named): "WebKitGTK drops a backdrop-filter surface
  for a frame whenever a composited animation on or inside it ends";
  one see-through frame exactly at the end of a 100 ms entrance animation,
  60 fps off-screen recordings. Their fix avoided composited animations (not
  acceptable here). Same family: a layer loses content for one frame when an
  animation ends.
- https://github.com/WebKit/WebKit/commit/3292b5d3e18271e9e376a51d251674f5b7bc2862
  "Tiled layer flicker when an animation ends" (Core Animation, Apple ports:
  not our code path, same idea).
- https://bugs.webkit.org/show_bug.cgi?id=207255 (GTK, closed Feb 2026 as
  fixed by bug 306856): accelerated animation removed one frame after its
  finished promise. https://bugs.webkit.org/show_bug.cgi?id=168911 (2017):
  GTK flicker when leaving compositing; frame drawn after the root layer was
  detached.

### 14. Which animation's end? The scrim fade's (test5.sh)
- Quit, 3 openings per run, flash times after the first changed frame:
  A normal (scrim 140, sheet 220): 3/3, 3/3 @134-158 ms.
  S scrim 500 ms: 3/3, 3/3 @483-731 ms (moves with the scrim's end).
  L sheet 600 ms: 0/3, then 3/3 @142-160 ms (stays at the scrim's end).
  N no scrim fade: 0/3, 0/3.  D no sheet unfold: 2/3, 0/3.
- Conclusion: the flash follows the END of the scrim's opacity animation;
  the sheet's unfold is not needed. The probe's 200 ms tick does not fit
  (flashes at 142 ms with the sheet at 600 ms), so test6 was stopped.
- WebKit source (TextureMapperAnimation.cpp, main): a stopped animation still
  applies its last keyframe "to avoid a flash", so the compositor alone holds
  the end value. Remaining places: the main thread's commit that removes the
  finished animation (stale opacity?), or the scrim dropping its own layer
  and being repainted into the parent tiles a frame late.
- Next: test8.sh: fill-mode both on the scrim (H) vs normal; then painting
  threads 0.

### 15. Hold the end value (fill-mode both)? No (test8.sh)
- Quit ×6: normal 4, 5; fill-mode both 3, 5. No change.

### 16. Leading explanation from WebKit's source (main, fetched 2026-10-19)
- RenderLayerBacking::animationFinished() removes the animation from the
  GraphicsLayer at once, but only marks a geometry update; the layer's
  opacity is refreshed later from the style (updateGeometry ->
  updateOpacity). A fade from opacity 0 last pushed opacity 0 to the layer.
- CoordinatedPlatformLayer::needsBackingStore(): "If the CSS opacity value is
  0 and there's no animation over the opacity property, the layer is
  invisible" -> returns false and the layer's tiles are dropped.
  hasActiveAnimationsOfType() only checks that an animation is in the list.
- So a flush between the removal and the opacity refresh leaves the scrim
  layer with opacity 0 and no animation: its backing store is thrown away;
  the next flush has opacity 1, new tiles must be painted (worker threads),
  and at least one frame shows no scrim. A race, so ~4 of 6 openings.
- Fits everything: follows the fade's END (14); no fade -> no flash; will-
  change keeps the layer but not its tiles (12); fill-mode doesn't help
  (15); slower painting (CPU drawing) is worse; the original Themes flash was
  the veil's own fade from 0 (10).
- Check: test9.sh, the same scrim fade starting at opacity 0.01 (invisible
  difference, skips the opacity-0 rule). Predicted: ~0 flashes.
- Hole check (CoordinatedPlatformLayer.cpp:943-962, 1165-1177): when
  needsBackingStore() turns false, the proxy is invalidated and the
  compositor-side layer gets setBackingStore(nullptr); the old tiles are
  freed. When it turns true again, a NEW empty store is made and its tiles
  arrive only after painting, so the layer shows nothing in between.
- test8.sh final (Quit ×6): normal 4, 5, 5; fill-mode both 3, 5, 4;
  WEBKIT_SKIA_GPU_PAINTING_THREADS=0 4, 5. Painting threads ruled out; what
  matters is the dropped and re-made backing store, not where tiles paint.

### 17. Fade from 0.01: still flashes (test9.sh, partial)
- Quit ×6: normal 6; fade from 0.01: 4. The "tiles dropped at opacity 0"
  rule is NOT needed (or not the only path).
- Review 2 (Codex gpt-6.1-sol, high, read-only): the fade-end link is strong,
  but (a) the renderer's opacity may already be 1 at removal (accelerated
  ticks skip style updates, KeyframeEffect.cpp:1933, but other style
  resolutions advance it); (b) ThreadedCompositor waits for pending tiles
  (ThreadedCompositor.cpp:522, 586), so worker painting alone gives no hole;
  (c) after removal the compositor falls back to the layer's STORED opacity
  (TextureMapperLayer.cpp:1425): a stale start value (0 or 0.01) looks the
  same as no scrim; (d) smallest real fix: publish the post-animation
  opacity together with the animation removal. Asked for a trace of the
  real values instead of more guessing.
- New leading idea: stale stored opacity at removal (the fade's start
  value), not dropped tiles. Discriminating test queued: fade from 0.5
  (test10.sh): stale value -> the bad frame shows a half scrim; dropped tiles
  -> no flash. Trace: Arch debuginfod symbols for webkit2gtk-4.1 2.54.1-1,
  then gdb dprintf on RenderLayerBacking::animationFinished,
  GraphicsLayerCoordinated::setOpacity/removeAnimation and the layer flush.

### 18. Fade from 0.5: the bad frame shows a HALF scrim (test10.sh)
- Quit ×6, scrim fade from opacity 0.5: 4/6 and 3/6 openings dip, and the
  dip lands at ~0.52-0.62 (vs ~0.22 for a fade from 0), e.g.
  0.92 0.85 0.52 0.91 1.00 @127-160 ms.
- Conclusion: at the fade's end one frame (sometimes two) is drawn with the
  fade's STARTING opacity. Dropped tiles (only at 0) ruled out. Matches
  review 2 (c): after the animation is removed the compositor uses the
  layer's stored opacity, which is still the start value; the real value (1)
  arrives a frame later. A WebKit bug: removal and the final opacity are not
  published together.

### 19. Real trace of WebKit (gdb, names-only symbols)
- Tool: Arch debuginfod symbols for webkit2gtk-4.1 2.54.1-1 cut down to the
  symbol table (tests/out/wk/sym, 168 MB; full DWARF made gdb use 17.8 GB),
  test preload lib/libptracer.so so gdb can attach to the hidden app's web
  process only (picked by WAYLAND_DISPLAY=wayland-gpu-test), trace.py logs
  breakpoints and continues. Runner: tests/out/wk/sym/trace.sh.
- tr-2 (Quit ×6): scrim layer of opening 1 (0x...f000): setOpacity 0.000,
  addAnim, setOpacity 0.094 (first sampled value), then NO opacity update
  for the whole fade; removeAnim at 2397 ms, and no setOpacity(1) for that
  layer ever after (fresh layers are made per opening). So after the fade the
  scrim stops being its own layer and is painted into the layer behind; the
  bad frame falls between "animation removed" (layer left at the stale
  sampled opacity) and "scrim painted into the parent".
- Next: trace layer removal (removeFromParent, destructors), repaints
  (setNeedsDisplay*) and scene commits to the compositor (tr-3).
- tr-3 (Quit ×6, layer removal + repaints + commits traced; openings 1-4
  and 6 flashed, 5 clean). In BOTH a flashing and the clean opening the
  fade is removed (removeAnim) and the scrim's GraphicsLayer is destroyed in
  the same main-thread update. The difference: in clean opening 5 a style
  update one frame earlier had already pushed setOpacity(1.000) to the scrim
  layer; in flashing opening 1 the layer still held the early sampled value
  when the animation was removed.
- Conclusion (root cause): WebKitGTK 2.54.1 does not refresh a composited
  layer's stored opacity while an accelerated opacity animation runs (only
  style updates caused by something else push the current sample). When the
  finished animation is removed, the compositor falls back to that stale
  value for a frame before the layer/tile changes catch up. A race with
  whatever else triggers style updates, hence ~4 of 6.
- Upstream: no matching fix in WebKit history (searched commits); bug 306856
  is unrelated (tiled backing for the frame view).
- Next: test11.sh, an invisible helper animation (@property --fade-tick)
  next to the scrim fade so the element's style is resolved every frame and
  the stored opacity is current at removal. Predicted: ~0 flashes.

### 20. Fix candidate: invisible per-frame helper (test11.sh)
- Variant T: a second animation on .veil::before of a registered custom
  property (@property --fade-tick, 0->1, same duration) so the element's
  style is resolved every frame and WebKit's stored opacity stays current.
- Quit ×6, alternating: normal 4, 5, 3; helper 0, 0, 0 fade-end flashes
  (T-3 showed 2 dips at the START of an opening, progress going below 0;
  normal runs have those too, ~1 per 2 runs: t11-A-1, t9-A-1, t8-A-1,
  t4-quit-1/2. A separate, rarer glitch, not caused by the helper.)
- Same risk elsewhere: paper-settle (new messages, notices, tool rows) and
  paper-slide-left/right (side columns) animate only transform, like the
  scrim's opacity; unfold animations also animate clip-path (resolved on the
  main thread every frame), which likely keeps them safe.
- test12.sh: the helper inside the shared paper-fade keyframes
  (--paper-tick), the form a one-file fix in app.css would take.
- test12.sh (helper inside the shared paper-fade keyframes, Quit ×6,
  alternating): normal 4, 5, 4; helper 0, 0, 0 (13/18 vs 0/18 openings).
  The one-file form works as well as the separate helper animation.

### 21. The fix (approved by the owner 2026-10-19)
- src/app.css: `@property --paper-tick` (number, initial 0) and
  `--paper-tick: 0` -> `1` in all six shared keyframes (paper-unfold,
  -settle, -fade, -unfold-up, -slide-left, -slide-right).
  src/components/sheets/ChangesSheet.svelte: the same in its slide-in.
  The pulsing activity dots loop forever and are left alone.
- src/animation-ticks.test.js: every @keyframes in src/ that ends must run
  the tick (8 tests; removing it from the Changes sheet's slide-in fails).
- src-tauri/src/refresh.rs: the damage-only compositing switch (attempt 11)
  removed again; it did not help in the app (12). The near-60 fix stays.
- Kept from attempt 10: the veil's scrim fades on its own ::before layer.

## Current best explanation (root cause)
WebKitGTK 2.54.1 runs opacity- and transform-only CSS animations on the
compositor and stops refreshing the composited layer's stored value while
they run (it keeps whatever the last style update sampled). When the finished
animation is removed, a frame is drawn with that stale value before the main
thread catches up. Proven with a gdb trace (19), the fade-from-0.5 test
(18: the bad frame showed a half scrim) and the timing tests (14). A changing
registered custom property in the same keyframes makes WebKit resolve the
style every frame, so the value is current: 13/18 -> 0/18 openings (20).

## Draft WebKit bug report (not sent; the owner decides)
Title: [GTK] One stale frame when an accelerated opacity/transform CSS
animation ends (layer keeps an early sampled value)
Version: WebKitGTK 2.54.1 (Arch Linux), Wayland, AMD RX 9070 XT, Mesa 26.3.
Steps: an element with `animation: f 140ms ease-out` and
`@keyframes f { from { opacity: 0 } }` over a page; open it repeatedly and
record the screen at the display's rate.
Expected: the last frames go smoothly to opacity 1.
Actual: in ~4 of 6 runs, one frame right after the animation's end shows the
element almost transparent (with `from { opacity: .5 }`, half transparent),
then it is correct. Transform-only animations jump back the same way.
Trace (gdb on the web process): GraphicsLayerCoordinated::setOpacity is called
with the first sampled value (e.g. 0.094) after addAnimation and not again
during the animation; removeAnimation follows with no setOpacity for that
layer in the same update. When a style update had already pushed 1.0 just
before removal, no stale frame appears. RenderLayerBacking::animationFinished
removes the animation but leaves the opacity refresh to a later geometry
update. Workaround: animate a registered custom property in the same
keyframes so the style is resolved each frame.

### 22. Real build check (test13.sh, nothing injected)
- Release build with the fix; sway, 6 openings each: About 0, Themes 0,
  Changes 0, Quit 1 "dip" (0.92->0.86). Per-frame diff against the final
  picture shows that opening converging monotonically over the sheet's box
  (0.88, 0.64, 0.42, 0.26, 0.11) and only a tiny later change in a
  ~90x40 px spot inside the dialog (<0.1% of the window, a button settling);
  monotone.py's 'after' picture is taken later, so that spot made a fake
  dip. No end-of-animation flash in 24 openings.
- Unit tests: 1934 pass (incl. animation-ticks.test.js).
