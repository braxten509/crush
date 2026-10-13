package model

import (
	"image"
	"testing"

	"github.com/charmbracelet/crush/internal/ui/completions"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

func newTestMention() mentionCompletions {
	sty := styles.CharmtonePantera()
	return mentionCompletions{popup: completions.New(sty.Completions.Normal, sty.Completions.Focused, sty.Completions.Match)}
}

func TestMentionCompletionsTrigger(t *testing.T) {
	t.Parallel()

	c := newTestMention()
	require.True(t, c.triggers("@", ""), "at the start of the prompt")
	require.True(t, c.triggers("@", "look at "), "after whitespace")
	require.False(t, c.triggers("@", "mail"), "inside a word")
	require.False(t, c.triggers("a", ""))

	_ = c.begin(8, image.Pt(3, 4), 1, 10)
	require.True(t, c.open)
	require.Equal(t, 8, c.startIndex)
	require.Equal(t, image.Pt(3, 4), c.position)
	require.False(t, c.triggers("@", ""), "already open")
}

func TestMentionCompletionsFollow(t *testing.T) {
	t.Parallel()

	c := newTestMention()
	_ = c.begin(4, image.Point{}, 1, 10)

	c.follow("@", "see @", "@")
	require.True(t, c.open, "the opening keystroke is skipped")

	c.follow("m", "see @ma", "@ma")
	require.True(t, c.open)
	require.Equal(t, "ma", c.query)

	c.follow("space", "see @ma ", "")
	require.False(t, c.open, "a space ends the mention")

	_ = c.begin(4, image.Point{}, 1, 10)
	c.follow("backspace", "see", "see")
	require.False(t, c.open, "the cursor moved before the @")
	require.Zero(t, c.startIndex)

	_ = c.begin(4, image.Point{}, 1, 10)
	c.follow("left", "see @ma", "see")
	require.False(t, c.open, "the word is no mention any more")
}
