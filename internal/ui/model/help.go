package model

import (
	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/crush/internal/ui/dialog"
)

// ShortHelp implements [help.KeyMap].
func (m *UI) ShortHelp() []key.Binding {
	var binds []key.Binding
	k := &m.keyMap

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

	tab := m.tabHelp()
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "commands")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds, k.Quit)
	case uiChat:
		// Show cancel binding if agent is busy.
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.promptQueue > 0 {
				cancelBinding.SetHelp("esc", "interrupt, send queued")
			}
			binds = append(binds, cancelBinding)
			if m.promptQueue > 0 {
				binds = append(binds, k.Chat.Interrupt)
			}
		}

		binds = append(
			binds,
			tab,
			k.ShiftTab,
			commands,
			k.Models,
		)

		if m.hasSession() && m.chat.BackgroundableGroup() != nil {
			binds = append(binds, k.Chat.BackgroundCommand)
		}

		switch m.focus {
		case uiFocusEditor:
			binds = append(
				binds,
				k.Editor.Newline,
			)
		case uiFocusSidebar:
			binds = append(
				binds,
				k.Chat.UpDown,
				k.Chat.FocusChat,
			)
		case uiFocusMain:
			binds = append(
				binds,
				k.Chat.UpDown,
				k.Chat.UpDownOneItem,
				k.Chat.PageUp,
				k.Chat.PageDown,
				k.Chat.Copy,
			)
			if m.pillsExpanded && hasIncompleteTodos(m.session.Todos) && m.promptQueue > 0 {
				binds = append(binds, k.Chat.PillLeft)
			}
		}
	default:
		// TODO: other states
		// if m.session == nil {
		// no session selected
		landingTab := k.Tab
		landingTab.SetEnabled(!m.composerFocusOnly())
		binds = append(binds,
			landingTab,
			commands,
			k.ShiftTab,
			k.Models,
			k.Editor.Newline,
		)
	}

	binds = append(
		binds,
		k.Quit,
		k.Help,
	)

	return binds
}

// FullHelp implements [help.KeyMap].
func (m *UI) FullHelp() [][]key.Binding {
	// When an inline editor is active, show its help.
	if m.activeInline != nil {
		if m.focus == uiFocusEditor {
			return [][]key.Binding{m.activeInline.ShortHelp()}
		}
		return [][]key.Binding{
			{m.inlineFocusHelp()},
			{
				m.keyMap.Chat.UpDown,
				m.keyMap.Chat.UpDownOneItem,
				m.keyMap.Chat.PageUp,
				m.keyMap.Chat.PageDown,
			},
			{
				m.keyMap.Chat.HalfPageUp,
				m.keyMap.Chat.HalfPageDown,
				m.keyMap.Chat.Home,
				m.keyMap.Chat.End,
			},
		}
	}

	var binds [][]key.Binding
	k := &m.keyMap
	help := k.Help
	help.SetHelp("ctrl+g", "less")
	hasAttachments := m.attachments.Count() > 0
	hasSession := m.hasSession()
	commands := k.Commands
	if m.focus == uiFocusEditor && m.textarea.Value() == "" {
		commands.SetHelp("/ or ctrl+p", "commands")
	}

	switch m.state {
	case uiInitialize:
		binds = append(binds,
			[]key.Binding{
				k.Quit,
			})
	case uiChat:
		// Show cancel binding if agent is busy.
		if m.isAgentBusy() {
			cancelBinding := k.Chat.Cancel
			if m.promptQueue > 0 {
				cancelBinding.SetHelp("esc", "interrupt, send queued")
			}
			binds = append(binds, []key.Binding{cancelBinding})
			if m.promptQueue > 0 {
				binds = append(binds, []key.Binding{k.Chat.Interrupt})
			}
		}

		mainBinds := []key.Binding{}
		mainBinds = append(
			mainBinds,
			m.tabHelp(),
			k.ShiftTab,
			commands,
			k.Models,
			k.Sessions,
			k.ToggleYolo,
		)
		if hasSession {
			mainBinds = append(mainBinds, k.Chat.NewSession, k.Chat.EndFollow)
		}
		if m.hasSession() && m.chat.BackgroundableGroup() != nil {
			mainBinds = append(mainBinds, k.Chat.BackgroundCommand)
		}

		binds = append(binds, mainBinds)

		switch m.focus {
		case uiFocusEditor:
			editorBinds := []key.Binding{
				k.Editor.Newline,
				k.Editor.MentionFile,
				k.Editor.OpenEditor,
				k.Editor.PasteText,
				k.Editor.SelectAll,
				k.Editor.CopySelection,
				k.Editor.CutSelection,
			}
			if m.currentModelSupportsImages() {
				editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
			}
			binds = append(binds, editorBinds)
			if hasAttachments {
				binds = append(
					binds,
					[]key.Binding{
						k.Editor.AttachmentDeleteMode,
						k.Editor.DeleteAllAttachments,
						k.Editor.Escape,
					},
				)
			}
		case uiFocusSidebar:
			binds = append(
				binds,
				[]key.Binding{
					k.Chat.UpDown,
				},
				[]key.Binding{
					k.Chat.FocusChat,
				},
				[]key.Binding{
					k.Chat.Home,
					k.Chat.End,
				},
			)
		case uiFocusMain:
			binds = append(
				binds,
				[]key.Binding{
					k.Chat.UpDown,
					k.Chat.UpDownOneItem,
					k.Chat.PageUp,
					k.Chat.PageDown,
				},
				[]key.Binding{
					k.Chat.HalfPageUp,
					k.Chat.HalfPageDown,
					k.Chat.Home,
					k.Chat.End,
					k.Chat.EndFollow,
					k.Chat.FocusSidebar,
				},
				[]key.Binding{
					k.Chat.Copy,
					k.Chat.ClearHighlight,
				},
			)
			if m.pillsExpanded && hasIncompleteTodos(m.session.Todos) && m.promptQueue > 0 {
				binds = append(binds, []key.Binding{k.Chat.PillLeft})
			}
		}
	default:
		if m.session == nil {
			// no session selected
			binds = append(
				binds,
				[]key.Binding{
					k.ShiftTab,
					commands,
					k.Models,
					k.Sessions,
					k.ToggleYolo,
				},
			)
			editorBinds := []key.Binding{
				k.Editor.Newline,
				k.Editor.MentionFile,
				k.Editor.OpenEditor,
				k.Editor.PasteText,
				k.Editor.SelectAll,
				k.Editor.CopySelection,
				k.Editor.CutSelection,
			}
			if m.currentModelSupportsImages() {
				editorBinds = append(editorBinds, k.Editor.AddImage, k.Editor.PasteImage)
			}
			binds = append(binds, editorBinds)
			if hasAttachments {
				binds = append(
					binds,
					[]key.Binding{
						k.Editor.AttachmentDeleteMode,
						k.Editor.DeleteAllAttachments,
						k.Editor.Escape,
					},
				)
			}
		}
	}

	binds = append(
		binds,
		[]key.Binding{
			help,
			k.Quit,
		},
	)

	return binds
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
