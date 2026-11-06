package tools

// The bash tool is not in this package. It lives in package shelltool
// (internal/agent/tools/shelltool), which imports this one, so nothing here
// can refer back to it:
//
//   - shelltool/bash.go: NewBashTool, NewReadOnlyBashTool, the dangerous
//     command list (defaultBlockedCommands, resolveBlockedCommands,
//     blockFuncs) and output formatting.
//   - shelltool/safe.go: the commands that run without a prompt
//     (isSafeReadOnly, userSafeCommands).
//   - shell_types.go (this package): BashParams, BashPermissionsParams,
//     BashResponseMetadata, BashToolName and the output limits.
//
// Upstream Crush still keeps the tool in this file, so upstream changes to
// it arrive here when merging and need porting to shelltool/bash.go.
