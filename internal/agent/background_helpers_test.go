package agent

import (
	"slices"
	"testing"
)

func registerTestHub(t *testing.T) *taskHub {
	h := &taskHub{dir: t.TempDir()}
	hubsMu.Lock()
	hubs = append(hubs, h)
	hubsMu.Unlock()
	t.Cleanup(func() {
		hubsMu.Lock()
		hubs = slices.DeleteFunc(hubs, func(x *taskHub) bool { return x == h })
		hubsMu.Unlock()
	})
	return h
}
