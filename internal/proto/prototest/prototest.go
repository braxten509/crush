// Package prototest helps tests build wire requests. It is a normal
// package, not a test file, so tests in every package can import it.
package prototest

import "github.com/charmbracelet/crush/internal/proto"

// CreateRequest returns w as this build's create request: w with Protocol
// set to proto.ProtocolVersion. Every test that builds a create request
// uses it (engine-design.md, Build pieces, S22).
func CreateRequest(w proto.Workspace) proto.Workspace {
	w.Protocol = proto.ProtocolVersion
	return w
}
