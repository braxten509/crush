package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetFocusSyncsTextareaAndChat(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.setFocus(uiFocusMain)
	require.Equal(t, uiFocusMain, u.focus)
	require.False(t, u.textarea.Focused())
	require.True(t, u.chat.Focused())

	u.setFocus(uiFocusSidebar)
	require.False(t, u.textarea.Focused())
	require.False(t, u.chat.Focused())

	u.setFocus(uiFocusEditor)
	require.True(t, u.textarea.Focused())
	require.False(t, u.chat.Focused())
}

func TestSetFocusOptions(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.setFocus(uiFocusMain, focusRecordOnly)
	require.Equal(t, uiFocusMain, u.focus)
	require.True(t, u.textarea.Focused(), "record only")
	require.False(t, u.chat.Focused(), "record only")

	u.setFocus(uiFocusMain, keepTextareaFocus)
	require.True(t, u.textarea.Focused())
	require.True(t, u.chat.Focused())

	u.setFocus(uiFocusEditor, keepChatFocus)
	require.True(t, u.textarea.Focused())
	require.True(t, u.chat.Focused())
}

func TestOpenInlineKeepsChatFocus(t *testing.T) {
	t.Parallel()

	u := newRoutingUI(t)
	u.setFocus(uiFocusMain)
	inline := &recInline{}
	u.openInline(inline)
	require.Equal(t, uiFocusEditor, u.focus)
	require.Same(t, inline, u.activeInline)
	require.True(t, inline.focused)
	require.False(t, u.textarea.Focused(), "the inline editor replaces the textarea")
	require.True(t, u.chat.Focused(), "the chat looks as it did")
}
