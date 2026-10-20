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
