package cliagent

import "encoding/json"

// rpcMessage is a JSON-RPC 2.0 request, notification or response, as Codex's
// app-server and the Agent Client Protocol CLIs exchange them.
type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}
