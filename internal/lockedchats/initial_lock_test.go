package lockedchats

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/session"
)

// A first lock interrupted before the vault opens has moved nothing. It
// must not turn into a protected chat that asks for the vault each start.
func TestRecoverFirstLockWaitingForVault(t *testing.T) {
	f := newFixture(t)
	before := f.chatRows()
	require.NoError(t, f.svc.startLock(f.ctx, f.root))
	f.vault.Closed = true
	f.svc = f.newService()
	f.svc.Recover(f.ctx)
	f.svc.Recover(f.ctx)
	require.Empty(t, f.state())
	require.Zero(t, f.vault.Opens, "recovery must not prompt for the vault")
	require.Equal(t, before, f.chatRows())
	require.Equal(t, "the secret chat", f.title())
	require.EqualValues(t, 1000, f.updatedAt(f.root))
}

func TestFailedFirstLockReturnsToNormal(t *testing.T) {
	f := newFixture(t)
	before := f.chatRows()
	f.vault.Closed = true
	f.lock()
	require.Empty(t, f.state())
	require.Equal(t, before, f.chatRows())
	// It also stays normal after a restart, without another password box.
	opens := f.vault.Opens
	f.svc = f.newService()
	f.svc.Recover(f.ctx)
	require.Equal(t, opens, f.vault.Opens)
}

// Relocking a previously protected chat still keeps it hidden when the
// vault cannot open, including a crash while waiting for the vault.
func TestRecoverInterruptedRelockKeepsChatHidden(t *testing.T) {
	f := newFixture(t)
	f.lock()
	f.unlock()
	require.NoError(t, f.svc.startLock(f.ctx, f.root))
	f.vault.Closed = true
	abandon(f)
	f.svc = f.newService()
	f.svc.Recover(f.ctx)
	require.Equal(t, session.LockStateRelock, f.state())
	require.True(t, f.outsideHas(), "keep the only up-to-date copy")
	require.True(t, f.vaultHas())
}
