# File restore review evidence

## Independent final review

The parent reviewer re-read the capture ownership and pinned filesystem target
changes. The original sequential-action regression now passes unchanged and
is retained in `internal/filechange/review_session_budget_test.go`.
Tracked job `058` ran fresh (uncached) filechange, filehistory, message, and
UI model tests successfully. Job `059` rebuilt the executable from this source
and independently reran the saved-fixture terminal check: restore recovered
the original bytes, undo recovered the edited bytes, and cleanup passed.
No remaining blocking findings were identified in this review. Ordinary
concurrent leaf writes and Windows runtime coverage retain the limitations
documented below. Nothing was installed or pushed during review.

Implemented on `file-restore`, starting at `70c21d14`. Not installed or pushed.
The test executable is `/tmp/crush-file-restore`.

All Go tests, builds, fixture servers, and headless test drivers ran through
installed `crush bg`, with `timeout 900 systemd-run --user --wait --pipe
--collect` and PATH/HOME/GOPATH/GOCACHE passed through. The worker inherited
the task directory but not its session ID. Its own child session was identified
by matching its recent progress messages; that ID was supplied to `crush bg`.
No untracked build/test runner was substituted.

## Automated checks

`go-test.txt` records the successful full `go test ./...` run `032`.
Full runs `028` and `034` also passed. Job `035` built the final test binary
(`build.txt`); the subsequent cached rebuild also passed. Selected regressions
also passed under the race detector in `03A` (`race-test.txt`).

New tests cover full capture above the review limit, binary contents,
shell-created files, Codex reversed patches (including repeated lines and
final-newline changes), review pruning independent of file versions,
same-branch restore, cross-branch restore, target-only paths using the current
ancestry as their conflict baseline, created/deleted files and permissions,
conflict skipping and rechecking, changed cursors/messages after a preview,
symlinks, large files, undo after reopening, deduplication, oldest-chat budget
eviction, copied history, deletion garbage collection, and preview controls
at 40x12, 80x24, and 120x40.

## Actual terminal checks

Only named detached tmux sessions were used. App working folders, `-D` data,
configuration and `CRUSH_GLOBAL_DATA` were under
`~/.cache/crush-test/file-restore-review-qa/`. Notifications were disabled.
Because cache paths must be invisible to file restore, the two disposable
files under test were in this worktree's temporary `restore-qa-fixture/`
folder. No existing project file was edited by the test models. That fixture
folder is removed at completion.

The native OpenAI-compatible fixture read and edited one file. Its terminal
checks exercised the preview, Restore files, Chat only, Cancel, conflict
marking, restoring before a forked prompt, undo from the new chat, and clone
leaving files unchanged. See `native-*.txt`. Job `02F` passed the complete
fork/clone and choice check.

Live **Claude Code Haiku** received one small editing prompt. Its actual edit
was captured; selecting the chat root restored the original bytes; the
palette undo returned the edited bytes. This passed again after reopening
with the updated executable. See `claude-*.txt`; jobs `02D` and `02E`. Native restore/undo passed in `02C`.
Jobs `036` and `038`, the final terminal-size checks, also reopen this saved live edit and repeat
restore/undo with the final binary. `preview-80x24.txt` and
`preview-40x12.txt` show every action remaining visible. Plain terminal cells
were inspected, never the user's desktop. The web design detector reported
no findings; it is not a substitute for these terminal checks.

The fixture originally tried to overwrite without reading first. Crush
correctly rejected it. The fixture was corrected to issue a read before the
write. That is a test-driver correction, not a weakened file-tool safeguard.

## Real database copies

Job `033` used `sqlite3 .backup` on each real database, then made separate
baseline and candidate copies. Neither executable opened the original.
The installed executable opened the baseline; the test executable opened the
candidate. Both loaded an old chat and the saved-chat list headlessly.
Private captures and database copies stay in the scratch folder, outside Git.

- Exact fresh backup counts and hashes are recorded in `database-checks.json`.
- No messages were lost. Every persisted message content field and every
  tree row matched the installed binary's result.
- Both old-chat screens and both saved-chat lists matched (footer and elapsed
  relative-age labels excluded).
- SQLite integrity passed for both copies.

The source databases still needed the existing legacy review migration.
Both executables performed the same cleanup. `messages.updated_at` differs
because they ran at different times; this housekeeping timestamp is excluded
from the comparison. Raw pre-migration review payloads are not claimed to be
byte-identical to their migrated summaries. Counts, content hashes, and
results are in `database-checks.json`. The first run (`030`) correctly preserved
all data but its screen assertion compared “15 seconds ago” with “17 seconds
ago”. The rerun normalizes only the right-aligned relative-age field; it still
compares the rest of every list row.

## Review fixes and regression coverage

1. File-history failures log warnings. Message creation and updates, startup,
   and chat deletion continue. Failed versions keep an unavailable reason.
   Message/session tests cover failure paths; headless job `031` blocks the
   object folder with a regular file and verifies startup, a real tool reply,
   and the unavailable preview (`storage-failure.txt`).
2. Trackers no longer retain base64 restore payloads. Unchanged before images
   spill to private scratch files until their first edit; handed-off versions
   use a digest. Capture has a 64 MB checkpoint budget, in addition to 10 MB
   per file. New copies/checkouts/build products stay stat-only until edited.
   Tests cover handoff, an unchanged baseline later edited, overflow reasons,
   copied/generated files, and command finalization without rereading a jar.
3. Preview, undo preview, navigation/apply, fork/apply and undo execute in
   Bubble Tea commands. A small stable checking/restoring dialog remains on
   screen. Tests assert no file operation runs until its command executes,
   canceled previews cannot reopen, and busy controls work at three sizes.
   Preview caches repository and HEAD per directory.
4. A shared file lock covers blob publication through history/undo insertion,
   plus collection. A subprocess regression attempts collection in the gap,
   proves it cannot acquire the lock, then verifies the referenced blob lives.
5. Message points share Git lookups for one second. A regression checks 30
   points use only two Git processes, and ten preview files use only two.
   Capture no longer collects or enforces the budget on every tool result;
   maintenance runs in a throttled background worker at startup and roughly
   once a minute. Tests cover the lack of collection during Capture and the
   throttle. The 2 GB compressed budget is therefore eventually enforced.
6. Undo compares the saved restore HEAD with the current HEAD. Its regression
   verifies no warning for the same HEAD and a warning after HEAD changes.
7. Parents are checked before creation. Unix walks directory handles with
   `O_NOFOLLOW`; a regression proves no directory appears behind a symlink,
   while a normal missing parent is created successfully.

## Boundaries for review

History begins with newly captured tool changes. It cannot recreate old
versions from expired or partial diffs. Symbolic links and special files are
shown as unavailable; restores never traverse a new symlink. A late Codex
deletion without a tracker snapshot of its original permissions is also
unavailable, rather than guessing the old mode. The 2 GB budget
counts compressed object bytes. A multi-file restore can be interrupted by a
filesystem/database error; its saved undo is retained, rather than pretending
the filesystem and SQLite share one atomic transaction. Power-loss injection
and sustained multi-gigabyte tool batches were not tested.

`docs/tree.md` documents the user-facing behavior and all restore limits.

Cleanup passed: all six possible named test terminals were absent, the fixture
unit was inactive, and both disposable files were removed. The copied private
databases remain only in the scratch directory for reviewer inspection.

## Follow-up capture ownership and pinned restore targets

Completed-action capacity is returned to the shared capture pool by owner,
not by resetting its counter. Overlapping invocations and commands reported
late retain their reservations. Unchanged captures can share one reservation;
it is released only after the last owner finishes. Native shell subprocesses
retain their reservations until their combined report is finalized. Finalizing
reuses captured bytes, loads spilled restore payloads, and is idempotent.
Preview text from failed restore captures remains charged to its live owners.
Finishing a native report also closes its pool: a detached child that outlives
the shell cannot allocate again from released capacity or append to a finished
report. A dedicated regression covers this case.

Restore now keeps the validated directory handle through reading, temporary
file creation, rename, and unlink. Unix uses descriptor-relative operations
with `O_NOFOLLOW`. Windows holds all checked ancestor handles without delete
or write sharing, rejects reparse points by handle, and deletes through a
checked file handle. Missing parents are created beneath the retained ancestor.
A second content check immediately precedes replacement/deletion.

Deterministic fixtures replace the selected parent with a symlink after its
handle is opened. Reads, replacements, deletion, and recreation stay in the
original directory; outside contents and directories remain untouched. Other
tests cover late reports, overlapping baselines, shared ownership, native
redirects/subprocesses, imported baselines, repeated Finish, changed leaf
contents, and leaf symlinks. The parent's original sequential-action regression
was preserved unchanged.

Follow-up checks (all through tracked `crush bg`, timeout and `systemd-run` with
an explicit working directory; no pipeline hides the command's exit status):

- `044`: full `go test ./...` passed (`ownership-full-go-test.txt`).
- `045`: selected capture/restore tests passed under `-race`
  (`ownership-race-test.txt`).
- `046`: file-history test binaries compiled for Windows, macOS and FreeBSD;
  the Linux executable rebuilt successfully (`pinned-portable-builds.txt`).
- `04A`: the file-history package compiled for OpenBSD, NetBSD and Android
  (`pinned-bsd-android-builds.txt`).
- `050`: the full Go suite, detached-child race regression, and final Linux
  rebuild passed after the close guard was added (`ownership-final-checks.txt`).
- `04C`: affected packages passed after closing native reports against late
  detached children (`review-focused.txt`).
- `04D`: final full `go test ./...` passed (`review-full-go-test.txt`).
- `04E`: final selected races passed three repetitions (`review-race.txt`).
- `049`: Windows file-history tests compiled (`review-windows-compile.txt`).
  This is a compile check, not a Windows runtime result.

The final suite/race log pipelines used Bash `set -eu -o pipefail`; tracked
systemd jobs reported exit status zero. No test command's exit was replaced
by the exit status of `tee`.

The focused headless restore/undo passed in `051` on the final rebuilt binary
(`/tmp/crush-file-restore-final`): reopening the saved native fixture chat,
restoring its original bytes, then undoing recovered the edited bytes.
`pinned_restore_qa.py`, `pinned-headless-check.txt`, `pinned-restored.txt`, and
`pinned-undo.txt` record the driver and evidence. The driver was corrected to
select the populated fixture chat and skip an unchanged branch switch (which
correctly has no restore dialog). Its named detached terminal and disposable
file were removed in `finally`. No new live-model call was needed.

The repeatability follow-up passed twice consecutively in `056`
(`pinned-repeat-check.txt`). The driver returns to the saved tip before testing
restore, waits for the summary question instead of waiting for its still-visible
tree title to disappear, and handles a tip switch without a restore dialog.
A fixture lock serializes review workers using this saved chat. Earlier driver
runs `052`/`053` timed out on the incorrect title check; both cleaned up. The
fixture-occupied check also correctly refused an overlapping run. These were
driver failures; the successful repeat runs used the same final executable.

Platform runtime coverage is Linux only. Windows locking tests are present
and compile, but were not executed on Windows. Directory descriptors prevent
symlink redirection; they do not provide an atomic compare-and-swap against
an ordinary writer that changes a leaf after the last content check. On Unix,
if the original parent itself is renamed, restore operates on that retained
original directory. No installation or push was performed.

The repeatable headless driver also passed in `057`
(`review-headless-repeat.txt`). It first selects the populated chat's tip,
handles the summary prompt while the tree panel remains open, then restores
from the root and undoes the restore. A fixture lock prevents two drivers
from using the same detached terminal at once. Earlier repeat attempts
(`052`, `053`) timed out on driver assumptions about the selected point and
panel visibility; the corrected driver passes with the cursor left at the
root by a previous run. The terminal and disposable file were removed.
