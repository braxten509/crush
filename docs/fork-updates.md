# Updating this fork

The fork's maintained branch is `main`. Both macOS and Linux use this branch.
The original `cli-agents` branch remains available, but existing checkouts do
not switch branches automatically when its changes are merged into `main`.

## Switch an existing updater checkout

Quit Crush before switching the source checkout. Start with `git status` and
commit or stash your own uncommitted changes first. Run `git remote -v` and
identify the remote whose URL is `braxten509/crush` (usually `fork` or `origin`).
The commands below use `fork`; substitute `origin` if that is your fork remote.

```sh
git fetch fork
git switch main
git merge --ff-only fork/main
git branch --set-upstream-to=fork/main main
```

If the local `main` branch does not exist, use `git switch --track -c main
fork/main` instead of the last three commands. If Git reports diverged history,
stop and reconcile the local commits; do not reset or force-push them away.
Confirm the result with `git status -sb` and `git remote -v`: `main` must track
this fork, not `charmbracelet/crush`. If a separate `remote.pushDefault` or
`branch.main.pushRemote` was configured, ensure it also names your fork remote.

Build and install the updated fork using your existing installation procedure.
The updater finds the checkout embedded in `version.SourceDir`, falling back
to `~/dev/crush`. Builds installed elsewhere should embed their absolute source
path using Go's `-ldflags '-X github.com/charmbracelet/crush/internal/version.SourceDir=…'`.
Keep that checkout in place, with Go and Git available on PATH.

## Update behavior

The updater fast-forwards the current branch from its tracking remote, fetches
stable release tags directly from `https://github.com/charmbracelet/crush.git`,
and merges the release while retaining fork changes. It builds and runs the
full test suite before staging an installation. Dirty working trees and
non-fast-forward pulls are refused; unresolved merge conflicts are left for
review. Installation pushes through the checkout's normal Git push settings.

The test step uses a Go context deadline rather than GNU `timeout`, which macOS
does not include. Linux retains its `systemd-run` isolation when available,
with a matching runtime limit.

## Platform checks and limits

CI builds and tests the fork on macOS and Linux. The fork skips upstream-only
release, snapshot, nightly and generated-file publishing jobs, which depend on
Charm's credentials or private runners.

The Linux file watcher keeps syscall replies in its surviving helper, so a
background command can continue even if Crush exits midway through a review.
CI repeatedly exercises watcher shutdown in addition to the full test suite.

The Linux amd64 syscall tracer is still required for reviewing arbitrary writes
made by external shell processes. The test for that feature is explicitly
skipped on other platforms; this does not add equivalent macOS tracing.
`/remote` offers guided Tailscale setup on macOS and Linux. Linux uses the local
Tailscale service socket; macOS uses the official CLI, including the copy inside
Tailscale.app when it is not on PATH. Both paths verify the caller's Tailscale
account before serving the existing Pocket Agents protocol. Setup never
changes access rules, disables incoming-connection protection, or resets
existing Tailscale preferences. If the phone cannot connect after sharing is
on, check that both devices use the same account and that the Tailscale access
rules and computer firewall permit the connection. Tailnet Lock, if enabled,
may require signing the computer from a trusted device.

The Linux installer supports the distributions supported by Tailscale's
official installer. Starting an existing Linux service supports systemd and
OpenRC; other service managers need a manual service start. The macOS installer
may require administrator approval, approval of its system extension, and a
restart as directed by Tailscale. These OS prompts cannot be approved by Crush.

The separate `crush remote launcher` feature still defaults to Konsole; opening
new windows from the phone on other desktops requires `CRUSH_LAUNCH_COMMAND`.
Guided `/remote` setup shares an already-running window and does not install
that launcher service.

The session list runs each session in a hidden terminal on macOS and Linux and
draws it through Charm's terminal emulator. Inside it, sessions use ordinary
keyboard input: the emulator does not support the Kitty keyboard protocol, so
Shift+Enter is sent as Ctrl+J (Crush's other new-line key), typed characters
are sent as text (so Shift, Caps Lock and other keyboard layouts work), shortcuts that
need that protocol fall back to their alternatives. When the real terminal
shows Kitty graphics, the list says so to each session, tells it the
character size in pixels, and passes its pictures on: question-form pictures
are put in the session's part of the screen, taken down while another
session or one of the list's boxes is shown, and put back after. Other
terminals show pictures as colored characters. Copying
and window-focus changes are passed through, so clipboard copies and
finished-work notifications still work. Closing a session asks its Crush to
exit and kills what is left in its terminal after five seconds.

CPU affinity and detached-process cleanup remain
platform-specific. Native Windows support is outside this validation scope.
