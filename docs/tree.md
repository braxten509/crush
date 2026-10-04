# Session trees for Crush

## Verdict

Yes. Crush can keep several paths through one chat, like Pi's `/tree`.
The prototype lives only on `pi-tree`, based on `0a1caf95`. Nothing is
installed or pushed. The original checkout is untouched. This new local
branch has no upstream tracking branch; no pull or rebase was performed.

A jump changes what the chat and model can see. It does **not** change files
on disk. Old branches remain in the same SQLite database. SQLite is a better
fit for this fork than copying Pi's JSONL file format.

The prototype is deliberately for a single local Crush window. It is a useful
working base, not a release-ready copy of every Pi tree feature.

## What was checked in Pi

The installed package is `@earendil-works/pi-coding-agent` 1.0.2. Its package
metadata points to [earendil-works/pi](https://github.com/earendil-works/pi),
not the older pi-mono repository name. The installed README is now a short
entry point; the command details live in its linked docs.

Read locally: `docs/sessions.md`, `docs/session-format.md`,
`docs/how-pi-works.md`, `docs/extensions.md`, `docs/compaction.md`,
`docs/keybindings.md`, and the shipped `dist/core/agent-session.js` and
`dist/core/compaction/branch-summarization.js` implementation.

Pi entries have an ID and parent ID. The selected entry supplies the path
back to the root. `/tree` moves inside that file. `/fork` copies the path
before a selected user message into a new session; `/clone` copies the
current path into a new session. Selecting a user message in Pi puts its
text into the editor and moves before it. Selecting another entry continues
after it. The prototype always continues **after** the selected message;
it does not yet implement Pi's edit-and-resubmit behavior.

Pi's selector supports search, labels, folding branch segments, jumping
between segments, and filters for users, labels, tools, or all entries.
Its `session_before_tree` hook can cancel navigation or supply a summary.
`navigateTree(targetId, {summarize, customInstructions, replaceInstructions,
label})` is exposed to extension command handlers. The summary covers the
path being left back to the common ancestor, and is attached to the path
being entered. Compaction also preserves raw history. File lists inside
these summaries track work; navigation itself does not restore file bytes.

## Storage and migration

Two additive tables carry the feature:

- `tree_nodes`: message ID, session ID, parent ID, optional bookmark label.
- `tree_heads`: session ID, selected message ID, and a revision number.

Messages, message IDs, tool results, review payloads, and upstream session
columns retain their existing shape. No generated sqlc files are changed.
A small handwritten `internal/db/tree.go` owns queries. Optional interfaces
in the message and workspace packages avoid changing every upstream mock
or the server protocol.

Migration `20261003120000_session_tree.sql` links each existing session into
one chain, ordered by creation time and then SQLite rowid. The second key
preserves insertion order when several messages share the old one-second
timestamp. The final message becomes the selected entry. A migration test
starts with the earlier schema, adds tied timestamps, migrates, switches,
reopens, and checks order and labels.

An insert trigger attaches each new message to the selected entry and moves
the cursor, in the same database transaction as the message insert. This
covers native turns, CLI turns, summaries and tool results without touching
each writer. A delete trigger keeps descendants linked when upstream removes
a failed summary placeholder. Switching validates session ownership and
updates the cursor and the latest summary on that path in one transaction.

The message service returns only the active path for chat rendering, model
history, per-chat prompt recall and the last assistant reply. The selector
has a separate all-branches read. Global prompt history remains global.
Session deletion still covers every branch. Labels stay outside model context.

The revision increases on each jump. CLI links record the revision they saw.
A mismatch prevents reuse even when an old linked message happens to be a
shared ancestor. Restarting Crush preserves the selected path and revision.

This is additive, not conflict-free: future upstream message writers and
summary changes still need review. Do not use an old Crush binary on a
branched production database: it would show all messages as one linear chat.
The migration intentionally has no destructive downgrade. Before release,
add an explicit feature-version gate for older readers.

## CLI findings

| Model backend | What was verified | Prototype behavior / later option |
|---|---|---|
| Native API | The agent already builds requests from `message.Service.ListFromSummary`. | Reads only the selected path. Existing tool-call/result repair still handles interrupted boundaries. |
| Claude Code 2.1.288 | Local help exposes `--resume` and `--fork-session`. The official CLI reference also documents `--resume-session-at` for print mode. | Close the idle kept process; start a fresh session and seed it through the existing handoff. A later optimization can combine fork/resume-at after saving native assistant UUIDs beside Crush message IDs. |
| Codex CLI 0.160.0 | Generated the installed app-server JSON schema. `thread/fork` accepts `threadId` and optional inclusive `lastTurnId`; the target cannot be in progress. No `thread/rollback` request exists in this installed schema. | Fresh thread plus handoff. Later persist native turn IDs and use non-destructive fork at supported boundaries. Never roll back the original thread. |
| Grok over ACP | The current driver uses `session/new` and `session/load`. It does not negotiate a fork implementation. | Fresh session plus handoff. Do not infer arbitrary-point branching from resume support. |
| OpenCode over ACP | Shares that driver. The ACP fork proposal is capability-gated and forks an existing session; a historical-message boundary is not guaranteed. | Fresh session plus handoff until the actual agent advertises and passes an appropriate capability test. |
| AGY | Local help exposes `--conversation` and `--continue`, with no fork or rewind flag; Crush resumes with `--conversation`. | Fresh conversation plus handoff. |

[Claude's CLI reference](https://code.claude.com/docs/en/cli-reference)
and the [ACP fork proposal](https://agentclientprotocol.com/rfds/session-fork)
are the external primary sources. The exact Codex finding is from the
installed binary's `app-server generate-json-schema`, not an assumption
based on an older web example. Capability checks for real Grok/OpenCode
servers remain future work; no claim is made that either currently forks.

Fresh seeding is the common safe fallback across model switches. It costs
more input tokens and loses provider prompt-cache continuity. Crush's
existing handoff is bounded (120,000 characters overall, with per-message
and tool-output caps), so it is not a lossless copy of long transcripts or
historical binary attachments. Small test transcripts fit completely.

## What happens to the rest of the app

**Queued and steered prompts:** a jump holds the session's dispatch lock.
It rejects active work, accepted-but-not-started requests, queued prompts,
UI pending prompts, and an in-progress transcript reload. The user can finish
or recall the queue first; the prototype never silently moves queued work.

**Background tasks and sub-agents:** sessions with known sub-agent records
are blocked, including completed records that might still be delivering a
result. Known running shell jobs and Claude's background tasks also block.
A release version should stamp accepted turns, task requests and their
results with a branch ID/revision, append results to their original path,
and leave the currently viewed path alone. This needs an explicit inactive-
branch notification instead of replaying a result into whatever is open.

**Summaries and compaction:** a summary is a normal node. A jump selects the
most recent summary on its ancestry or clears the summary pointer. Summaries
from sibling paths cannot leak into model context. Pi-style summaries of
the path being left are not implemented. A future optional summary should
append a new node containing the old leaf and common ancestor as metadata,
then switch only after generation succeeds. Cancelling keeps the old cursor.

**Diff reviews:** message IDs stay stable, so each visible tool result still
opens its own saved review. The existing retention rule stays unchanged:
only the most recent five real prompts across the chat family retain full
diffs, and idle expiry still applies. An old branch can therefore retain its
conversation while its full diff has expired. The prototype does not
promise permanent branch snapshots. The files sidebar and spending totals
still describe the whole session; context-usage estimates may be stale until
the next response. Todos clear on a jump to avoid stale instructions; plan
mode resets. Branch-local todo snapshots are future work.

**Phone sharing and client/server:** `/tree` is available only through the
optional local workspace capability. A jump refuses while `/remote` is on
or starting. Phone sharing can resume afterward and reads the chosen path.
There are no phone controls for tree selection yet. Production support needs
branch revision in the phone/server snapshot, prompt submissions and result
events, with a full transcript reset event and stale-revision rejection.
It also needs an exclusive writer policy; a second local process sharing
the same database is outside this prototype's supported mode.

## Files on disk

Keep Pi's behavior: switching conversation branches leaves files alone.
The dialog and completion notice both say this. A model on an older branch
must still inspect the current files before editing. Existing read-before-
write safeguards and Git guards remain active.

Saved diffs could support a **separate**, explicit restore workflow, but
not a reliable automatic rewind today. They expire, may contain several
edits to one file, and do not cover all external side effects. A later tool
would need a preview, current-content hashes, conflict handling, and a
backup of every changed file before applying a reverse edit. No Git reset,
checkout, clean, or destructive history operation belongs in `/tree`.
There is no restore button in this prototype.

## UI

Type `/tree`, or choose **Session Tree** in the command palette. Search
matches message text and labels. Arrow keys choose, Enter jumps, Ctrl+R
sets or clears a bookmark, and Escape closes. The active entry has `*`.
The existing session loader rebuilds the chat and folded tool groups after
a jump. The selector uses the existing dialog styles and stable row geometry.
No new global key chord is claimed; a release can add a configurable binding.

Not yet implemented: fold/unfold branches, filter modes, next/previous
branch shortcuts, `/fork`, `/clone`, root-before-first-message selection,
inline prompt editing, branch-leaving summaries, and tree extension hooks.
Very large trees currently load all messages before filtering; production
should join only active ancestry in SQL and page/search selector previews.

## Verification and remaining effort

The required build succeeded as Crush background job **008**:
`timeout 900 go build -buildvcs=false -o /tmp/crush-pi-tree .`.
The final full suite succeeded as job **009**: `go test ./...`, wrapped
in `timeout 900 systemd-run --user --wait --pipe --collect` so nested shell
review tests have their normal process environment. Every package passed.
The earlier duplicate migration number was corrected before these runs.

The worker's inherited task bridge was stale and had no session ID. A
separate detached, silent scratch Crush session tracked all builds, tests,
and the local API fixture through the installed `crush bg` command.
The installed executable was only run; it was never replaced. All prototype
terminals used their own `-D` directories and notifications were disabled.

Seven Go tests cover migration order/reopening, separate branches, label
persistence, summary isolation, failed-summary deletion, CLI revision and
handoff isolation, accepted/queued prompt rejection, and selector search,
bookmarks, selection stability and initial visibility.

Headless terminal checks used `/tmp/crush-pi-tree` in named detached tmux
sessions. The native OpenAI-compatible fixture recorded these requests:

1. ROOT (amber).
2. ROOT + OLD (violet).
3. Jump back, then ROOT + NEW (cobalt): OLD absent.
4. Select the bookmarked old branch, then ROOT + OLD + RETURN: NEW absent.

The chat re-rendered on each switch. Restarting retained both branches,
the selected entry and the `original` bookmark. This is a real native API
transport test with a deterministic local endpoint, **not** a paid live
API-model test. The fixture source, recorded user histories and final Go
suite output are saved in `docs/tree-prototype/`.

Live Claude Code (Haiku, existing login) was also tested with tiny no-tool
prompts. Its original path contained amber and violet. After jumping before
violet, a fresh native session replied **amber** when asked which colors it
remembered. Returning to the old branch restored violet in the transcript, and its next
reply was **Amber and violet.**
The original native session ID and the new branch's ID differed; links
recorded the branch revision. No native Claude transcript was truncated.

The requested prototype and research deliverable are complete. Relative to
a polished, broadly usable Pi-style feature, estimate **55% complete and
45% remaining**. These are rough shares of work, not calendar predictions:

| Share | Work |
|---|---|
| 55% done | Storage/migration, local navigation, branch-aware native/CLI context, labels, tests and headless proof |
| 15% remaining | Branch ownership for sub-agents, background results, queues, multi-client and phone protocols |
| 10% remaining | Optional branch summaries, `/fork`, `/clone`, compaction edge cases |
| 10% remaining | Folding, filters, branch shortcuts, prompt editing, small-terminal/accessibility polish |
| 10% remaining | Large-history performance, older-reader gate, branch-local state and review-retention policy |

Before a production release, the user should choose:

- Ship local-only navigation first, or wait for phone/background-task support.
- Keep the existing five-prompt diff limit, or budget storage for per-branch
  retained reviews. Neither choice should silently imply file rewind.
- Whether branch-leaving summaries should be offered on demand. Recommended:
  opt in, since carrying old-branch context can defeat intentional isolation.

File restore, if wanted later, should be a separately reviewed feature.
