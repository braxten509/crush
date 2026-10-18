package backend

// lifecycleHooks are piece A's client hooks (engine lifecycle and wire,
// engine-design.md 5.1, 5.2, 5.10 and 5.14).
func (b *Backend) lifecycleHooks() clientHooks { return clientHooks{} }
