# File restore review evidence

Implemented on `file-restore`, starting at `70c21d14`. Not installed or pushed.
The test executable is `/tmp/crush-file-restore`.

All Go tests, builds, fixture servers, and headless test drivers ran through
installed `crush bg`, with `timeout 900 systemd-run --user --wait --pipe
--collect` and PATH/HOME/GOPATH/GOCACHE passed through. The worker inherited
the task directory but not its session ID. Its own child session was identified
by matching its recent progress messages; that ID was supplied to `crush bg`.
No untracked build/test runner was substituted.

## Automated checks

`go-test.txt` is the final successful full `go test ./...` run (job `021`).
Job `022` built the final test binary.

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
`~/.cache/crush-test/file-restore-qa/`. Notifications were disabled.
Because cache paths must be invisible to file restore, the two disposable
files under test were in this worktree's temporary `restore-qa-fixture/`
folder. No existing project file was edited by the test models. That fixture
folder is removed at completion.

The native OpenAI-compatible fixture read and edited one file. Its terminal
checks exercised the preview, Restore files, Chat only, Cancel, conflict
marking, restoring before a forked prompt, undo from the new chat, and clone
leaving files unchanged. See `native-*.txt`. Job `018` passed the complete
fork/clone and choice check.

Live **Claude Code Haiku** received one small editing prompt. Its actual edit
was captured; selecting the chat root restored the original bytes; the
palette undo returned the edited bytes. This passed again after reopening
with the updated executable. See `claude-*.txt`; jobs `010`, `012`, and `01C`.
Job `01F`, the final terminal-size check, also reopens this saved live edit and repeats
restore/undo with the final binary. `preview-80x24.txt` and
`preview-40x12.txt` show every action remaining visible. Plain terminal cells
were inspected, never the user's desktop. The web design detector reported
no findings; it is not a substitute for these terminal checks.

The fixture originally tried to overwrite without reading first. Crush
correctly rejected it. The fixture was corrected to issue a read before the
write. That is a test-driver correction, not a weakened file-tool safeguard.

## Real database copies

Job `01B` used `sqlite3 .backup` on each real database, then made separate
baseline and candidate copies. Neither executable opened the original.
The installed executable opened the baseline; the test executable opened the
candidate. Both loaded an old chat and the saved-chat list headlessly.
Private captures and database copies stay in the scratch folder, outside Git.

- Home database: **28,449 messages**, 201 tree heads.
- Forecaster database: **20,966 messages**, 294 tree heads.
- No messages were lost. Every persisted message content field and every
  tree row matched the installed binary's result.
- Both old-chat screens and both saved-chat lists matched (footer excluded).
- SQLite integrity passed for both copies.

The source databases still needed the existing legacy review migration.
Both executables performed the same cleanup. `messages.updated_at` differs
because they ran at different times; this housekeeping timestamp is excluded
from the comparison. Raw pre-migration review payloads are not claimed to be
byte-identical to their migrated summaries. Counts, content hashes, and
results are in `database-checks.json`.

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
