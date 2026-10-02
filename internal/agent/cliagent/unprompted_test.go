package cliagent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnpromptedReplyGoesToTheWorkspaceThatOwnsTheSession(t *testing.T) {
	var mu sync.Mutex
	var got []string
	owner := func(name, session string) func(string) bool {
		return func(id string) bool {
			if id != session {
				return false
			}
			mu.Lock()
			got = append(got, name)
			mu.Unlock()
			return true
		}
	}
	removeA := OnUnprompted(owner("a", "session-a"))
	removeB := OnUnprompted(owner("b", "session-b"))
	defer removeB()

	notifyUnprompted("session-a")
	notifyUnprompted("session-b")
	notifyUnprompted("unknown")
	removeA()
	notifyUnprompted("session-a")

	require.Equal(t, []string{"a", "b"}, got)
}

func TestUnpromptedRegistrationIsRaceFree(t *testing.T) {
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			remove := OnUnprompted(func(string) bool { return false })
			notifyUnprompted("nobody")
			remove()
		})
	}
	wg.Wait()
}
