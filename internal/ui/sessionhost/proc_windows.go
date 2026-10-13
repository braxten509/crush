//go:build windows

package sessionhost

import "os"

func terminate(p *os.Process) { _ = p.Kill() }

func kill(p *os.Process) { _ = p.Kill() }
