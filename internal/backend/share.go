package backend

// shareState is piece D's state per workspace (phone sharing and secure
// entry, engine-design.md 5.7 and 5.8).
type shareState struct{}

// shareHooks are piece D's client hooks.
func (b *Backend) shareHooks() clientHooks { return clientHooks{} }
