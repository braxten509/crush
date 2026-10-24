# Debugging: one ended background process reported hundreds of times

## Symptom

A chat filled with the same three user messages, about 30 a second for
17 seconds: two copies of a typed prompt and one
`<crush-task-result>` saying a background process had ended. The engine
log showed `Running a prompt an earlier Crush left queued` 1369 times.
The burst only stopped when the engine was restarted.

## Suspects (ranked)

1. `watchDetached` reporting the same ended process every scan: ruled out.
   The log line present was the queued-prompt restore, not a detached
   report, and one ended process can only be reported once per hub.
2. The desktop app resending prompts: ruled out. The clients sent one
   message each; the engine's own log showed the restore path running.
3. `claimPrompts` replaying the engine's own saved queue: confirmed.

## Attempts

### 1. Read the engine log of the burst

- Where: `~/.cache/crush/server-unix*/crush.log`
- Predicted: a repeated writer is named.
- Actual: `Running a prompt an earlier Crush left queued` for session
  `47570cbb…` thousands of times, one every ~22 ms, alternating prompt
  lengths 127, 331, 127.
- Conclusion: `restorePrompts` replayed a queue that had grown to hundreds
  of entries.
- Reverted: no.

### 2. Count the stored messages

- Where: project `crush.db`, session `47570cbb…`
- Actual: 509 copies of the prompt and 254 copies of the notification in
  one 17-second window.
- Conclusion: the queue in `pending-work.json` had been duplicated over
  and over, then replayed in one go.
- Reverted: no.

### 3. Unit test on `claimPrompts`

- Where: `internal/agent/pending_work_test.go`,
  `TestPromptsOfThisCrushAreLeftAlone`
- Predicted: fails before the fix, passes after.
- Actual: failed; `claimPrompts` returned an entry owned by
  `os.Getpid()`.
- Conclusion: confirmed.
- Reverted: no.

## Root cause

`claimPrompts` kept saved prompts only when another live process owned
them (`q.Owner != self && taskProcessAlive(q.Owner)`). Entries this
process had saved itself fell through to the claim branch, so every
resume took the engine's own still-queued prompts and ran them again.
`writeQueues` saved the growing in-memory queue every 2 seconds, so each
extra resume doubled it; eventually one restore replayed hundreds of
copies at once. `claimInterruptedJobs` already had the right check
(`s.Owner == self || taskProcessAlive(s.Owner)`); `claimPrompts` now
matches it.

## Fix

`internal/agent/pending_work.go`: keep entries whose owner is this
process or any other live process.

## Checks

- `TestPromptsOfThisCrushAreLeftAlone` fails before and passes after.
- `go build ./...` passes.
- `go test ./internal/agent ./internal/backend ./internal/server` has
  failures that are not from this change:
  - `TestBackgroundCountAfterCtrlB`,
    `TestNativeBackgroundJobListedOnceAndUserStopIsReported` and
    `TestShellFileReviewCLIEndToEnd` fail the same way on a clean checkout
    of `7bb01c90`; this Crush's own `CRUSH_TASKS_DIR` and
    `CRUSH_NATIVE_SHELL` markers leak into the fixture processes, and the
    file-review keeper cannot observe them while tests run inside Crush
    (already noted in `desktop/DEBUGGING.md`).
  - `TestFolderSwitchKeepsOwnedWork`, `TestQ2DropPastGraceStopsUnviewedWork`,
    `TestQ2KeepsWorkAnotherAppViews`, `TestListHelpersScopedToChat` and
    `TestEndedWorkLeavesOwners` fail in the full backend run (21 s waits)
    and pass alone; a clean checkout of `7bb01c90` fails them identically.

## Folder + closes the shown chat (2026-10-24)

- Report: the + beside a sidebar folder closes the current view without
  opening a new chat. The exact live failure has not been reproduced.
- Clean reproduction: an isolated real app on gpu-screen creates chats in
  the current folder and another folder and keeps them through refreshes.
  First test launches mistakenly shared the owner's sidebar state; all
  subsequent launches hide HOME, /tmp and XDG state in a separate namespace.
  The new driver refuses to run without the isolated HOME and state paths.
- Confirmed failure path: newChat cleared currentId before POST sessions
  succeeded. Its caller returned true immediately and only logged errors.
  A regression with a rejected creation failed before the fix: currentId
  was empty while the save was pending. The folder action also returned
  true for a false creation result.
- Fix: create before leaving the shown chat, report save errors, await the
  result, serialize folder + operations, and restore the previous folder
  when creation fails. Tests for folder rollback and overlapping clicks
  failed before the second part was implemented.
- Independent review t1 identified the rollback and overlapping-click gaps;
  those gaps have targeted regression checks. Other hypotheses (duplicate
  helper rows and the missing question form) remain unproven and untouched.
- Research: https://svelte.dev/docs/svelte/runtime-errors documents
  each_key_duplicate. The live log contains that error, but its stack does
  not identify the failing component, so it is not established as the cause.
- Validation: initial fix passed 2,046 tests, screen/helper/Go release
  builds, and the isolated folder-click driver. Final rollback/queue
  version passed all 2,047 tests and the screen build. The final isolated
  real-app driver passed all four folder + flows, verifying folder ownership,
  previous-chat retention, and survival through sidebar refreshes. Final
  independent review found no blocker in this focused fix. The owner's
  live creation failure and question-form issue remain unverified.
