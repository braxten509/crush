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

The Linux amd64 syscall tracer is still required for reviewing arbitrary writes
made by external shell processes. The test for that feature is explicitly
skipped on other platforms; this does not add equivalent macOS tracing.
Tailscale remote access, platform-specific terminal launching, CPU affinity,
and detached-process cleanup have not been redesigned in this change. Native
Windows support is outside this validation scope.
