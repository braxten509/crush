package model

import (
	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

// helpBindings builds the bindings the short and the full help share; each
// arranges them its own way.
type helpBindings struct {
	m *UI
	k *KeyMap
}

func (m *UI) helpBindings() helpBindings {
	return helpBindings{m: m, k: &m.keyMap}
}

// commands is the commands key, which "/" also opens on an empty editor.
func (h helpBindings) commands() key.Binding {
	commands := h.k.Commands
	if h.m.focus == uiFocusEditor && h.m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "commands")
	}
	return commands
}

// running is cancel, and interrupt with prompts queued, while the agent
// works.
func (h helpBindings) running() []key.Binding {
	if !h.m.isAgentBusy() {
		return nil
	}
	cancel := h.k.Chat.Cancel
	if h.m.promptQueue == 0 {
		return []key.Binding{cancel}
	}
	cancel.SetHelp("esc", "interrupt, send queued")
	return []key.Binding{cancel, h.k.Chat.Interrupt}
}

// backgroundCommand reports whether a running command can move to the
// background.
func (h helpBindings) backgroundCommand() bool {
	return h.m.hasSession() && h.m.chat.BackgroundableGroup() != nil
}

// pillSections reports whether the pill section keys apply.
func (h helpBindings) pillSections() bool {
	return h.m.pillsExpanded && hasIncompleteTodos(h.m.session.Todos) && h.m.promptQueue > 0
}

// editor is the focused editor's keys.
func (h helpBindings) editor() []key.Binding {
	k := h.k
	binds := []key.Binding{
		k.Editor.Newline,
		k.Editor.MentionFile,
		k.Editor.OpenEditor,
		k.Editor.PasteText,
		k.Editor.SelectAll,
		k.Editor.CopySelection,
		k.Editor.CutSelection,
	}
	if h.m.currentModelSupportsImages() {
		binds = append(binds, k.Editor.AddImage, k.Editor.PasteImage)
	}
	return binds
}

// editorRows is the full help's rows for the focused editor: its keys and,
// with attachments, theirs.
func (h helpBindings) editorRows() [][]key.Binding {
	rows := [][]key.Binding{h.editor()}
	if h.m.attachments.Count() > 0 {
		rows = append(rows, []key.Binding{
			h.k.Editor.AttachmentDeleteMode,
			h.k.Editor.DeleteAllAttachments,
			h.k.Editor.Escape,
		})
	}
	return rows
}

// ShortHelp implements [help.KeyMap].
func (m *UI) ShortHelp() []key.Binding {
	h := m.helpBindings()
	k := h.k

	// When an inline editor is active, show its help.
	if m.activeInline != nil {
		if m.focus == uiFocusEditor {
			return m.activeInline.ShortHelp()
		}
		return []key.Binding{
			m.inlineFocusHelp(),
			k.Chat.UpDown,
			k.Chat.PageUp,
			k.Chat.PageDown,
		}
	}

	var binds []key.Binding
	switch m.state {
	case uiInitialize:
		binds = append(binds, k.Quit)
	case uiChat:
		binds = append(binds, h.running()...)
		binds = append(binds, m.tabHelp(), k.ShiftTab, h.commands(), k.Models)
		if h.backgroundCommand() {
			binds = append(binds, k.Chat.BackgroundCommand)
		}
		switch m.focus {
		case uiFocusEditor:
			binds = append(binds, k.Editor.Newline)
		case uiFocusSidebar:
			binds = append(binds, k.Chat.UpDown, k.Chat.FocusChat)
		case uiFocusMain:
			binds = append(binds,
				k.Chat.UpDown,
				k.Chat.UpDownOneItem,
				k.Chat.PageUp,
				k.Chat.PageDown,
				k.Chat.Copy,
			)
			if h.pillSections() {
				binds = append(binds, k.Chat.PillLeft)
			}
		}
	default:
		landingTab := k.Tab
		landingTab.SetEnabled(!m.composerFocusOnly())
		binds = append(binds, landingTab, h.commands(), k.ShiftTab, k.Models, k.Editor.Newline)
	}
	return append(binds, k.Quit, k.Help)
}

// FullHelp implements [help.KeyMap].
func (m *UI) FullHelp() [][]key.Binding {
	h := m.helpBindings()
	k := h.k

	// When an inline editor is active, show its help.
	if m.activeInline != nil {
		if m.focus == uiFocusEditor {
			return [][]key.Binding{m.activeInline.ShortHelp()}
		}
		return [][]key.Binding{
			{m.inlineFocusHelp()},
			{k.Chat.UpDown, k.Chat.UpDownOneItem, k.Chat.PageUp, k.Chat.PageDown},
			{k.Chat.HalfPageUp, k.Chat.HalfPageDown, k.Chat.Home, k.Chat.End},
		}
	}

	var binds [][]key.Binding
	switch m.state {
	case uiInitialize:
		binds = append(binds, []key.Binding{k.Quit})
	case uiChat:
		for _, b := range h.running() {
			binds = append(binds, []key.Binding{b})
		}
		mainBinds := []key.Binding{m.tabHelp(), k.ShiftTab, h.commands(), k.Models, k.Sessions, k.ToggleYolo}
		if m.hasSession() {
			mainBinds = append(mainBinds, k.Chat.NewSession, k.Chat.EndFollow)
		}
		if h.backgroundCommand() {
			mainBinds = append(mainBinds, k.Chat.BackgroundCommand)
		}
		binds = append(binds, mainBinds)
		binds = append(binds, h.focusRows()...)
	default:
		if m.session == nil {
			// no session selected
			binds = append(binds, []key.Binding{k.ShiftTab, h.commands(), k.Models, k.Sessions, k.ToggleYolo})
			binds = append(binds, h.editorRows()...)
		}
	}

	help := k.Help
	help.SetHelp("ctrl+g", "less")
	return append(binds, []key.Binding{help, k.Quit})
}

// focusRows is the full help's rows for the focused pane of the chat.
func (h helpBindings) focusRows() [][]key.Binding {
	k := h.k
	switch h.m.focus {
	case uiFocusEditor:
		return h.editorRows()
	case uiFocusSidebar:
		return [][]key.Binding{
			{k.Chat.UpDown},
			{k.Chat.FocusChat},
			{k.Chat.Home, k.Chat.End},
		}
	case uiFocusMain:
		rows := [][]key.Binding{
			{k.Chat.UpDown, k.Chat.UpDownOneItem, k.Chat.PageUp, k.Chat.PageDown},
			{k.Chat.HalfPageUp, k.Chat.HalfPageDown, k.Chat.Home, k.Chat.End, k.Chat.EndFollow, k.Chat.FocusSidebar},
			{k.Chat.Copy, k.Chat.ClearHighlight},
		}
		if h.pillSections() {
			rows = append(rows, []key.Binding{k.Chat.PillLeft})
		}
		return rows
	}
	return nil
}

// tabHelp is the Tab binding's help for the current focus. It's disabled
// (hidden from help) with composer_focus_only, where Tab does nothing.
func (m *UI) tabHelp() key.Binding {
	tab := m.keyMap.Tab
	if m.composerFocusOnly() {
		tab.SetEnabled(false)
		return tab
	}
	if m.focus == uiFocusEditor {
		tab.SetHelp("tab", "focus chat")
	} else {
		tab.SetHelp("tab", "focus editor")
	}
	return tab
}

// inlineFocusHelp returns the binding that restores a blurred inline
// editor: Tab, or a click with composer_focus_only. Collapsible editors
// provide context-specific wording.
func (m *UI) inlineFocusHelp() key.Binding {
	description := "focus editor"
	if collapsed, ok := m.activeInline.(dialog.CollapsibleInlineEditor); ok {
		description = collapsed.CollapsedHelp()
	}
	if m.composerFocusOnly() {
		return key.NewBinding(key.WithKeys("click"), key.WithHelp("click", description))
	}
	tab := m.keyMap.Tab
	tab.SetHelp("tab", description)
	return tab
}
