# Background commands from agent CLIs

Use `crush bg` inside a Crush-managed agent session to run a command independently of the current agent turn:

```sh
crush bg --name 'UI tests' -- 'timeout 600 go test ./internal/ui/...'
crush bg --output 001
crush bg --stop 001
```

A single command argument is a shell script. Multiple arguments are quoted individually, so `crush bg -- printf '%s' 'literal text'` preserves the argument boundaries. The job starts in the caller's working directory.

Crush uses its normal shell tool, including permission checks, configured hooks, command restrictions, and file-change reviews. Jobs keep running when the caller's turn ends. Running jobs appear in the background-process list; stopping one there cancels the whole shell script. Crush sends the owning conversation a completion message with output and the exit status. Other conversations cannot read or stop it through `crush bg`.

Commands that finish during the initial shell check return their result directly. For jobs that continue running, the returned ID works with `--output` and `--stop`. Completed output uses the existing shell manager's retention policy. Give commands an appropriate timeout if they might hang. For builds launched through a service manager, keep its waiting client inside the tracked job, for example `crush bg -- 'systemd-run --user --wait --pipe --collect timeout 600 go test ./...'`.

Detached commands launched with `nohup` or a trailing `&` are also detected on Linux, even below the normal age threshold. This fallback relies on process-table scans and can miss very short commands. Its completion message says only that the process ended: Crush cannot recover an orphan's exit code or redirected output. Use `crush bg` when reliable ownership and completion reporting matter.
