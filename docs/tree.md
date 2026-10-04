# Session trees for Crush

Local release: `v0.97.1-local.20261003-tree`.

A tree keeps several conversation paths in one chat. Switching changes the
messages shown to you and sent to the model. If tracked files would change,
Crush shows a **file restore preview** after the summary choice. Choose
**Restore files**, **Chat only**, or **Cancel**. Nothing is restored without
that choice. If there are no recorded file changes, navigation needs no extra
step. `/fork` offers the same preview; `/clone` always leaves files alone.

## Commands and keys

- `/tree` opens this chat's tree. The command palette also has **Session Tree**.
- `/fork` opens a prompt picker. Choose a user prompt to copy its ancestry
  **before that prompt** into a new chat. The full prompt and saved binary
  attachments go into the composer for editing; nothing is sent automatically.
- `/clone` copies the current path into a new chat, including its last message.
  Other branches stay in the original chat. Copies have new message IDs and
  start fresh CLI conversations through the existing context handoff.

| Key in the tree | Action |
|---|---|
| Up / Down | Choose an entry |
| Enter | Continue after it; opens the summary choice |
| Ctrl+E on a user prompt | Move before it and put the full prompt in the composer |
| Left / Right | Fold / unfold the selected entry's descendants |
| Tab / Shift+Tab | Cycle default, no tools, user only, labeled only, all |
| Alt+Up / Alt+Down | Previous / next branch end or branch start |
| Ctrl+R | Set a bookmark; an empty name clears it |
| Escape | Close, leave the summary choice, or cancel summary generation |

The first row selects the start of the chat, before its first message.
`*` marks the active message. Search matches the displayed text previews and
bookmarks. Default hides tool results and empty assistant activity; no-tools
keeps empty assistant entries; all shows every message. Searching reveals
matches inside folded branches. The list scrolls on small terminals, and
footer text is clipped rather than wrapped into extra rows.

There is no new global shortcut. This fork's keymap is compiled into the app
and has no user-configurable binding layer; `/tree` and the palette avoid
claiming a conflicting chord.

## Optional summaries

Every jump offers **No summary** (the default) or **Add summary**. Choose with
Left/Right or Tab and confirm with Enter. A summary uses the current session
model, including native API models and CLI models. It covers only the path
being left after the common ancestor, through the old leaf.

The summary is generated without tools in a separate request. Only after a
successful, nonempty response does one database transaction select the new
path and append the summary there. It is a normal context node, not a
compaction boundary, so the destination history remains visible to the model.
Its old leaf and common ancestor are stored separately as provenance. Summary
usage is charged to the session. Escape, a model error, an empty response, or
the request timeout leaves the old position selected.

Copies and jumps leave the original history intact. A copied chat's spending
starts at zero; it does not charge again for historical turns. The usual CLI
handoff still has its existing transcript and attachment limits; copying a
Crush transcript does not create a lossless copy of a provider's private state.

## Local-only safety

Navigation and copying require an idle local workspace. Messages explain what
to do when a switch is blocked:

- A reply, accepted prompt, queue, or transcript load is pending: wait for it
  to finish, or recall queued prompts.
- `/remote` is on or starting: turn off phone sharing first.
- A sub-agent is running or its result has not been delivered: wait for the
  result. If delivery failed, use a new chat. Finished, delivered task records
  no longer block switching.
- A background shell command is running in this Crush process, or Claude has
  active background work: wait for it to finish.
- A remote/server workspace is in use: use a local workspace.

Phone tree controls, background-result branch ownership, and multiple writers
are deferred. Use one local Crush process per database for tree navigation.
The files sidebar and lifetime spending still describe the whole chat. Todos
clear and plan mode resets on a jump, avoiding stale branch instructions.
Branch-local todo snapshots are not implemented.

## File restore

The preview lists each path, its restore/delete/recreate action, and added and
removed line counts. It marks conflicts and unavailable versions. The list
and explanatory text scroll with Up/Down; Left/Right or Tab selects a button.
The action buttons stay in place, including in narrow terminals.

A jump backward restores the state before the changes being left behind.
A jump across branches first undoes changes to their common ancestor, then
reapplies the target branch through the selected point. Forking uses the
point **before** the selected user prompt. Available full contents and permissions are
saved independently of the five-prompt diff drawer, before its text is bounded.
The same hidden-path rules apply: no hidden folders, `/tmp`, caches, or
protected secure-entry paths. Only files already named by the tracker are
captured; starting a chat never scans its working folder. Preview runs in the
background and shows **Checking files…**. Applying and undoing also run off the
UI thread. Git repository/HEAD lookups are shared by files in the same folder
within each preview.

File history is best effort. A failed file save logs a warning and leaves an
unavailable version with a reason in the preview. It cannot stop a tool reply
from being saved, prevent startup, or report a successful chat deletion as a
failure. Failed restores themselves still report errors and keep any saved undo.

Before a write, Crush compares the current contents and permissions with the
last recorded state on the branch being left. It **skips conflicts** and lists
the skipped paths in the completion notice. It also checks again after the
preview, so a newer outside edit is not silently overwritten. Symbolic links
and special files are unavailable, and a new symbolic link in a path is a
conflict. If Codex reports a deletion only after the file is gone and no
tracker captured its original permissions, that deletion is also marked
unavailable instead of guessing its mode. Regular files are written through a temporary file and atomic
rename. Deletions remove only the named regular file, never a folder.

Before changing any files, Crush saves their current contents as a durable
undo snapshot. The completion notice points to **Undo Last File Restore** in
the command palette. Undo shows its own preview and uses the same conflict
checks. It survives restarting Crush. A fork's undo belongs to the new chat.
If a filesystem or database error interrupts a restore, the saved undo remains
available; a multi-file filesystem change cannot be one SQLite transaction.

The limits are **10 MB per file**, **64 MB of full snapshot bytes per
checkpoint** (both before and after), and **2 GB of compressed objects per
project**. The checkpoint remainder says **not saved: too many changes at
once**. Long-lived trackers keep digests, not full restore payloads. Before
images awaiting their first edit spill to private scratch files; a completed
review hands its bytes to durable storage once. Later checkpoints reuse the
saved digest. The existing bounded text diff cache remains separate.

New copies, checkouts and generated build products are recorded from their
file metadata, without reading their contents. Their backward action is a
delete (with the usual conflict check and undo snapshot). Reapplying them
across branches is unavailable unless a later edit captured their contents.
An explicit later edit reads its before image normally.
Larger files are marked **Too large to restore**. When the project budget is
full, the oldest chats lose saved versions first. Their file rows remain with
a clear budget notice in the preview. Chat copies retain their own references
to shared objects. Deleting a chat removes its index rows; unused objects are
collected after deletion and in throttled background maintenance at startup
and roughly once a minute. Capture does not scan all objects or run the budget
query for each tool result. The compressed budget is enforced by that
maintenance, so new captures can temporarily exceed it until the next pass.
Versions are
otherwise retained while the chat exists. Versions from before this feature
was enabled are not reconstructed from expired or partial diffs.

Restore affects tracked regular files only. It does not undo outside edits,
databases, installed packages, network effects, or Git commits. The preview
states these limits. When the project's Git HEAD differs from the selected
conversation point, it warns that restored files will be uncommitted changes.
Undo records the HEAD at the actual restore, so it warns only if Git has
moved since then. Ordinary message points share HEAD lookups for up to one
second. Git is used only for read-only HEAD/repository identification. Restore never
uses stash, checkout, reset, clean, or another Git write command.

## Saved diffs

The existing retention policy is unchanged: the latest **five real prompts**
in a chat family keep full diffs, and idle expiry still applies. Opening an
expired review gives a clear notice; it does not pretend there were no changes.
Copies preserve existing review references, so copying cannot revive an
expired diff or extend its retention. Old conversation branches remain saved
when their full diffs expire.

## Storage, speed, and older binaries

The tree adds `tree_nodes`, `tree_heads`, and `tree_branch_summaries`.
File restore adds `file_history_changes`, `file_history_objects`,
`file_history_points`, and `file_history_undo`, plus compressed objects under
`<data directory>/file-history/objects/<digest prefix>/<sha256>`.
A shared filesystem lock protects writing a blob and publishing its history
or undo reference from collection by another Crush process. This protects
storage shared by two windows; the existing one-writer navigation restriction
still applies. Directory creation checks every parent before creating it;
on Unix it uses directory handles that refuse symbolic links.
Existing messages retain their shape. No generated sqlc files are edited.
The first migration links older messages by creation time and SQLite insertion
order; tied timestamps keep their original order. Insert/delete triggers
maintain links. A jump changes the cursor, branch revision, compaction pointer,
and todos together. CLI links include the revision, so a jump cannot silently
reuse context from a different branch.

Chat and model history use a recursive SQL query that loads **only the active
ancestry**. The selector reads topology and SQL text previews capped at 240
characters per message, without decoding full tools, diffs, reasoning, or
binary attachments in Go. Editing a prompt fetches its full message on demand.
All topology still loads for navigation; this is not an unbounded full-history
payload load and it is not a claim of constant-time navigation for millions
of entries.

Older binaries cannot be patched retroactively to check a new marker. Instead,
the migration-history view requires a SQLite function registered by this new
binary. Older Crush versions fail during startup, before loading chats, with:

`no such function: Upgrade Crush: this database uses session trees`

The underlying migration records remain intact, and future migrations work
through an insert trigger. There is no destructive downgrade. Keep a database
backup if you need to use a pre-tree binary again. An already-running old
process is not upgraded in place: restart Crush before using the feature.

A regression found during the full suite also fixed database paths containing
`#` or `?`: both SQLite drivers now encode paths correctly instead of opening
a truncated, unintended filename.

## Verification

Builds and tests run as tracked `crush bg` jobs, each inside
`timeout 900 systemd-run --user --wait --pipe --collect`. The final release is
built and tested again on `cli-agents` after merging `pi-tree`.

Automated checks cover migration order/reopening, root selection, separate
branches, summary isolation, atomic notes, opt-in summary generation, common
ancestor scope, provider failure/cancellation, copying/forking, bounded
previews, CLI revision/handoff isolation, accepted/queued prompt rejection,
bookmarks, folding, filters, narrow-terminal rendering, and database path
encoding. The full `go test ./...` suite must pass before installation.

Headless runs use named detached tmux sessions and scratch `-D` directories
with notifications disabled. The local API fixture verifies native branch
history, optional summaries, cloning, forking, and edited resubmission. Live
Claude Code Haiku verifies fresh CLI handoffs and summaries using tiny prompts.
Pi was not logged in or changed.

Migration is tested on a SQLite backup of the real project database, never by
opening the original with a test binary. Both the project database (82 messages in two chats) and the main home database
(27,851 messages across 199 chats) were copied and checked. Every message and
ancestry order stayed unchanged; saved-chat displays matched and SQLite
integrity passed. The installed older binary also refused the upgraded
project copy before loading a chat.

Research and earlier prototype evidence remain in `docs/tree-prototype/`.
Final release evidence is recorded in `docs/tree-release/`.
