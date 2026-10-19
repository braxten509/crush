package cliagent

// claudeForkArgs makes Claude Code copy the session p.Native up to and
// including its message p.Marker into a new session, which the turn then
// continues. --resume-session-at is a hidden flag (not in `claude --help`);
// the live check found it works. The new session's ID comes on the turn's
// system/init line, as a fresh session's does.
func claudeForkArgs(p ForkPoint) []string {
	return []string{"--resume", p.Native, "--fork-session", "--resume-session-at", p.Marker}
}
