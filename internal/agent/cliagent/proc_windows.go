package cliagent

import "os/exec"

// ponytail: on Windows only the CLI itself is killed; a job object would
// take its children too.
func ownGroup(*exec.Cmd) {}

func (p *proc) kill() { _ = p.cmd.Process.Kill() }

func (p *proc) killCommands([]openCall) {}
