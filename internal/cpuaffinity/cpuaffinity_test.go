package cpuaffinity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseList(t *testing.T) {
	t.Parallel()

	cpus, err := ParseList(" 12-14, 3,0-1,3\n")
	require.NoError(t, err)
	require.Equal(t, []int{0, 1, 3, 12, 13, 14}, cpus)

	for _, bad := range []string{"", "a", "5-2", "-1", "1-x"} {
		_, err := ParseList(bad)
		require.Error(t, err, bad)
	}
}

func TestFormatList(t *testing.T) {
	t.Parallel()

	require.Equal(t, "6-11,18-23", FormatList([]int{18, 19, 20, 21, 22, 23, 6, 7, 8, 9, 10, 11}))
	require.Equal(t, "0,2,4-5", FormatList([]int{4, 0, 5, 2, 2}))
	require.Equal(t, "", FormatList(nil))
}
