package gitutil

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommitDirs returns the folders a shell command runs git commit in, in
// order. It follows plain cd steps and git -C; workingDir is where the
// command starts. Words it can't read literally (variables, $(...)) are
// skipped, except a leading ~ for the home folder.
func CommitDirs(command, workingDir string) []string {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}
	cur := workingDir
	var dirs []string
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		words := make([]string, len(call.Args))
		for i, arg := range call.Args {
			words[i] = literalWord(arg)
		}
		switch words[0] {
		case "cd":
			if len(words) > 1 && words[1] != "" {
				cur = joinDir(cur, words[1])
			}
		case "git":
			dir := cur
			for i := 1; i < len(words); i++ {
				word := words[i]
				if word == "-C" && i+1 < len(words) {
					i++
					if words[i] == "" {
						return true
					}
					dir = joinDir(dir, words[i])
					continue
				}
				if word == "-c" {
					i++ // -c name=value
					continue
				}
				if strings.HasPrefix(word, "-") {
					continue
				}
				if word == "commit" && dir != "" {
					dirs = append(dirs, dir)
				}
				break
			}
		}
		return true
	})
	return dirs
}

// literalWord returns a shell word's plain text, or "" when part of it is
// only known when the shell runs it.
func literalWord(word *syntax.Word) string {
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(lit.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}

// joinDir resolves dir against base, expanding a leading ~.
func joinDir(base, dir string) string {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	if !filepath.IsAbs(dir) {
		if base == "" {
			return ""
		}
		dir = filepath.Join(base, dir)
	}
	return filepath.Clean(dir)
}

// SameRepoWorktree returns the root of the Git checkout holding path when
// that checkout belongs to the same repository as mainDir (the main folder
// or one of its worktrees), and "" otherwise.
func SameRepoWorktree(mainDir, path string) string {
	root := checkoutRoot(path)
	if root == "" {
		return ""
	}
	mainCommon := commonGitDir(mainDir)
	if mainCommon == "" || mainCommon != commonGitDir(root) {
		return ""
	}
	return root
}

// checkoutRoot walks up from path to the folder holding .git.
func checkoutRoot(path string) string {
	dir, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				return real
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// commonGitDir is the repository's shared .git folder, the same for the
// main folder and all its worktrees.
func commonGitDir(dir string) string {
	out, err := execGit(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(out)
	if real, err := filepath.EvalSymlinks(common); err == nil {
		return real
	}
	return common
}

// IsCrushCopy reports whether dir is a folder copy Crush made itself for a
// chat (under the data folder's worktrees folder).
func IsCrushCopy(dataDir, dir string) bool {
	if dataDir == "" || dir == "" {
		return false
	}
	base, err := filepath.Abs(filepath.Join(dataDir, "worktrees"))
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(base, abs)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

// HasCheckout reports whether dir is still a Git checkout.
func HasCheckout(dir string) bool { return hasWorktree(dir) }
