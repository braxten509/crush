// Package shelltool implements the agent tools that run shell commands:
// bash, job_output and job_kill, with the command safety checks they share.
// Their names and parameter types live in the parent tools package, which
// the UI and the wire protocol share, as does the registry of foreground
// shells the UI can move to the background.
package shelltool
