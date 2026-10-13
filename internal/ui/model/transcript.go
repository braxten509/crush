package model

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/ui/chat"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// applyChatScroll scrolls the chat by lines and, if the selection is then
// outside the viewport, moves it to the nearest visible edge. The selection
// is moved rather than scrolled to so a large coalesced delta is applied in
// full instead of being rewound to the selected item.
func (m *UI) applyChatScroll(lines int) {
	m.chat.ScrollBy(lines)
	if m.chat.SelectedItemInView() {
		return
	}
	if lines > 0 && m.chat.AtBottom() {
		m.chat.SelectLast()
		return
	}
	m.chat.SelectNearestInView(lines < 0)
}

// handleAnimTick advances every visible spinner by one frame. A tick that
// changed nothing visible is scroll-only so the frame cache survives; one
// that did keeps the view pinned to the bottom while following, since
// animated items can change height.
func (m *UI) handleAnimTick(msg animTickMsg) tea.Cmd {
	if msg.chat != nil && msg.chat != m.chat {
		return m.handleSubagentAnimTick(msg)
	}
	if m.state != uiChat {
		m.chat.stopAnimating(msg)
		m.frame.markScrollOnly()
		return nil
	}
	changed, cmd := m.chat.Tick(msg)
	if !changed {
		m.frame.markScrollOnly()
		return cmd
	}
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
	}
	return cmd
}

// setSessionMessages sets the messages for the current session in the chat,
// preparing them on the spot. Session loads prepare them off the UI thread
// instead (see loadSession).
func (m *UI) setSessionMessages(msgs []message.Message) tea.Cmd {
	return m.setPreparedMessages(msgs, m.prepareTranscriptNow(msgs))
}

// setMessagePlanFlags marks assistant message items as plan-agent output
// while the UI is in plan mode, so their streaming content renders as the
// open plan card. Finished non-plan messages ignore the flag, so reloading
// an old session in plan mode never grows spurious cards.
func (m *UI) setMessagePlanFlags(items []chat.MessageItem) {
	if m.mode != uiInputModePlan {
		return
	}
	for _, item := range items {
		if a, ok := item.(*chat.AssistantMessageItem); ok {
			a.SetPlanAgent(true)
		}
	}
}

// appendSessionMessage appends a new message to the current session in the chat
// if the message is a tool result it will update the corresponding tool call message
func (m *UI) appendSessionMessage(msg message.Message) tea.Cmd {
	msg = m.applyImmediateCancel(msg)
	var cmds []tea.Cmd

	existing := m.chat.MessageItem(msg.ID)
	if existing != nil {
		// message already exists, skip
		return nil
	}

	switch msg.Role {
	case message.User:
		// Shell commands are rendered live via shellResultMsg; skip
		// the persisted duplicate.
		hasShellCmd := false
		for _, part := range msg.Parts {
			if _, ok := part.(message.ShellCommand); ok {
				hasShellCmd = true
				break
			}
		}
		if hasShellCmd {
			return nil
		}
		m.lastUserMessageTime = msg.CreatedAt
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil, m.com.Workspace.WorkingDir())
		if pending := m.confirmPendingPrompt(msg.Content().SubmissionID); pending != nil && m.chat.ReplaceMessage(pending.ID, items...) {
			return nil
		}
		m.chat.AppendMessages(items...)
		m.chat.ScrollToBottom()
	case message.System:
		m.chat.AppendMessages(chat.ExtractMessageItems(m.com.Styles, &msg, nil, m.com.Workspace.WorkingDir())...)
		if m.chat.Follow() {
			m.chat.ScrollToBottom()
		}
	case message.Assistant:
		items := chat.ExtractMessageItems(m.com.Styles, &msg, nil, m.com.Workspace.WorkingDir())
		if _, cancelled := m.canceledMessages[msg.ID]; !cancelled && msg.FinishReason() != message.FinishReasonCanceled {
			items = m.chat.AcceptResponse(&msg, items)
		}
		m.setMessagePlanFlags(items)
		m.chat.AppendMessages(items...)
		if m.chat.Follow() {
			m.chat.ScrollToBottom()
		}
		if chat.ShouldShowAssistantInfo(&msg) {
			infoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.AppendMessages(infoItem)
			if m.chat.Follow() {
				m.chat.ScrollToBottom()
			}
		}
	case message.Tool:
		for _, tr := range msg.ToolResults() {
			toolItem := m.chat.MessageItem(tr.ToolCallID)
			if toolItem == nil {
				// we should have an item!
				continue
			}
			if toolMsgItem, ok := toolItem.(chat.ToolMessageItem); ok {
				toolMsgItem.SetResult(&tr)
				if m.chat.Follow() {
					m.chat.ScrollToBottom()
				}
			}
		}
	}
	return tea.Sequence(cmds...)
}

// updateSessionMessage updates an existing message in the current session in
// the chat when an assistant message is updated it may include updated tool
// calls as well that is why we need to handle creating/updating each tool call
// message too.
func (m *UI) updateSessionMessage(msg message.Message) tea.Cmd {
	msg = m.applyImmediateCancel(msg)
	// A message update means work is active; the animation clock may have
	// been frozen by a non-busy session reload (ghost-spinner guard).
	m.chat.SetAnimationsAllowed(true)
	var cmds []tea.Cmd
	existingItem := m.chat.MessageItem(msg.ID)

	if existingItem != nil {
		if assistantItem, ok := existingItem.(*chat.AssistantMessageItem); ok {
			assistantItem.SetMessage(&msg)
			assistantItem.SetPlanAgent(m.mode == uiInputModePlan)
			m.chat.Refold(assistantItem)
		}
	}

	shouldRenderAssistant := chat.ShouldRenderAssistantMessage(&msg)
	_, interrupted := m.canceledMessages[msg.ID]
	hiddenInterruptedStep := interrupted && existingItem == nil && msg.Content().Text == "" && len(msg.ToolCalls()) > 0
	if hiddenInterruptedStep {
		shouldRenderAssistant = false
	}
	// Remove empty status/reasoning rows after a step finishes or hands off
	// to tools. The separate model/provider/duration footer stays visible.
	if !shouldRenderAssistant && existingItem != nil {
		m.chat.RemoveMessage(msg.ID)
	} else if shouldRenderAssistant && existingItem == nil && msg.Role == message.Assistant {
		// A previously hidden reasoning/tool-only step can acquire reply text
		// later in the stream. Restore that visible reply without its thoughts.
		item := chat.NewAssistantMessageItem(m.com.Styles, &msg).(*chat.AssistantMessageItem)
		item.SetPlanAgent(m.mode == uiInputModePlan)
		m.chat.InsertRelatedMessages(msg.ID, true, item)
	}

	// The info item shows for every turn with a Prism-routed model, and
	// for the final turn of the prompt. It is removed again when the
	// turn no longer qualifies (e.g. a retry reset the stream).
	if infoItem := m.chat.MessageItem(chat.AssistantInfoID(msg.ID)); chat.ShouldShowAssistantInfo(&msg) {
		if infoItem == nil && !hiddenInterruptedStep {
			newInfoItem := chat.NewAssistantInfoItem(m.com.Styles, &msg, m.com.Config(), time.Unix(m.lastUserMessageTime, 0))
			m.chat.InsertRelatedMessages(msg.ID, false, newInfoItem)
		}
	} else if infoItem != nil {
		m.chat.RemoveMessage(chat.AssistantInfoID(msg.ID))
	}

	var items []chat.MessageItem
	for _, tc := range msg.ToolCalls() {
		existingToolItem := m.chat.MessageItem(tc.ID)
		if toolItem, ok := existingToolItem.(chat.ToolMessageItem); ok {
			existingToolCall := toolItem.ToolCall()
			// only update if finished state changed or input changed
			// to avoid clearing the cache
			if (tc.Finished && !existingToolCall.Finished) || tc.Input != existingToolCall.Input {
				toolItem.SetToolCall(tc)
			}
		}
		if existingToolItem == nil {
			items = append(items, chat.NewToolMessageItem(m.com.Styles, msg.ID, tc, nil, msg.FinishReason() == message.FinishReasonCanceled, m.com.Workspace.WorkingDir()))
		}
	}

	m.chat.InsertRelatedMessages(msg.ID, false, items...)
	if m.chat.Follow() {
		m.chat.ScrollToBottom()
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

// handleChildSessionMessage handles messages from child sessions (agent tools).
func (m *UI) handleChildSessionMessage(event pubsub.Event[message.Message]) tea.Cmd {
	var cmds []tea.Cmd

	// Only process messages with tool calls or results.
	if len(event.Payload.ToolCalls()) == 0 && len(event.Payload.ToolResults()) == 0 {
		return nil
	}

	// Check if this is an agent tool session and parse it.
	childSessionID := event.Payload.SessionID
	_, toolCallID, ok := m.com.Workspace.ParseAgentToolSessionID(childSessionID)
	if !ok {
		return nil
	}
	// Nested tool activity means the agent is running; the animation clock
	// may have been frozen by a non-busy session reload.
	m.chat.SetAnimationsAllowed(true)

	// Find the parent agent tool item.
	var agentItem chat.NestedToolContainer
	for i := 0; i < m.chat.Len(); i++ {
		item := m.chat.MessageItem(toolCallID)
		if item == nil {
			continue
		}
		if agent, ok := item.(chat.NestedToolContainer); ok {
			if toolMessageItem, ok := item.(chat.ToolMessageItem); ok {
				if toolMessageItem.ToolCall().ID == toolCallID {
					// Verify this agent belongs to the correct parent message.
					// We can't directly check parentMessageID on the item, so we trust the session parsing.
					agentItem = agent
					break
				}
			}
		}
	}

	if agentItem == nil {
		return nil
	}

	// Get existing nested tools.
	nestedTools := agentItem.NestedTools()

	// Update or create nested tool calls.
	for _, tc := range event.Payload.ToolCalls() {
		found := false
		for _, existingTool := range nestedTools {
			if existingTool.ToolCall().ID == tc.ID {
				existingTool.SetToolCall(tc)
				found = true
				break
			}
		}
		if !found {
			// Create a new nested tool item.
			nestedItem := chat.NewToolMessageItem(m.com.Styles, event.Payload.ID, tc, nil, false, m.com.Workspace.WorkingDir())
			if simplifiable, ok := nestedItem.(chat.Compactable); ok {
				simplifiable.SetCompact(true)
			}
			nestedTools = append(nestedTools, nestedItem)
		}
	}

	// Update nested tool results.
	for _, tr := range event.Payload.ToolResults() {
		for _, nestedTool := range nestedTools {
			if nestedTool.ToolCall().ID == tr.ToolCallID {
				nestedTool.SetResult(&tr)
				break
			}
		}
	}

	// Update the agent item with the new nested tools.
	agentItem.SetNestedTools(nestedTools)

	// Update the chat so it updates the index map for animations to work as expected
	m.chat.UpdateNestedToolIDs(toolCallID)

	if m.chat.Follow() {
		m.chat.ScrollToBottom()
		m.chat.SelectLast()
	}

	return tea.Sequence(cmds...)
}

func (m *UI) copyChatHighlight() tea.Cmd {
	text := m.chat.HighlightContent()
	return common.CopyToClipboardWithCallback(
		text,
		"Selected text copied to clipboard",
		util.CmdHandler(chatHighlightCopiedMsg{}),
	)
}
