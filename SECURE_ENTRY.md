# Secure entry

Agents can prepare a file and ask Crush to insert a secret without receiving it.
After updating Crush, restart the terminal session to load this feature.

For example, the agent first creates `.env` containing:

```dotenv
FIRST_API_KEY=%s
SECOND_API_KEY=%s
```

Then it opens the first dialog:

```sh
crush secure-entry --file /absolute/path/to/.env --label 'First API key'
```

Type or paste the key into the **Secure entry** dialog and press Enter. Escape
cancels. Input is masked; Ctrl+U clears it, and arrow/Home/End keys support editing.
Crush reports only `saved` or `cancelled` to the agent. Save errors stay in the
dialog and never include the entered value or file contents.

For the second key, open another dialog for the same file. The first remaining
`%s` is replaced, preserving the earlier key without the agent reading it:

```sh
crush secure-entry --file /absolute/path/to/.env --label 'Second API key'
```

For distinct slots, use unique template markers such as `FIRST_KEY_SLOT` and
`SECOND_KEY_SLOT`, then pass `--placeholder FIRST_KEY_SLOT`. This also avoids
ambiguity if a key itself contains `%s`. `--occurrence 2` selects the second
remaining occurrence of a marker. Each dialog replaces exactly one occurrence.
Replacement is literal, with no shell expansion or automatic format escaping;
prepare the surrounding format accordingly. Input must be a nonempty single line
of at most 64 KiB. Template files must be existing regular files of at most 8 MiB.

The destination is shown before entry. Crush checks that the file hasn't changed,
writes a private temporary file in the same directory, and atomically replaces
the destination with mode `0600`. Symbolic-link destinations are rejected. On
failure or cancellation, the original file remains untouched. A failed save
clears the input; enter it again to retry, or cancel and reopen if the file changed.

The value goes directly from the local TUI's memory to the file writer. It is
never placed in question answers, chat messages, task request/reply files,
Crush logs, CLI stdin/arguments, model input, or remote APIs. The temporary file
is removed after replacement or an ordinary error. A crash during writing can
leave a private temporary file in the destination directory.

This protects the entry channel. It does not sandbox later agent tools or other
programs running as your user: they can still read files you own. Agents are
instructed never to read, print, diff, attach, or commit populated secret files.
Keep those files out of source control and automatic context inputs. This flow
requires the **local Crush terminal**; phone and server clients cannot submit
secure entries. Secrets still exist briefly in process memory, and the system
clipboard, OS swap, backups, or external terminal recorders are outside this
feature's logging guarantees.
