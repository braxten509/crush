package backend

// lifecycleHooks are piece A's client hooks (engine lifecycle and wire,
// engine-design.md 5.1, 5.2, 5.10 and 5.14): the waiting-for-an-answer
// watcher starts at a workspace's first agent start and stops at its
// teardown.
func (b *Backend) lifecycleHooks() clientHooks {
	return clientHooks{
		agentInit: func(ws *Workspace, _ string, _ bool) { b.startPromptWatcher(ws) },
		teardown:  b.stopPromptWatcher,
	}
}
