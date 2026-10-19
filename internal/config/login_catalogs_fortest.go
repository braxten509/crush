package config

import "sync/atomic"

// loginCatalogsSkipped makes a sign-in's save fetch no model catalog
// (SkipLoginCatalogsForTest).
var loginCatalogsSkipped atomic.Bool

// SkipLoginCatalogsForTest makes saving a sign-in's credential fetch no
// model catalog (Hyper's provider, ChatGPT's and Grok's models), so a
// check signs in with no network, until restore runs. The crushtest seam
// `auth` uses it (desktop design 5-6, G-24).
func SkipLoginCatalogsForTest() (restore func()) {
	prev := loginCatalogsSkipped.Swap(true)
	return func() { loginCatalogsSkipped.Store(prev) }
}
