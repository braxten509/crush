package chat

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/diff"
	"github.com/charmbracelet/crush/internal/fsext"
)

// formatToolForCopy formats the tool call for clipboard copying.
func (t *baseToolMessageItem) formatToolForCopy() string {
	var parts []string

	toolName := prettifyToolName(t.toolCall.Name)
	parts = append(parts, fmt.Sprintf("## %s Tool Call", toolName))

	if t.toolCall.Input != "" {
		params := t.formatParametersForCopy()
		if params != "" {
			parts = append(parts, "### Parameters:")
			parts = append(parts, params)
		}
	}

	if t.result != nil && t.result.ToolCallID != "" {
		if t.result.IsError {
			parts = append(parts, "### Error:")
			parts = append(parts, t.result.Content)
		} else {
			parts = append(parts, "### Result:")
			content := t.formatResultForCopy()
			if content != "" {
				parts = append(parts, content)
			}
		}
	} else if t.status == ToolStatusCanceled {
		parts = append(parts, "### Status:")
		parts = append(parts, "Cancelled")
	} else {
		parts = append(parts, "### Status:")
		parts = append(parts, "Pending...")
	}

	return strings.Join(parts, "\n\n")
}

// formatParametersForCopy formats tool parameters for clipboard copying.
func (t *baseToolMessageItem) formatParametersForCopy() string {
	if spec, ok := toolSpecs[t.toolCall.Name]; ok && spec.copyParams != nil {
		if params, ok := spec.copyParams(t.toolCall.Input); ok {
			return params
		}
	}

	var params map[string]any
	if json.Unmarshal([]byte(t.toolCall.Input), &params) == nil {
		var parts []string
		for key, value := range params {
			displayKey := strings.ReplaceAll(key, "_", " ")
			if len(displayKey) > 0 {
				displayKey = strings.ToUpper(displayKey[:1]) + displayKey[1:]
			}
			parts = append(parts, fmt.Sprintf("**%s:** %v", displayKey, value))
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

// copyBashParams formats Bash parameters for the clipboard.
func copyBashParams(params tools.BashParams) string {
	cmd := strings.ReplaceAll(params.Command, "\n", " ")
	cmd = strings.ReplaceAll(cmd, "\t", "    ")
	return fmt.Sprintf("**Command:** %s", cmd)
}

// copyViewParams formats View parameters for the clipboard.
func copyViewParams(params tools.ViewParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath)))
	if params.Limit > 0 {
		parts = append(parts, fmt.Sprintf("**Limit:** %d", params.Limit))
	}
	if params.Offset > 0 {
		parts = append(parts, fmt.Sprintf("**Offset:** %d", params.Offset))
	}
	return strings.Join(parts, "\n")
}

// copyEditParams formats Edit parameters for the clipboard.
func copyEditParams(params tools.EditParams) string {
	return fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath))
}

// copyMultiEditParams formats MultiEdit parameters for the clipboard.
func copyMultiEditParams(params tools.MultiEditParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath)))
	parts = append(parts, fmt.Sprintf("**Edits:** %d", len(params.Edits)))
	return strings.Join(parts, "\n")
}

// copyWriteParams formats Write parameters for the clipboard.
func copyWriteParams(params tools.WriteParams) string {
	return fmt.Sprintf("**File:** %s", fsext.PrettyPath(params.FilePath))
}

// copyFetchParams formats Fetch parameters for the clipboard.
func copyFetchParams(params tools.FetchParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
	if params.Format != "" {
		parts = append(parts, fmt.Sprintf("**Format:** %s", params.Format))
	}
	if params.Timeout > 0 {
		parts = append(parts, fmt.Sprintf("**Timeout:** %ds", params.Timeout))
	}
	return strings.Join(parts, "\n")
}

// copyAgenticFetchParams formats AgenticFetch parameters for the clipboard.
func copyAgenticFetchParams(params tools.AgenticFetchParams) string {
	var parts []string
	if params.URL != "" {
		parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
	}
	if params.Prompt != "" {
		parts = append(parts, fmt.Sprintf("**Prompt:** %s", params.Prompt))
	}
	return strings.Join(parts, "\n")
}

// copyWebFetchParams formats WebFetch parameters for the clipboard.
func copyWebFetchParams(params tools.WebFetchParams) string {
	return fmt.Sprintf("**URL:** %s", params.URL)
}

// copyGrepParams formats Grep parameters for the clipboard.
func copyGrepParams(params tools.GrepParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**Pattern:** %s", params.Pattern))
	if params.Path != "" {
		parts = append(parts, fmt.Sprintf("**Path:** %s", params.Path))
	}
	if params.Include != "" {
		parts = append(parts, fmt.Sprintf("**Include:** %s", params.Include))
	}
	if params.LiteralText {
		parts = append(parts, "**Literal:** true")
	}
	return strings.Join(parts, "\n")
}

// copyGlobParams formats Glob parameters for the clipboard.
func copyGlobParams(params tools.GlobParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**Pattern:** %s", params.Pattern))
	if params.Path != "" {
		parts = append(parts, fmt.Sprintf("**Path:** %s", params.Path))
	}
	return strings.Join(parts, "\n")
}

// copyLSParams formats LS parameters for the clipboard.
func copyLSParams(params tools.LSParams) string {
	path := params.Path
	if path == "" {
		path = "."
	}
	return fmt.Sprintf("**Path:** %s", fsext.PrettyPath(path))
}

// copyDownloadParams formats Download parameters for the clipboard.
func copyDownloadParams(params tools.DownloadParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**URL:** %s", params.URL))
	parts = append(parts, fmt.Sprintf("**File Path:** %s", fsext.PrettyPath(params.FilePath)))
	if params.Timeout > 0 {
		parts = append(parts, fmt.Sprintf("**Timeout:** %s", (time.Duration(params.Timeout)*time.Second).String()))
	}
	return strings.Join(parts, "\n")
}

// copySourcegraphParams formats Sourcegraph parameters for the clipboard.
func copySourcegraphParams(params tools.SourcegraphParams) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("**Query:** %s", params.Query))
	if params.Count > 0 {
		parts = append(parts, fmt.Sprintf("**Count:** %d", params.Count))
	}
	if params.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("**Context:** %d", params.ContextWindow))
	}
	return strings.Join(parts, "\n")
}

// copyAgentParams formats Agent parameters for the clipboard.
func copyAgentParams(params agent.AgentParams) string {
	return fmt.Sprintf("**Task:**\n%s", params.Prompt)
}

// copyDiagnosticsParams formats Diagnostics parameters for the clipboard.
// The input is not shown.
func copyDiagnosticsParams(string) (string, bool) {
	return "**Project:** diagnostics", true
}

// formatResultForCopy formats tool results for clipboard copying.
func (t *baseToolMessageItem) formatResultForCopy() string {
	if t.result == nil {
		return ""
	}

	if t.result.Data != "" {
		if strings.HasPrefix(t.result.MIMEType, "image/") {
			return fmt.Sprintf("[Image: %s]", t.result.MIMEType)
		}
		return fmt.Sprintf("[Media: %s]", t.result.MIMEType)
	}

	if spec, ok := toolSpecs[t.toolCall.Name]; ok && spec.copyResult != nil {
		return spec.copyResult(t)
	}
	return t.result.Content
}

// copyResultCodeBlock copies the result content as a code block.
func copyResultCodeBlock(t *baseToolMessageItem) string {
	return fmt.Sprintf("```\n%s\n```", t.result.Content)
}

// formatBashResultForCopy formats bash tool results for clipboard.
func (t *baseToolMessageItem) formatBashResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var meta tools.BashResponseMetadata
	if t.result.Metadata != "" {
		json.Unmarshal([]byte(t.result.Metadata), &meta)
	}

	output := meta.Output
	if output == "" && t.result.Content != tools.BashNoOutput {
		output = t.result.Content
	}

	if output == "" {
		return ""
	}

	return fmt.Sprintf("```bash\n%s\n```", output)
}

// formatViewResultForCopy formats view tool results for clipboard.
func (t *baseToolMessageItem) formatViewResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var meta tools.ViewResponseMetadata
	if t.result.Metadata != "" {
		json.Unmarshal([]byte(t.result.Metadata), &meta)
	}

	if meta.Content == "" {
		return t.result.Content
	}

	lang := ""
	if meta.FilePath != "" {
		ext := strings.ToLower(filepath.Ext(meta.FilePath))
		switch ext {
		case ".go":
			lang = "go"
		case ".js", ".mjs":
			lang = "javascript"
		case ".ts":
			lang = "typescript"
		case ".py":
			lang = "python"
		case ".rs":
			lang = "rust"
		case ".java":
			lang = "java"
		case ".c":
			lang = "c"
		case ".cpp", ".cc", ".cxx":
			lang = "cpp"
		case ".sh", ".bash":
			lang = "bash"
		case ".json":
			lang = "json"
		case ".yaml", ".yml":
			lang = "yaml"
		case ".xml":
			lang = "xml"
		case ".html":
			lang = "html"
		case ".css":
			lang = "css"
		case ".md":
			lang = "markdown"
		}
	}

	var result strings.Builder
	if lang != "" {
		fmt.Fprintf(&result, "```%s\n", lang)
	} else {
		result.WriteString("```\n")
	}
	result.WriteString(meta.Content)
	result.WriteString("\n```")

	return result.String()
}

// formatEditResultForCopy formats edit tool results for clipboard.
func (t *baseToolMessageItem) formatEditResultForCopy() string {
	if t.result == nil || t.result.Metadata == "" {
		if t.result != nil {
			return t.result.Content
		}
		return ""
	}

	var meta tools.EditResponseMetadata
	if json.Unmarshal([]byte(t.result.Metadata), &meta) != nil {
		return t.result.Content
	}

	var params tools.EditParams
	json.Unmarshal([]byte(t.toolCall.Input), &params)

	var result strings.Builder

	if meta.OldContent != "" || meta.NewContent != "" {
		fileName := params.FilePath
		if fileName != "" {
			fileName = fsext.PrettyPath(fileName)
		}
		diffContent, additions, removals := diff.GenerateDiff(meta.OldContent, meta.NewContent, fileName)

		fmt.Fprintf(&result, "Changes: +%d -%d\n", additions, removals)
		result.WriteString("```diff\n")
		result.WriteString(diffContent)
		result.WriteString("\n```")
	}

	return result.String()
}

// formatMultiEditResultForCopy formats multi-edit tool results for clipboard.
func (t *baseToolMessageItem) formatMultiEditResultForCopy() string {
	if t.result == nil || t.result.Metadata == "" {
		if t.result != nil {
			return t.result.Content
		}
		return ""
	}

	var meta tools.MultiEditResponseMetadata
	if json.Unmarshal([]byte(t.result.Metadata), &meta) != nil {
		return t.result.Content
	}

	var params tools.MultiEditParams
	json.Unmarshal([]byte(t.toolCall.Input), &params)

	var result strings.Builder
	if meta.OldContent != "" || meta.NewContent != "" {
		fileName := params.FilePath
		if fileName != "" {
			fileName = fsext.PrettyPath(fileName)
		}
		diffContent, additions, removals := diff.GenerateDiff(meta.OldContent, meta.NewContent, fileName)

		fmt.Fprintf(&result, "Changes: +%d -%d\n", additions, removals)
		result.WriteString("```diff\n")
		result.WriteString(diffContent)
		result.WriteString("\n```")
	}

	return result.String()
}

// formatWriteResultForCopy formats write tool results for clipboard.
func (t *baseToolMessageItem) formatWriteResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.WriteParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	lang := ""
	if params.FilePath != "" {
		ext := strings.ToLower(filepath.Ext(params.FilePath))
		switch ext {
		case ".go":
			lang = "go"
		case ".js", ".mjs":
			lang = "javascript"
		case ".ts":
			lang = "typescript"
		case ".py":
			lang = "python"
		case ".rs":
			lang = "rust"
		case ".java":
			lang = "java"
		case ".c":
			lang = "c"
		case ".cpp", ".cc", ".cxx":
			lang = "cpp"
		case ".sh", ".bash":
			lang = "bash"
		case ".json":
			lang = "json"
		case ".yaml", ".yml":
			lang = "yaml"
		case ".xml":
			lang = "xml"
		case ".html":
			lang = "html"
		case ".css":
			lang = "css"
		case ".md":
			lang = "markdown"
		}
	}

	var result strings.Builder
	fmt.Fprintf(&result, "File: %s\n", fsext.PrettyPath(params.FilePath))
	if lang != "" {
		fmt.Fprintf(&result, "```%s\n", lang)
	} else {
		result.WriteString("```\n")
	}
	result.WriteString(params.Content)
	result.WriteString("\n```")

	return result.String()
}

// formatFetchResultForCopy formats fetch tool results for clipboard.
func (t *baseToolMessageItem) formatFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.FetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	if params.URL != "" {
		fmt.Fprintf(&result, "URL: %s\n", params.URL)
	}
	if params.Format != "" {
		fmt.Fprintf(&result, "Format: %s\n", params.Format)
	}
	if params.Timeout > 0 {
		fmt.Fprintf(&result, "Timeout: %ds\n", params.Timeout)
	}
	result.WriteString("\n")

	result.WriteString(t.result.Content)

	return result.String()
}

// formatAgenticFetchResultForCopy formats agentic fetch tool results for clipboard.
func (t *baseToolMessageItem) formatAgenticFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.AgenticFetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	if params.URL != "" {
		fmt.Fprintf(&result, "URL: %s\n", params.URL)
	}
	if params.Prompt != "" {
		fmt.Fprintf(&result, "Prompt: %s\n\n", params.Prompt)
	}

	result.WriteString("```markdown\n")
	result.WriteString(t.result.Content)
	result.WriteString("\n```")

	return result.String()
}

// formatWebFetchResultForCopy formats web fetch tool results for clipboard.
func (t *baseToolMessageItem) formatWebFetchResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var params tools.WebFetchParams
	if json.Unmarshal([]byte(t.toolCall.Input), &params) != nil {
		return t.result.Content
	}

	var result strings.Builder
	fmt.Fprintf(&result, "URL: %s\n\n", params.URL)
	result.WriteString("```markdown\n")
	result.WriteString(t.result.Content)
	result.WriteString("\n```")

	return result.String()
}

// formatAgentResultForCopy formats agent tool results for clipboard.
func (t *baseToolMessageItem) formatAgentResultForCopy() string {
	if t.result == nil {
		return ""
	}

	var result strings.Builder

	if t.result.Content != "" {
		fmt.Fprintf(&result, "```markdown\n%s\n```", t.result.Content)
	}

	return result.String()
}
