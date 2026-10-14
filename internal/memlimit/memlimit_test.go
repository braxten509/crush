package memlimit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitKeepsTotalUnderLimit(t *testing.T) {
	t.Parallel()
	ram, swap, limited := Split(16 << 30)
	require.True(t, limited)
	require.Equal(t, uint64(16<<30), ram+swap)
	require.Equal(t, uint64(16<<30)/5, swap)

	_, _, limited = Split(0)
	require.False(t, limited, "0 is no limit")
}

func TestSlicePath(t *testing.T) {
	t.Parallel()
	in := "0::/user.slice/user-1000.slice/user@1000.service/crush.slice/crush-42.scope\n"
	cg := cgroupPath(strings.NewReader(in))
	require.Equal(t, "/user.slice/user-1000.slice/user@1000.service/crush.slice/crush-42.scope", cg)
	require.Equal(t, "/user.slice/user-1000.slice/user@1000.service/crush.slice", slicePath(cg, Slice))

	tab := "/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.kde.konsole-6249.scope/tab(6257).scope"
	require.Empty(t, slicePath(tab, Slice))
	require.Empty(t, slicePath("/user.slice/x/notcrush.slice/a.scope", Slice))
}
