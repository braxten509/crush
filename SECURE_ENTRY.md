# Secure entry

Agents can prepare a file and ask Crush to insert a secret without receiving it.
After updating Crush, restart the terminal session to load this feature.

For example, the agent first creates `.env` containing:

```dotenv
FIRST_API_KEY=FIRST_KEY_SLOT
SECOND_API_KEY=SECOND_KEY_SLOT
```

Then it asks for both keys in Crush's question form, as `secure_entry`
questions. They show as masked tabs like any other question, with one review
and one Submit:

```sh
crush ask <<'EOF'
{"questions":[
 {"type":"secure_entry","label":"First key","question":"Enter the first API key","description":"Saved into .env","file":"/absolute/path/to/.env","placeholder":"FIRST_KEY_SLOT"},
 {"type":"secure_entry","label":"Second key","question":"Enter the second API key","description":"Saved into .env","file":"/absolute/path/to/.env","placeholder":"SECOND_KEY_SLOT"}
]}
EOF
```

Secure questions can sit beside ordinary ones in the same form. For a single
secret, `crush secure-entry` is shorthand for a one-question form:

```sh
crush secure-entry --file /absolute/path/to/.env --placeholder FIRST_KEY_SLOT --label 'First API key'
```

Type or paste the key into the masked field. Escape cancels. Crush reports only
`saved` or `cancelled` for each secure question, in the same "Questions" result
as the ordinary answers. Save errors stay in the form and never include the
entered value or file contents.

The placeholder defaults to `%s`, and each secure question replaces exactly one
occurrence. Unique markers such as `FIRST_KEY_SLOT` avoid ambiguity, including
when a key itself contains `%s`; `occurrence` (or `--occurrence 2`) selects a
later occurrence of a marker, counted in the file as prepared. Replacement is
literal, with no shell expansion or automatic format escaping; prepare the
surrounding format accordingly. Input must be a nonempty single line of at most
64 KiB. Template files must be existing regular files of at most 8 MiB.

Crush checks that the file hasn't changed, writes a private temporary file in
the same directory, and atomically replaces the destination with mode `0600`.
Symbolic-link destinations are rejected. On failure or cancellation, the
original file remains untouched.

The value goes directly from the local TUI's memory to the file writer. It is
never placed in question answers, chat messages, task request/reply files,
Crush logs, CLI stdin/arguments, model input, or remote APIs. The temporary file
is removed after replacement or an ordinary error. A crash during writing can
leave a private temporary file in the destination directory.

This flow requires the **local Crush terminal**; phone and server clients cannot
submit secure entries. Secrets still exist briefly in process memory, and the
system clipboard, OS swap, backups, or external terminal recorders are outside
this feature's logging guarantees.

## Secrets guard

Keep secrets in a `secrets` folder such as `~/.config/secrets` (or Crush's own
`~/.config/crush/secrets`), and let small helper programs read them: agents
never need to. Crush blocks agent tool calls that would read those folders:

- Crush's own tools, including sub-agents and background jobs.
- Claude Code and Codex started by Crush, through a `PreToolUse` hook
  (`crush secrets-guard`). Crush adds the hook when it starts them, and trusts
  this one hook in Codex, which otherwise skips new hooks until approved.

A call is blocked when it names a path into a `secrets` folder, the folder name
next to a `.config` path, or a `.config` glob that could expand to one. Writing
or editing files is allowed, so agents can still prepare templates and write
helpers; `crush secure-entry` and `crush ask` are allowed on their own.

To guard the same CLIs outside Crush, add the hook to their own settings, for
example in `~/.claude/settings.json`:

```json
{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"crush secrets-guard","timeout":10}]}]}}
```

This stops accidental and prompted reads. It is not a sandbox: a program running
as your user can still open the files, and an agent could write a helper that
prints a secret. Keep populated secret files out of source control and automatic
context inputs.
