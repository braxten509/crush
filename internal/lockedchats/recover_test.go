package lockedchats

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/session"
)

// A crash at any step of a lock or unlock, then a restart's recovery,
// never leaves the chat without a whole copy and never wipes the only one.
// Each case stops the work where a crash would, checks a copy is left,
// recovers with a fresh service, and checks the chat ends locked with a
// complete vault copy that unlocks again.
func TestCrashAtEveryStepKeepsACopy(t *testing.T) {
	cases := []struct {
		name string
		// run gets the fixture to the crash.
		run func(f *fixture)
		// closed makes the vault's password box go unanswered at restart.
		closed bool
		// want is the state after recovery.
		want string
	}{
		{name: "lock before the vault copy is complete", run: crashLockAt("lock:copying"), want: session.LockStateLocked},
		{name: "lock with the vault copy complete, rows still here", run: crashLockAt("lock:copied"), want: session.LockStateLocked},
		{name: "lock committed, outside copies not wiped yet", run: crashLockAt("lock:committed"), want: session.LockStateLocked},
		{name: "unlock with files copied back, rows not yet", run: func(f *fixture) {
			f.lock()
			crashHook = crashAt("unlock:restored")
			_ = f.svc.Unlock(f.ctx, f.root)
			f.svc.Wait()
		}, want: session.LockStateLocked},
		{name: "unlocked when Crush died", run: func(f *fixture) {
			f.lock()
			f.unlock()
			abandon(f)
		}, want: session.LockStateLocked},
		{name: "unlocked when Crush died, password box cancelled", run: func(f *fixture) {
			f.lock()
			f.unlock()
			abandon(f)
		}, closed: true, want: session.LockStateRelock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.run(f)
			crashHook = func(string) error { return nil }
			require.True(t, f.outsideHas() || f.vaultHas(), "after the crash no whole copy is left")

			f.vault.Closed = tc.closed
			restarted := f.newService()
			f.svc = restarted
			restarted.Recover(f.ctx)
			restarted.Wait()

			require.Equal(t, tc.want, f.state())
			require.True(t, f.outsideHas() || f.vaultHas(), "after recovery no whole copy is left")
			if tc.want == session.LockStateRelock {
				require.True(t, f.outsideHas(), "a chat that could not go back keeps its outside copy")
				return
			}
			require.False(t, f.outsideHas(), "a recovered chat leaves nothing outside")
			require.True(t, f.vaultHas())
			require.NoFileExists(t, f.dataDir+"/transcripts/"+safeName(f.root)+".md")
			f.unlock()
			require.Equal(t, session.LockStateUnlocked, f.state())
			require.True(t, f.outsideHas())
		})
	}
}

func crashAt(step string) func(string) error {
	return func(at string) error {
		if at == step {
			return errCrash
		}
		return nil
	}
}

// crashLockAt locks the chat and dies at step.
func crashLockAt(step string) func(f *fixture) {
	return func(f *fixture) {
		crashHook = crashAt(step)
		require.NoError(f.t, f.svc.Lock(f.ctx, f.root))
		f.svc.Wait()
	}
}

// abandon makes the chat's owner in the registry a Crush that is gone.
func abandon(f *fixture) {
	path := registryPath()
	data, err := os.ReadFile(path)
	require.NoError(f.t, err)
	var entries []registryEntry
	require.NoError(f.t, json.Unmarshal(data, &entries))
	require.NotEmpty(f.t, entries)
	for i := range entries {
		entries[i].PID = 1 << 30 // no such process
	}
	data, err = json.Marshal(entries)
	require.NoError(f.t, err)
	require.NoError(f.t, os.WriteFile(path, data, 0o600))
}
