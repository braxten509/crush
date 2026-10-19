package cliagent

// codexForkParams turns the params of thread/start into those of
// thread/fork: a new thread holding the source thread's turns through
// p.Marker. lastTurnId is inclusive ("turns after last_turn_id are
// omitted"), so the cut's last whole turn is the marker. The answer has the
// new thread as result.thread.id, as thread/start's does.
func codexForkParams(p ForkPoint, params map[string]any) map[string]any {
	params["threadId"] = p.Native
	params["lastTurnId"] = p.Marker
	return params
}
