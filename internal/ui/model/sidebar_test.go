package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSidebarPaneScrolling(t *testing.T) {
	t.Parallel()

	var s sidebarPane
	s.setContent(strings.Repeat("line\n", 29)+"line", "", 20, 10)
	require.True(t, s.scrollable)
	require.Equal(t, 30, s.totalLines)
	require.Equal(t, 20, s.maxOffset)

	require.Nil(t, s.wheel(0))
	require.NotNil(t, s.wheel(50))
	require.Equal(t, 20, s.offset, "the wheel stops at the end")
	require.True(t, s.scrollbarVisible)

	// An outdated hide timer, or one firing while focused, keeps it.
	s.hideScrollbar(s.scrollbarSeq-1, false)
	require.True(t, s.scrollbarVisible)
	s.hideScrollbar(s.scrollbarSeq, true)
	require.True(t, s.scrollbarVisible)
	s.hideScrollbar(s.scrollbarSeq, false)
	require.False(t, s.scrollbarVisible)

	s.scrollUp()
	require.Equal(t, 16, s.offset)
	seq := s.scrollbarSeq
	s.scrollToBottom()
	s.scrollDown()
	require.Equal(t, 20, s.offset)
	require.Equal(t, seq+1, s.scrollbarSeq, "scrolling down at the end changes nothing")
	s.scrollToTop()
	require.Zero(t, s.offset)

	// Shorter content pulls the offset back into range.
	s.offset = 20
	s.setContent("a\nb", "", 20, 10)
	require.False(t, s.scrollable)
	require.Zero(t, s.offset)
}
