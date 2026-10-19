package remote

import (
	"context"
	"net/netip"
	"sync"
)

// TestTailnet stands in for Tailscale (SetTailnetForTest).
type TestTailnet struct {
	// Listen is the address that counts as this computer's tailnet
	// address, such as 127.0.0.1; shares and the launcher listen on it,
	// and requests from it count as the owner's devices.
	Listen netip.Addr
	// Host names this computer.
	Host string
	// User is the owning account's name. Nothing shows it today; the
	// account's ID is fixed.
	User string
	// Phones are the names the device lookup gives; the first is the name
	// of every phone (they all come from Listen).
	Phones []string
}

// testTailnetUser is the stand-in account's ID.
const testTailnetUser = 1

var testTailnet struct {
	sync.Mutex
	set bool
	tn  TestTailnet
}

// SetTailnetForTest makes the share and the launcher use tn instead of
// Tailscale, until restore runs. The crushtest seam `remote` uses it
// (desktop design 5-6, G-23).
func SetTailnetForTest(tn TestTailnet) (restore func()) {
	testTailnet.Lock()
	defer testTailnet.Unlock()
	savedSet, savedTN := testTailnet.set, testTailnet.tn
	testTailnet.set, testTailnet.tn = true, tn
	return func() {
		testTailnet.Lock()
		defer testTailnet.Unlock()
		testTailnet.set, testTailnet.tn = savedSet, savedTN
	}
}

// testTailnetAPI returns the stand-in, if one is set.
func testTailnetAPI() (tailnetAPI, bool) {
	testTailnet.Lock()
	defer testTailnet.Unlock()
	if !testTailnet.set {
		return nil, false
	}
	return fakeTailnet{tn: testTailnet.tn}, true
}

// isTestTailnetAddr reports whether a is the stand-in's address.
func isTestTailnetAddr(a netip.Addr) bool {
	testTailnet.Lock()
	defer testTailnet.Unlock()
	return testTailnet.set && testTailnet.tn.Listen.IsValid() && a == testTailnet.tn.Listen.Unmap()
}

// fakeTailnet answers as a running Tailscale signed in to one account.
type fakeTailnet struct{ tn TestTailnet }

func (f fakeTailnet) status(context.Context) (tailnetStatus, error) {
	var st tailnetStatus
	st.BackendState = "Running"
	st.Self.HostName = f.tn.Host
	st.Self.TailscaleIPs = []netip.Addr{f.tn.Listen}
	st.Self.UserID = testTailnetUser
	st.Self.Online = true
	return st, nil
}

func (f fakeTailnet) self(ctx context.Context) (self, error) {
	st, err := f.status(ctx)
	return st.self(), err
}

func (f fakeTailnet) whois(context.Context, string) (peer, error) {
	name := "phone"
	if len(f.tn.Phones) > 0 {
		name = f.tn.Phones[0]
	}
	return peer{User: testTailnetUser, Name: name}, nil
}
