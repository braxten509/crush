package chat

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// toolItemConstructor builds the chat item for one call to a tool.
type toolItemConstructor func(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
	workingDir string,
) ToolMessageItem

// toolSpec describes how the chat shows and copies one built-in tool.
type toolSpec struct {
	// newItem builds the chat item for a call to the tool.
	newItem toolItemConstructor
	// copyName is the tool's name in the heading of the clipboard copy.
	// Empty means the humanized tool name.
	copyName string
	// copyParams formats the call's input for the clipboard. It reports
	// false when the input does not decode, and the copy then lists the
	// raw parameters instead, as it does when copyParams is nil.
	copyParams func(input string) (string, bool)
	// copyResult formats a successful text result for the clipboard. Nil
	// copies the result content as is.
	copyResult func(t *baseToolMessageItem) string
}

// toolSpecs holds the built-in tools the chat renders with a dedicated
// item. Tools missing here are MCP tools or fall back to the generic item.
// It is filled in init because some items build nested items through
// [NewToolMessageItem], which reads it.
var toolSpecs map[string]toolSpec

func init() {
	toolSpecs = map[string]toolSpec{
		tools.BashToolName: {
			newItem: func(sty *styles.Styles, toolCall message.ToolCall, result *message.ToolResult, canceled bool, workingDir string) ToolMessageItem {
				return NewBashToolMessageItem(sty, toolCall, result, canceled, workingDir)
			},
			copyName:   "Bash",
			copyParams: copyParamsAs(copyBashParams),
			copyResult: (*baseToolMessageItem).formatBashResultForCopy,
		},
		tools.JobOutputToolName: {
			newItem:  ignoreWorkingDir(NewJobOutputToolMessageItem),
			copyName: "Job: Output",
		},
		tools.JobKillToolName: {
			newItem:  ignoreWorkingDir(NewJobKillToolMessageItem),
			copyName: "Job: Kill",
		},
		tools.ViewToolName: {
			newItem:    ignoreWorkingDir(NewViewToolMessageItem),
			copyName:   "View",
			copyParams: copyParamsAs(copyViewParams),
			copyResult: (*baseToolMessageItem).formatViewResultForCopy,
		},
		tools.WriteToolName: {
			newItem:    ignoreWorkingDir(NewWriteToolMessageItem),
			copyName:   "Write",
			copyParams: copyParamsAs(copyWriteParams),
			copyResult: (*baseToolMessageItem).formatWriteResultForCopy,
		},
		tools.EditToolName: {
			newItem:    ignoreWorkingDir(NewEditToolMessageItem),
			copyName:   "Edit",
			copyParams: copyParamsAs(copyEditParams),
			copyResult: (*baseToolMessageItem).formatEditResultForCopy,
		},
		tools.MultiEditToolName: {
			newItem:    ignoreWorkingDir(NewMultiEditToolMessageItem),
			copyName:   "Multi-Edit",
			copyParams: copyParamsAs(copyMultiEditParams),
			copyResult: (*baseToolMessageItem).formatMultiEditResultForCopy,
		},
		tools.GlobToolName: {
			newItem:    ignoreWorkingDir(NewGlobToolMessageItem),
			copyName:   "Glob",
			copyParams: copyParamsAs(copyGlobParams),
			copyResult: copyResultCodeBlock,
		},
		tools.GrepToolName: {
			newItem:    ignoreWorkingDir(NewGrepToolMessageItem),
			copyName:   "Grep",
			copyParams: copyParamsAs(copyGrepParams),
			copyResult: copyResultCodeBlock,
		},
		tools.LSToolName: {
			newItem:    ignoreWorkingDir(NewLSToolMessageItem),
			copyName:   "List",
			copyParams: copyParamsAs(copyLSParams),
			copyResult: copyResultCodeBlock,
		},
		tools.DownloadToolName: {
			newItem:    ignoreWorkingDir(NewDownloadToolMessageItem),
			copyName:   "Download",
			copyParams: copyParamsAs(copyDownloadParams),
			copyResult: copyResultCodeBlock,
		},
		tools.FetchToolName: {
			newItem:    ignoreWorkingDir(NewFetchToolMessageItem),
			copyName:   "Fetch",
			copyParams: copyParamsAs(copyFetchParams),
			copyResult: (*baseToolMessageItem).formatFetchResultForCopy,
		},
		tools.SourcegraphToolName: {
			newItem:    ignoreWorkingDir(NewSourcegraphToolMessageItem),
			copyName:   "Sourcegraph",
			copyParams: copyParamsAs(copySourcegraphParams),
			copyResult: copyResultCodeBlock,
		},
		tools.DiagnosticsToolName: {
			newItem:    ignoreWorkingDir(NewDiagnosticsToolMessageItem),
			copyParams: copyDiagnosticsParams,
			copyResult: copyResultCodeBlock,
		},
		agent.AgentToolName: {
			newItem:    ignoreWorkingDir(NewAgentToolMessageItem),
			copyName:   "Agent",
			copyParams: copyParamsAs(copyAgentParams),
			copyResult: (*baseToolMessageItem).formatAgentResultForCopy,
		},
		tools.AgenticFetchToolName: {
			newItem:    ignoreWorkingDir(NewAgenticFetchToolMessageItem),
			copyName:   "Agentic Fetch",
			copyParams: copyParamsAs(copyAgenticFetchParams),
			copyResult: (*baseToolMessageItem).formatAgenticFetchResultForCopy,
		},
		tools.WebFetchToolName: {
			newItem:    ignoreWorkingDir(NewWebFetchToolMessageItem),
			copyName:   "Fetch",
			copyParams: copyParamsAs(copyWebFetchParams),
			copyResult: (*baseToolMessageItem).formatWebFetchResultForCopy,
		},
		tools.WebSearchToolName: {
			newItem:  ignoreWorkingDir(NewWebSearchToolMessageItem),
			copyName: "Search",
		},
		tools.TodosToolName: {
			newItem:    ignoreWorkingDir(NewTodosToolMessageItem),
			copyName:   "To-Do",
			copyResult: copyResultCodeBlock,
		},
		tools.QuestionToolName:      {newItem: ignoreWorkingDir(NewQuestionToolMessageItem)},
		tools.ReferencesToolName:    {newItem: ignoreWorkingDir(NewReferencesToolMessageItem)},
		tools.DefinitionToolName:    {newItem: ignoreWorkingDir(NewDefinitionToolMessageItem)},
		tools.RenameToolName:        {newItem: ignoreWorkingDir(NewRenameToolMessageItem)},
		tools.ReplaceSymbolToolName: {newItem: ignoreWorkingDir(NewReplaceSymbolToolMessageItem)},
		tools.CallHierarchyToolName: {newItem: ignoreWorkingDir(NewCallHierarchyToolMessageItem)},
		tools.SymbolsToolName:       {newItem: ignoreWorkingDir(NewSymbolsToolMessageItem)},
		tools.LSPRestartToolName:    {newItem: ignoreWorkingDir(NewLSPRestartToolMessageItem)},
	}
}

// ignoreWorkingDir adapts a tool item constructor that does not need the
// working directory to [toolItemConstructor].
func ignoreWorkingDir[T ToolMessageItem](
	fn func(*styles.Styles, message.ToolCall, *message.ToolResult, bool) T,
) toolItemConstructor {
	return func(sty *styles.Styles, toolCall message.ToolCall, result *message.ToolResult, canceled bool, _ string) ToolMessageItem {
		return fn(sty, toolCall, result, canceled)
	}
}

// copyParamsAs adapts a formatter of decoded parameters to
// [toolSpec.copyParams]: it decodes the input into P first and reports
// false when that fails.
func copyParamsAs[P any](format func(P) string) func(input string) (string, bool) {
	return func(input string) (string, bool) {
		var params P
		if json.Unmarshal([]byte(input), &params) != nil {
			return "", false
		}
		return format(params), true
	}
}

// NewToolMessageItem creates a new [ToolMessageItem] based on the tool call name.
//
// It returns a specific tool message item type if implemented, otherwise it
// returns a generic tool message item. The messageID is the ID of the assistant
// message containing this tool call.
func NewToolMessageItem(
	sty *styles.Styles,
	messageID string,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
	workingDir string,
) ToolMessageItem {
	var item ToolMessageItem
	if spec, ok := toolSpecs[toolCall.Name]; ok {
		item = spec.newItem(sty, toolCall, result, canceled, workingDir)
	} else if IsDockerMCPTool(toolCall.Name) {
		item = NewDockerMCPToolMessageItem(sty, toolCall, result, canceled)
	} else if strings.HasPrefix(toolCall.Name, "mcp_") {
		item = NewMCPToolMessageItem(sty, toolCall, result, canceled)
	} else {
		item = NewGenericToolMessageItem(sty, toolCall, result, canceled)
	}
	item.SetMessageID(messageID)
	return item
}

// prettifyToolName returns a human-readable name for tool names.
func prettifyToolName(name string) string {
	if spec, ok := toolSpecs[name]; ok && spec.copyName != "" {
		return spec.copyName
	}
	return humanizedToolName(name)
}
