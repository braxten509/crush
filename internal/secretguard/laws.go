package secretguard

// Owner-owned law files (owner, 2026-11-25): projects using a LAWS.bend file
// reserve LAWS.bend and its approval files (tools/laws/approve and
// tools/laws/approved) for the owner alone. Agents may read LAWS.bend, run
// tools/laws/check, and edit code, PROOF.bend, or the rule book in laws/,
// but must never edit the locked law files or run tools/laws/approve. Inline
// interpreter scripts (-c, -e, heredocs) naming locked law files are blocked
// as well because script writes are easy to obfuscate and dedicated read
// tools or cat/head/grep/sed -n already read LAWS.bend. Like the secrets
// guard, this stops accidental or prompted edits; it is not a sandbox.

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// LawsReason is what the agent sees when a tool call tries to edit locked
// law files or run tools/laws/approve.
const LawsReason = "Blocked: LAWS.bend, tools/laws/approve, and tools/laws/approved are owned by the user. Never edit them or run tools/laws/approve; fix the code, PROOF.bend, or the rule book in laws/ instead."

var (
	lawReadOnlyTools = []string{
		"read", "view", "read_file", "view_file", "view_file_outline",
		"view_code_item", "view_file_chunk", "grep", "grep_search", "glob",
		"find_by_name", "ls", "list_dir", "webfetch", "web_fetch",
		"websearch", "web_search", "search_web", "todowrite", "todos",
		"update_steps", "ask_question",
	}
	lawPatchTools = []string{"apply_patch", "patch"}
	lawPathKeys   = []string{
		"file_path", "filePath", "target_file", "path", "absolute_path",
		"notebook_path", "Path", "TargetFile", "AbsolutePath",
		"old_path", "new_path", "destination", "dest",
	}
	lawWorkDirKeys = []string{
		"working_dir", "workdir", "cwd", "Cwd", "WorkingDir", "workingDirectory",
	}
	lawShellTools = []string{
		"bash", "sh", "shell", "run_command", "run_terminal_command",
		"exec_command", "terminal",
	}
	lawMutatingCommands = []string{
		"rm", "unlink", "shred", "mv", "cp", "install", "ln", "rsync",
		"truncate", "tee", "touch", "chmod", "chown", "chattr", "patch",
		"apply_patch", "ed", "ex", "vi", "vim", "nvim", "nano", "sponge",
	}
	lawInterpreters = []string{
		"python", "python2", "python3", "node", "nodejs", "deno", "bun",
		"perl", "ruby", "php", "lua", "awk", "gawk", "mawk",
	}
	lawGitMutating = []string{
		"rm", "mv", "checkout", "restore", "reset", "apply", "am",
		"clean", "stash",
	}
	lawShellNames = []string{"sh", "bash", "zsh", "dash", "ksh", "fish"}
	// Wrapper flags that take a following argument token.
	wrapperValueFlags = map[string][]string{
		"env":         {"-u", "--unset", "-C", "--chdir", "-S", "--split-string"},
		"nice":        {"-n", "--adjustment"},
		"timeout":     {"-k", "--kill-after", "-s", "--signal"},
		"stdbuf":      {"-i", "-o", "-e"},
		"sudo":        {"-u", "-g", "-C", "-p", "-h"},
		"systemd-run": {"-p", "--property", "-E", "--setenv", "--working-directory", "-u", "--unit", "--slice", "-d", "--description"},
	}
	gitValueFlags = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env"}
)

// IsProtectedLawPathInDir reports whether raw (resolved against dir when
// relative) names LAWS.bend, tools/laws/approve, or tools/laws/approved.
func IsProtectedLawPathInDir(raw, dir string) bool {
	return isProtectedLawPathInDir(raw, dir)
}

// BlocksLaws reports whether a tool call with this name and JSON input would
// modify, replace, or delete LAWS.bend, tools/laws/approve, or
// tools/laws/approved, or run tools/laws/approve.
func BlocksLaws(tool string, input []byte) bool {
	return BlocksLawsInDir(tool, input, "")
}

// BlocksLawsInDir is like [BlocksLaws], resolving relative paths against dir
// (or a working-directory field inside input).
func BlocksLawsInDir(tool string, input []byte, dir string) bool {
	lowerTool := strings.ToLower(strings.TrimSpace(tool))
	if slices.Contains(lawReadOnlyTools, lowerTool) {
		return false
	}

	var value any
	if json.Unmarshal(input, &value) != nil {
		return BlocksLawsCommandInDir(string(input), dir)
	}

	if fields, ok := value.(map[string]any); ok {
		dir = resolveLawDir(dir, workDirFromFields(fields))
	}

	if slices.Contains(lawPatchTools, lowerTool) {
		for _, text := range stringsIn(value, nil) {
			if patchTargetsProtectedLawInDir(text, dir) {
				return true
			}
		}
		return fieldsTargetProtectedLawInDir(value, dir)
	}

	if writesOnly(lowerTool) || lowerTool == "delete_file" || lowerTool == "create_file" {
		return fieldsTargetProtectedLawInDir(value, dir)
	}

	if fields, ok := value.(map[string]any); ok {
		if cmd, ok := commandFromFields(fields); ok {
			return BlocksLawsCommandInDir(cmd, dir)
		}
		if words, ok := commandWordsFromFields(fields); ok {
			return BlocksLawsArgsInDir(words, dir)
		}
		if slices.Contains(lawShellTools, lowerTool) {
			return false
		}
		if fieldsTargetProtectedLawInDir(fields, dir) {
			return true
		}
	}

	if slices.Contains(lawShellTools, lowerTool) {
		return BlocksLawsCommandInDir(strings.Join(stringsIn(value, nil), "\n"), dir)
	}
	for _, text := range stringsIn(value, nil) {
		if patchTargetsProtectedLawInDir(text, dir) {
			return true
		}
	}
	return false
}

// BlocksLawsCommand reports whether a shell command would modify, replace, or
// delete LAWS.bend, tools/laws/approve, or tools/laws/approved, or run
// tools/laws/approve.
func BlocksLawsCommand(command string) bool {
	return BlocksLawsCommandInDir(command, "")
}

// BlocksLawsCommandInDir is like [BlocksLawsCommand], resolving relative paths
// against dir.
func BlocksLawsCommandInDir(command, dir string) bool {
	if strings.TrimSpace(command) == "" {
		return false
	}
	inToolsLaws := dirInToolsLaws(dir) || strings.Contains(strings.ToLower(command), "tools/laws")
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return mentionsProtectedLaw(command, inToolsLaws)
	}
	blocked := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if blocked {
			return false
		}
		switch n := node.(type) {
		case *syntax.Redirect:
			if redirectWritesProtectedLaw(n, dir, inToolsLaws) {
				blocked = true
			}
		case *syntax.Stmt:
			call, ok := n.Cmd.(*syntax.CallExpr)
			if !ok {
				return true
			}
			args := make([]string, len(call.Args))
			for i, w := range call.Args {
				args[i] = wordText(w)
			}
			if argsBlockLaws(args, n.Redirs, dir, inToolsLaws) {
				blocked = true
			}
		}
		return !blocked
	})
	return blocked
}

// BlocksLawsArgs reports whether a command given as an argument slice would
// modify locked law files or run tools/laws/approve.
func BlocksLawsArgs(args []string) bool {
	return BlocksLawsArgsInDir(args, "")
}

// BlocksLawsArgsInDir is like [BlocksLawsArgs], resolving relative paths
// against dir.
func BlocksLawsArgsInDir(args []string, dir string) bool {
	if len(args) == 0 {
		return false
	}
	inToolsLaws := dirInToolsLaws(dir)
	for _, arg := range args {
		if strings.Contains(strings.ToLower(arg), "tools/laws") {
			inToolsLaws = true
			break
		}
	}
	return argsBlockLaws(args, nil, dir, inToolsLaws)
}

func argsBlockLaws(args []string, redirs []*syntax.Redirect, dir string, inToolsLaws bool) bool {
	args, dir = stripLawWrappers(args, dir)
	if len(args) == 0 {
		return false
	}
	inToolsLaws = inToolsLaws || dirInToolsLaws(dir)

	if isApproveScriptInDir(args[0], dir, inToolsLaws) {
		return true
	}

	name := filepath.Base(args[0])
	switch {
	case name == "source" || name == ".":
		return len(args) > 1 && isApproveScriptInDir(args[1], dir, inToolsLaws)
	case name == "crush" && len(args) >= 3 && args[1] == "bg":
		return crushBgBlocksLaws(args[2:], dir, inToolsLaws)
	case slices.Contains(lawShellNames, name):
		return shellCallBlocksLaws(args[1:], redirs, dir, inToolsLaws)
	case slices.Contains(lawInterpreters, name):
		return interpreterBlocksLaws(name, args[1:], redirs, dir, inToolsLaws)
	case name == "sed":
		return sedBlocksLaws(args[1:], dir, inToolsLaws)
	case name == "dd":
		return ddBlocksLaws(args[1:], dir, inToolsLaws)
	case name == "curl":
		return outputFlagBlocksLaws(args[1:], []string{"-o", "--output"}, dir, inToolsLaws)
	case name == "wget":
		return outputFlagBlocksLaws(args[1:], []string{"-O", "--output-document"}, dir, inToolsLaws)
	case name == "git":
		return gitBlocksLaws(args[1:], redirs, dir, inToolsLaws)
	case name == "xargs":
		return xargsBlocksLaws(args[1:], dir, inToolsLaws)
	case name == "find":
		return findBlocksLaws(args[1:], dir, inToolsLaws)
	case name == "tar" || name == "unzip":
		return archiveBlocksLaws(name, args[1:], dir, inToolsLaws)
	case slices.Contains(lawMutatingCommands, name):
		if name == "patch" || name == "apply_patch" {
			if heredocMatches(redirs, func(body string) bool { return patchTargetsProtectedLawInDir(body, dir) }) {
				return true
			}
		}
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if matchesProtectedLawTargetInDir(arg, dir, inToolsLaws) {
				return true
			}
		}
	}
	return false
}

func stripLawWrappers(args []string, dir string) ([]string, string) {
	for len(args) > 0 {
		name := filepath.Base(args[0])
		if name != "sudo" && !slices.Contains(agentShellWrappers, name) {
			break
		}
		valueFlags := wrapperValueFlags[name]
		args = args[1:]
		for len(args) > 0 {
			arg := args[0]
			if arg == "--" {
				args = args[1:]
				break
			}
			if val, ok := wrapperDirFlag(name, arg); ok {
				dir = resolveLawDir(dir, val)
				args = args[1:]
				continue
			}
			if slices.Contains(valueFlags, arg) {
				args = args[1:]
				if len(args) > 0 {
					if (name == "env" && (arg == "-C" || arg == "--chdir")) ||
						(name == "systemd-run" && arg == "--working-directory") {
						dir = resolveLawDir(dir, args[0])
					}
					args = args[1:]
				}
				continue
			}
			if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") || (name == "timeout" && isDuration(arg)) {
				args = args[1:]
				continue
			}
			break
		}
	}
	return args, dir
}

func wrapperDirFlag(name, arg string) (string, bool) {
	if name == "env" {
		if val, ok := strings.CutPrefix(arg, "--chdir="); ok {
			return val, true
		}
		if len(arg) > 2 && strings.HasPrefix(arg, "-C") {
			return arg[2:], true
		}
	}
	if name == "systemd-run" {
		if val, ok := strings.CutPrefix(arg, "--working-directory="); ok {
			return val, true
		}
	}
	return "", false
}

func crushBgBlocksLaws(args []string, dir string, inToolsLaws bool) bool {
	bgDir := dir
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest := args[i+1:]
			if len(rest) == 1 {
				return BlocksLawsCommandInDir(rest[0], bgDir)
			}
			return argsBlockLaws(rest, nil, bgDir, inToolsLaws || dirInToolsLaws(bgDir))
		}
		if val, ok := strings.CutPrefix(arg, "--cwd="); ok {
			bgDir = resolveLawDir(bgDir, val)
			continue
		}
		if arg == "-c" || arg == "--cwd" {
			if i+1 < len(args) {
				bgDir = resolveLawDir(bgDir, args[i+1])
				i++
			}
			continue
		}
		if arg == "--name" || arg == "-D" || arg == "--data-dir" || arg == "-H" || arg == "--host" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		rest := args[i:]
		if len(rest) == 1 {
			return BlocksLawsCommandInDir(rest[0], bgDir)
		}
		return argsBlockLaws(rest, nil, bgDir, inToolsLaws || dirInToolsLaws(bgDir))
	}
	return false
}

func shellCallBlocksLaws(args []string, redirs []*syntax.Redirect, dir string, inToolsLaws bool) bool {
	if heredocMatches(redirs, func(body string) bool { return BlocksLawsCommandInDir(body, dir) }) {
		return true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-c" || arg == "--command" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'c')) {
			if i+1 < len(args) {
				return BlocksLawsCommandInDir(args[i+1], dir)
			}
			return false
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return isApproveScriptInDir(arg, dir, inToolsLaws)
	}
	return false
}

func interpreterBlocksLaws(name string, args []string, redirs []*syntax.Redirect, dir string, inToolsLaws bool) bool {
	if heredocMatches(redirs, func(body string) bool { return mentionsProtectedLaw(body, inToolsLaws) }) {
		return true
	}
	inline := false
	inPlace := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-c" || arg == "-e" || arg == "-E" || arg == "--eval" || arg == "-p" {
			inline = true
			if i+1 < len(args) && mentionsProtectedLaw(args[i+1], inToolsLaws) {
				return true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && (name == "perl" || name == "ruby") && strings.ContainsRune(arg, 'i') {
			inPlace = true
		}
		if arg == "-i" && (name == "awk" || name == "gawk") {
			inPlace = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isApproveScriptInDir(arg, dir, inToolsLaws) {
			return true
		}
		if (inline || inPlace) && matchesProtectedLawTargetInDir(arg, dir, inToolsLaws) {
			return true
		}
		if (name == "awk" || name == "gawk" || name == "mawk") && !inline {
			inline = true
			if mentionsProtectedLaw(arg, inToolsLaws) {
				return true
			}
		}
	}
	return false
}

func sedBlocksLaws(args []string, dir string, inToolsLaws bool) bool {
	inPlace := false
	for _, arg := range args {
		if arg == "--in-place" || strings.HasPrefix(arg, "--in-place=") ||
			(strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'i')) {
			inPlace = true
			break
		}
	}
	if !inPlace {
		return false
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if matchesProtectedLawTargetInDir(arg, dir, inToolsLaws) {
			return true
		}
	}
	return false
}

func ddBlocksLaws(args []string, dir string, inToolsLaws bool) bool {
	for _, arg := range args {
		if out, ok := strings.CutPrefix(arg, "of="); ok && matchesProtectedLawTargetInDir(out, dir, inToolsLaws) {
			return true
		}
	}
	return false
}

func outputFlagBlocksLaws(args, flags []string, dir string, inToolsLaws bool) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		for _, flag := range flags {
			if arg == flag && i+1 < len(args) && matchesProtectedLawTargetInDir(args[i+1], dir, inToolsLaws) {
				return true
			}
			if strings.HasPrefix(flag, "--") {
				if val, ok := strings.CutPrefix(arg, flag+"="); ok && matchesProtectedLawTargetInDir(val, dir, inToolsLaws) {
					return true
				}
			} else if len(arg) > len(flag) && strings.HasPrefix(arg, flag) {
				if matchesProtectedLawTargetInDir(arg[len(flag):], dir, inToolsLaws) {
					return true
				}
			}
		}
	}
	return false
}

func gitBlocksLaws(args []string, redirs []*syntax.Redirect, dir string, inToolsLaws bool) bool {
	gitDir := dir
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "-C" && i+1 < len(args) {
			gitDir = resolveLawDir(gitDir, args[i+1])
			i += 2
			continue
		}
		if val, ok := strings.CutPrefix(arg, "-C"); ok && val != "" {
			gitDir = resolveLawDir(gitDir, val)
			i++
			continue
		}
		if slices.Contains(gitValueFlags, arg) {
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "-") {
			i++
			continue
		}
		break
	}
	if i >= len(args) {
		return false
	}
	inToolsLaws = inToolsLaws || dirInToolsLaws(gitDir)
	sub := args[i]
	rest := args[i+1:]
	if !slices.Contains(lawGitMutating, sub) {
		return false
	}
	if (sub == "apply" || sub == "am") && heredocMatches(redirs, func(body string) bool { return patchTargetsProtectedLawInDir(body, gitDir) }) {
		return true
	}
	for _, arg := range rest {
		if arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		if matchesProtectedLawTargetInDir(arg, gitDir, inToolsLaws) {
			return true
		}
	}
	return false
}

func xargsBlocksLaws(args []string, dir string, inToolsLaws bool) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-I" || arg == "-J" || arg == "-L" || arg == "-n" || arg == "-P" || arg == "-d" || arg == "-E" || arg == "-s" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return argsBlockLaws(args[i:], nil, dir, inToolsLaws)
	}
	return false
}

func findBlocksLaws(args []string, dir string, inToolsLaws bool) bool {
	for i, arg := range args {
		if arg == "-delete" {
			for _, part := range args[:i] {
				if matchesProtectedLawTargetInDir(part, dir, inToolsLaws) {
					return true
				}
			}
		}
		if (arg == "-exec" || arg == "-execdir") && i+1 < len(args) {
			if argsBlockLaws(args[i+1:], nil, dir, inToolsLaws) {
				return true
			}
		}
	}
	return false
}

func archiveBlocksLaws(name string, args []string, dir string, inToolsLaws bool) bool {
	extracting := name == "unzip"
	if name == "tar" {
		for _, arg := range args {
			if arg == "--extract" || arg == "--get" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg, 'x')) {
				extracting = true
				break
			}
		}
	}
	if !extracting {
		return false
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if matchesProtectedLawTargetInDir(arg, dir, inToolsLaws) {
			return true
		}
	}
	return false
}

func redirectWritesProtectedLaw(r *syntax.Redirect, dir string, inToolsLaws bool) bool {
	if r == nil || r.Word == nil {
		return false
	}
	switch r.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.RdrAll, syntax.AppAll, syntax.RdrInOut:
		return matchesProtectedLawTargetInDir(wordText(r.Word), dir, inToolsLaws)
	}
	return false
}

func heredocMatches(redirs []*syntax.Redirect, check func(string) bool) bool {
	for _, r := range redirs {
		if r == nil || r.Hdoc == nil {
			continue
		}
		if check(wordText(r.Hdoc)) {
			return true
		}
	}
	return false
}

func commandFromFields(fields map[string]any) (string, bool) {
	for _, key := range []string{"command", "cmd", "CommandLine"} {
		if s, ok := fields[key].(string); ok {
			return s, true
		}
	}
	return "", false
}

func workDirFromFields(fields map[string]any) string {
	for _, key := range lawWorkDirKeys {
		if s, ok := fields[key].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func commandWordsFromFields(fields map[string]any) ([]string, bool) {
	for _, key := range []string{"command", "cmd", "args"} {
		raw, ok := fields[key].([]any)
		if !ok || len(raw) == 0 {
			continue
		}
		words := make([]string, 0, len(raw))
		allStrings := true
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				allStrings = false
				break
			}
			words = append(words, s)
		}
		if allStrings {
			return words, true
		}
	}
	return nil, false
}

func fieldsTargetProtectedLawInDir(value any, dir string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range lawPathKeys {
			if path, ok := v[key].(string); ok && isProtectedLawPathInDir(path, dir) {
				return true
			}
		}
		if edits, ok := v["edits"]; ok && fieldsTargetProtectedLawInDir(edits, dir) {
			return true
		}
		if files, ok := v["files"]; ok && fieldsTargetProtectedLawInDir(files, dir) {
			return true
		}
	case []any:
		for _, item := range v {
			if fieldsTargetProtectedLawInDir(item, dir) {
				return true
			}
		}
	}
	return false
}

func patchTargetsProtectedLawInDir(text, dir string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{
			"*** Add File:",
			"*** Update File:",
			"*** Delete File:",
			"*** Move to:",
			"+++ ",
			"--- ",
		} {
			rest, ok := strings.CutPrefix(trimmed, prefix)
			if !ok {
				continue
			}
			path := strings.TrimSpace(rest)
			if prefix == "+++ " || prefix == "--- " {
				fields := strings.Fields(path)
				if len(fields) == 0 {
					continue
				}
				path = fields[0]
				path = strings.TrimPrefix(path, "a/")
				path = strings.TrimPrefix(path, "b/")
			}
			if isProtectedLawPathInDir(path, dir) {
				return true
			}
		}
	}
	return false
}

func resolveLawDir(base, sub string) string {
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return base
	}
	if base == "" || filepath.IsAbs(sub) || strings.HasPrefix(sub, "~") || strings.HasPrefix(sub, "$") {
		return sub
	}
	return filepath.Join(base, sub)
}

func joinLawPath(raw, dir string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	if s == "" || dir == "" {
		return s
	}
	norm := strings.ReplaceAll(s, `\`, "/")
	if strings.HasPrefix(norm, "/") || strings.HasPrefix(norm, "~") || strings.HasPrefix(norm, "$") {
		return norm
	}
	return filepath.ToSlash(filepath.Join(strings.ReplaceAll(dir, `\`, "/"), norm))
}

func normalizeLawPath(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	s = strings.ReplaceAll(s, `\`, "/")
	if s == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(s)))
}

func dirInToolsLaws(dir string) bool {
	p := normalizeLawPath(dir)
	return p != "" && hasPathSuffix(p, "tools/laws")
}

func isProtectedLawPath(raw string) bool {
	p := normalizeLawPath(raw)
	if p == "" || p == "." {
		return false
	}
	if filepath.Base(p) == "laws.bend" {
		return true
	}
	return hasPathSuffix(p, "tools/laws/approve") || hasPathSuffix(p, "tools/laws/approved")
}

func isProtectedLawPathInDir(raw, dir string) bool {
	return isProtectedLawPath(raw) || (dir != "" && isProtectedLawPath(joinLawPath(raw, dir)))
}

func isApproveScriptInDir(raw, dir string, inToolsLaws bool) bool {
	if isApproveScript(raw, inToolsLaws) {
		return true
	}
	return dir != "" && isApproveScript(joinLawPath(raw, dir), inToolsLaws)
}

func isApproveScript(raw string, inToolsLaws bool) bool {
	p := normalizeLawPath(raw)
	if p == "" {
		return false
	}
	if hasPathSuffix(p, "tools/laws/approve") {
		return true
	}
	return inToolsLaws && filepath.Base(p) == "approve"
}

func matchesProtectedLawTargetInDir(raw, dir string, inToolsLaws bool) bool {
	if matchesProtectedLawTarget(raw, inToolsLaws) {
		return true
	}
	return dir != "" && matchesProtectedLawTarget(joinLawPath(raw, dir), inToolsLaws)
}

func matchesProtectedLawTarget(raw string, inToolsLaws bool) bool {
	if isProtectedLawPath(raw) {
		return true
	}
	p := normalizeLawPath(raw)
	if p == "" {
		return false
	}
	if inToolsLaws && (filepath.Base(p) == "approve" || filepath.Base(p) == "approved") {
		return true
	}
	if !strings.ContainsAny(p, "*?[") {
		return false
	}
	if !strings.Contains(p, "/") {
		if matched, _ := filepath.Match(p, "laws.bend"); matched {
			return true
		}
		if inToolsLaws {
			if matched, _ := filepath.Match(p, "approve"); matched {
				return true
			}
			if matched, _ := filepath.Match(p, "approved"); matched {
				return true
			}
		}
		return false
	}
	for _, candidate := range []string{"tools/laws/approve", "tools/laws/approved"} {
		if globMatchesSuffix(p, candidate) {
			return true
		}
	}
	// Match patterns like ./LAWS* or /repo/LAWS* without matching laws/*.bend.
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	if base != "" && !strings.HasSuffix(dir, "/laws") && dir != "laws" && !strings.Contains(dir, "/laws/") {
		if matched, _ := filepath.Match(base, "laws.bend"); matched {
			return true
		}
	}
	return false
}

func globMatchesSuffix(pattern, suffix string) bool {
	if matched, _ := filepath.Match(pattern, suffix); matched {
		return true
	}
	parts := strings.Split(pattern, "/")
	suffixParts := strings.Split(suffix, "/")
	if len(parts) < len(suffixParts) {
		return false
	}
	tail := strings.Join(parts[len(parts)-len(suffixParts):], "/")
	matched, _ := filepath.Match(tail, suffix)
	return matched
}

func hasPathSuffix(p, suffix string) bool {
	return p == suffix || strings.HasSuffix(p, "/"+suffix)
}

func rawMentionsProtectedLaw(text string) bool {
	lower := strings.ToLower(strings.ReplaceAll(text, `\`, "/"))
	return strings.Contains(lower, "laws.bend") ||
		strings.Contains(lower, "tools/laws/approve") ||
		strings.Contains(lower, "tools/laws/approved")
}

func mentionsProtectedLaw(text string, inToolsLaws bool) bool {
	if rawMentionsProtectedLaw(text) {
		return true
	}
	if !inToolsLaws {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "approve")
}
