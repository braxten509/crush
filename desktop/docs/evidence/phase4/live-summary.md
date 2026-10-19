# Phase 4 live fork checks (P4.3, design 3.5)

Run on 2026-10-19 with `desktop/tests/live/fork_live.py <tool>`. Each tool ran in its own throwaway project under `/var/tmp/crush-desktop-live/<tool>/`, on CPUs 6-11,18-23, under `timeout 300`, with every `*_API_KEY` variable removed (subscription logins only). Native IDs and markers come only from the tools' own output. Raw output for each step is in `live-<tool>/` (`01-help.txt`, `02-chat.jsonl`, `03-fork.jsonl`, `04-source.jsonl`, `05-mid-reply.jsonl`, `ids.json`). The verdicts come from `fork_live.py --judge-only live-<tool>`.

| Tool | Version | Model | Fork call that works | 1 help | 2 chat | 3 fork | 4 source | 5 mid-reply |
|---|---|---|---|---|---|---|---|---|
| Claude | 2.1.303 (Claude Code) | haiku | `claude -p --input-format stream-json --output-format stream-json --verbose … --resume <source> --fork-session --resume-session-at <uuid of the cut assistant message>`; the new session ID is `session_id` on the stream's `system`/`init` line | pass | pass | pass | pass | pass |
| Codex | codex-cli 0.163.0 | gpt-6.1-sol, effort low (no "mini" model offered) | app-server `thread/fork` `{"threadId": <source>, "lastTurnId": <cut turn id>, "cwd", "model", "approvalPolicy", "sandbox"}` → `result.thread.id`, then `turn/start` on it | pass | pass | pass | pass | pass |
| Grok | 1.0.62 (1474f9cbdd91) | grok-4.7, effort low (the only model) | ACP extension `_x.ai/session/fork` `{"sourceSessionId": <source>, "sourceCwd": <cwd>, "newCwd": <cwd>, "targetPromptIndex": <0-based index of the cut prompt>}` → `result.newSessionId`, then `session/load` of it | pass | pass | pass | pass | pass |
| OpenCode | 1.19.3 | opencode-go/deepseek-v4.1-flash | HTTP API of `opencode serve`: `POST /session/<source>/fork?directory=<cwd>` `{"messageID": <message after the cut>}` → `id`, then ACP `session/load` (mechanics checked without a model, below) | pass | not logged in | not run | not run | not run |
| AGY | 1.4.2 | gemini-3.8-flash-low | none: no fork and no resume-at (only `--conversation <id>` and `--continue`, which resume the whole chat) | pass | pass | n/a (written copy) | n/a | n/a |

Step 5 timing (seconds from the start of `05-mid-reply.jsonl`): Claude forked at 2.49 and the source's count ended at 3.89. Codex forked at 4.55, source ended at 8.21. Grok forked at 12.30, and the source's count reply came at 14.12 (`ids.json` says 22.08 because that run took the time after the process exited; the script now takes it at the reply). In every step-5 branch the answer named ALPHA only, and every source counted to 20.

The deliberate break (`deliberate-break.txt`) fed the step-3 judge a saved answer naming BETA. It returned `"3": "fail"` and exit code 1.

## Notes

**O5 (Grok, prompts sent mid-turn).** Grok does not drop a prompt sent while a turn runs. It queues it (`_x.ai/queue/changed`) and runs it after the turn as its own prompt, with its own `session/prompt` reply (`stopReason: end_turn`, `_meta.promptId`). Evidence: `live-grok/05-mid-reply.jsonl`, request `r4` "Say the word GAMMA.", sent at 4.73 s and answered "GAMMA" at 18.96 s, after the count reply at 14.12 s. So every prompt Crush sends gets its own prompt index. Crush's count stays right as long as it counts every `session/prompt` sent, in send order. The walk-back rule for a missing marker (design 3.4) still covers older chats and is harmless. (A first try that sent GAMMA at the same moment as the branch's prompt hit Grok's limit of 2 model requests a second, `-32003 Rate limited`; the run above spaced them.)

**OpenCode: a real fork exists, but it could not be checked live.** OpenCode's stored OpenCode Go key has expired: "This API key has expired… Signing in again will not renew this key." The free `opencode/deepseek-v4.1-flash-free` model is refused with the same error. No login was started. Without a model, `live-opencode/03-fork-no-model.jsonl` shows:
- `POST /session/<id>/fork {"messageID": M}` keeps exactly the messages **before** M (cut at the second user message: kept user + assistant, 2 of 4).
- The fork's messages get **new message IDs**.
- ACP `session/fork` (advertised as `sessionCapabilities.fork`) works but copies the **whole** session: its parameters are `{sessionId, cwd, mcpServers}`, with no cut point. `opencode run --fork` also copies the whole session.

A live check of the model's memory in a fork (steps 2-5) is still needed before OpenCode forks. Until then, per design 3.4, OpenCode keeps `ErrNoFork`.

**AGY.** Its markers are the `step_index` of each finished `agent_response` step: ALPHA = 1, BETA = 4 (`ids.json`). `agy --help` has no fork or resume-at; as Q3 says, AGY always gets the written copy.

## Differences from design 3.4 that P4.2 must follow

1. **Grok's method name and index.** The method is `_x.ai/session/fork`, with a leading underscore (ACP's extension prefix). `x.ai/session/fork` and `session/fork` give "Method not found". It is not listed in `initialize`. Its parameters are `sourceSessionId`, `sourceCwd`, `newCwd` and `targetPromptIndex`; the Grok binary also names optional `newSessionId`, `newModelId`, `sessionKind` and `sourceWorkspaceDir`. Unknown fields are silently ignored. **`targetPromptIndex` is the 0-based index of the last prompt kept, so it is Crush's prompt-count marker minus one.** The first run, which sent the count itself (1), kept BETA, and steps 3 and 5 failed: `live-grok/first-run-index-is-count/`. A fork without a model call reports `chatMessagesCopied`: 8 for index 0 and 11 for 1, 2 and 3, on a 2-prompt chat.
2. **Grok's prompt reply has its own ID.** It carries `_meta.promptId` (= `requestId`). The fork takes the index, not this ID.
3. **OpenCode needs its HTTP API, not ACP, to cut.** The fork call is `POST /session/{id}/fork?directory=<cwd>` with `{"messageID": <the message after the cut>}`, through `opencode serve --port 0 --hostname 127.0.0.1` (it prints its address). The message after a cut reply is the next **user** message, whose ID the ACP stream does not give: Crush's markers are the assistant `messageId`s. P4.2 must list the session's messages (`GET /session/{id}/message?directory=<cwd>`) to find it. When the cut is the last message, forking with no `messageID`, or ACP `session/fork`, copies everything. The fork's message IDs are new, so markers for a branch of a branch must come from the fork's own stream.
4. **Claude's `--resume-session-at` is a hidden flag.** It is not in `claude --help`; `--fork-session` is. It works as the design says.
5. **Codex is as designed.** `lastTurnId` is inclusive ("turns after `last_turn_id` are omitted"; the turn "cannot be in progress"). The fork worked from a fresh app-server process that had never loaded the source, and while another process was running a turn on the source thread. `result.thread.turns` lists the turns copied (1 here).
6. **All three working forks** (Claude, Codex, Grok) can be made while the source is mid-reply, from a separate process. The branch gets only the finished part up to the cut, and the source finishes undisturbed (step 5).

## Usage

About 34 model turns ran, all tiny prompts:
- Claude: 6.
- Codex: 6.
- Grok: 20, over three runs (the first used the count as the index; the second sent no mid-turn prompt).
- AGY: 2.
- OpenCode: 6 prompts, all refused with the expired-key error.

Help, `initialize`, fork calls and the fork probes made no model calls.
