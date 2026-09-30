package agent

import "testing"

func TestSharedMemoryEnv(t *testing.T) {
	for _, tc := range []struct {
		value         string
		off, readOnly bool
	}{
		{"", false, false},
		{"off", true, false},
		{"readonly", false, true},
	} {
		t.Setenv(sharedMemoryEnv, tc.value)
		if sharedMemoryOff() != tc.off || sharedMemoryReadOnly() != tc.readOnly {
			t.Errorf("CRUSH_SHARED_MEMORY=%q: off=%v readonly=%v, want %v %v", tc.value, sharedMemoryOff(), sharedMemoryReadOnly(), tc.off, tc.readOnly)
		}
	}
}
