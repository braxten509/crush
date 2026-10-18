package backend

// treeState is piece C's state per workspace (tree actions and branch copy,
// engine-design.md 5.6 and 5.11).
type treeState struct{}

// treeHooks are piece C's client hooks.
func (b *Backend) treeHooks() clientHooks { return clientHooks{} }
