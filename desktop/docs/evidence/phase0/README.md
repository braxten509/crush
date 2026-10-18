# Phase 0 evidence (2026-10-18)

## Harness self-test

- `selftest-summary.txt`: output of `tests/selftest.sh` after the review
  fixes (job 00C): the sealed-sandbox checks, the normal run passing, the
  deliberately broken run failing, and the owner's real Crush files unchanged
  (13 watched files).
- `selftest-normal.txt`, `selftest-broken.txt`: the two runs' full logs.
- `outside-after.txt`: the fingerprint comparison after both runs.
- `skeleton-hidden-screen.png`: the skeleton app on the hidden 1600x1000
  screen (the black strip on the right and bottom is the empty screen around
  the 1586x992 window).

Contrast, checked outside the sandbox on the same machine: the real screen
`:0` (both `/tmp/.X11-unix/X0` and the hidden `@/tmp/.X11-unix/X0` socket),
`pulse`, `pipewire-0`, `wayland-0` and `bus` in `/run/user/1000`, the system
bus socket `/run/dbus/system_bus_socket`, and the sound card in `/dev/snd`
all exist there, and the home folder holds many entries, so the sandbox
checks that report them missing or hidden are able to fail.

## Content rule

- `content-rule-contrast.txt` (job 00A): the same app built without the
  content rule (`TAURI_CONFIG='{"app":{"security":{"csp":null}}}'`, separate
  target folder in /var/tmp) fails exactly one check, "the window blocks
  injected inline code", so that check is able to catch a missing rule.

## Go tests

- `go-baseline.txt` (job 008): Crush's Go tests on `6b2146f1` before any
  engine change: 97 packages passed.
- `go-baseline-and-failing-contrast.txt` (job 00D): the same baseline after
  the review fixes (throwaway home, own time limit, outside-world check): 97
  packages passed and the real files were unchanged. Then a run on a package
  that does not exist, which must fail: go test exit 1, reported as a failure.

## Front end

Unit tests (`pnpm test`, 1 file, 2 tests) passed in job 00B; the front end
has not changed since.
