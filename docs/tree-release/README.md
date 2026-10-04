# Local tree release evidence

All builds and tests used the installed `crush bg` tracker and
`timeout 900 systemd-run --user --wait --pipe --collect`.
The worker had no inherited session ID, so a detached, silent scratch Crush
session tracked the jobs. No test binary opened a real data directory.

- `native-history.json`: local fixture request histories for branch isolation,
  opt-in summary scope, clone, fork, and editing an earlier prompt.
- `claude-clone.txt`, `claude-fork.txt`, `claude-summary.txt`: detached terminal
  captures of tiny live Haiku checks. The new branch, clone and fork remember
  amber, without violet from the abandoned branch. Summaries use a fresh,
  tool-free request with an explicitly delimited transcript.
- `tree-80x24.txt`, `tree-40x12.txt`: selector rendering in detached terminals.
- `go-test.txt`: final full test result after merging on `cli-agents`.

Migration used an online SQLite backup of
`~/dev/crush/.crush/crush.db`, discovered through the projects list. On the
scratch copy, all 82 messages in two sessions retained their IDs, content,
roles, model/provider fields, summary flags and creation timestamps. The hash
of those fields stayed
`6750f6765888c452ba6ebb58f600c8ccb8f8d6b91cc051a9824449e6a664839f`.
Every old linear path matched its migrated ancestry; SQLite integrity passed.
The old and new chat terminal captures matched byte for byte. Private chat
captures and the copied database remain in `/tmp/tree-ship-qa/`, not this repo.

The actual previously installed binary was run against that upgraded copy.
It exited before loading a chat with:
`no such function: Upgrade Crush: this database uses session trees`.

New regression tests cover summary success, failure and cancellation,
common-ancestor boundaries, transactional note insertion, root selection,
clone/fork copies, preview limits, fold/filter behavior, prompt selection,
and SQLite paths with `#` or `?`. The existing full suite also covers CLI
handoffs, queue safety, reviews and five-prompt diff retention.

The main home database (`~/.crush/crush.db`, about 858 MiB) was also backed up
with SQLite and migrated only in a scratch directory. It contained **27,851
messages across 199 sessions**. All message fields and every ancestry path
matched before and after. Its saved-chat terminal content matched (the live
account footer was excluded), and SQLite integrity passed. Message-field hash:
`f54442f50530e781f07e30aa6f890844e5af1c13afbedf02fa708c63d1a7e996`.

Final merged checks: tracked job `01C` ran the full suite on `cli-agents`, and
job `01D` built the release there; both succeeded. Jobs `01B` and `021` verified
the small and large database copies. Earlier failing checks were corrected
before these final runs; the SQLite `#` path regression is covered explicitly.
