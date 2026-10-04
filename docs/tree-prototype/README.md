# Prototype evidence

- `go-test.txt`: complete successful tracked `go test ./...` output (job 009).
- `native-history.json`: the exact test user messages observed by the local
  native API fixture. System text, tracker-chat messages and credentials are
  excluded. Compare the last two requests: OLD and NEW stay separate.
- `api_fixture.py`: a local OpenAI-compatible endpoint for repeating that
  transport test without credentials. Run it through `crush bg` with a timeout.
- `claude-new.txt`: detached tmux capture of live Haiku remembering only amber
  on the new branch.
- `claude-return.txt`: live Haiku remembering amber and violet again after
  returning to the old branch.
- `tree-selector.txt`: the final selector after restart, showing both paths
  and the saved bookmark.

Build through `crush bg -- 'timeout 900 go build -buildvcs=false -o /tmp/crush-pi-tree .'`.
Run the binary in detached tmux with a new scratch `-D` directory, an isolated
`CRUSH_GLOBAL_CONFIG` directory and `options.notifications: "disabled"`.
For the native fixture, define an `openai-compat` provider at
`http://127.0.0.1:18479/v1`, model `tree-fixture`, and dummy API key `fixture`.
For live Claude, use provider `claude-code`, model `haiku`, and the existing
login. Do not change credentials or Pi's configuration.

Enter two small turns, open `/tree`, choose an earlier reply, and submit a
new prompt. Open `/tree` again to return to the old leaf. Ctrl+R labels the
selected entry. Search finds the label. Restart with `--continue` to check
persistence. Stop fixture jobs with `crush bg --stop ID` and close only the
named test tmux sessions.
