package filechange

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Deletions are never paused for review, and Git (checkout, merge, reset,
// rebase, stash) deletes a file before writing its new version. Such a file
// would look brand new, so its version in the commit checked out when the
// command first wrote is used as its baseline instead.

// headCommit reads the checked-out commit without starting Git, since it runs
// while a command's system call waits. Empty when it can't be read.
func headCommit(repository string) string {
	gitDir := filepath.Join(repository, ".git")
	if data, err := os.ReadFile(gitDir); err == nil { // a linked worktree
		dir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
		if !ok {
			return ""
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(repository, dir)
		}
		gitDir = dir
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	ref, symbolic := strings.CutPrefix(strings.TrimSpace(string(data)), "ref: ")
	if !symbolic {
		return ref
	}
	common := gitDir
	if data, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common = strings.TrimSpace(string(data))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
	}
	if data, err := os.ReadFile(filepath.Join(common, ref)); err == nil {
		return strings.TrimSpace(string(data))
	}
	data, _ = os.ReadFile(filepath.Join(common, "packed-refs"))
	for line := range strings.SplitSeq(string(data), "\n") {
		if commit, name, ok := strings.Cut(line, " "); ok && name == ref {
			return commit
		}
	}
	return ""
}

// gitBaselines gives changes that look new the version Git had at commit.
// Files Git didn't have stay new.
func gitBaselines(repository, commit string, changes []Change) {
	var paths []string
	index := map[string]int{}
	for i, change := range changes {
		if change.Before != nil || change.After == nil || change.Transfer != nil {
			continue
		}
		relative, err := filepath.Rel(repository, change.Path)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		relative = filepath.ToSlash(relative)
		paths = append(paths, relative)
		index[relative] = i
	}
	if commit == "" || len(paths) == 0 {
		return
	}
	list := exec.Command("git", append([]string{"-C", repository, "ls-tree", "-z", "--full-tree", commit, "--"}, paths...)...)
	listed, err := list.Output()
	if err != nil {
		return
	}
	type blob struct {
		path string
		mode fs.FileMode
	}
	var blobs []blob
	var ids bytes.Buffer
	for record := range bytes.SplitSeq(listed, []byte{0}) {
		// <mode> SP <type> SP <object> TAB <path>
		meta, path, ok := bytes.Cut(record, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		mode := fs.FileMode(0o644)
		switch fields[0] {
		case "100755":
			mode = 0o755
		case "120000":
			mode = fs.ModeSymlink | 0o777
		}
		blobs = append(blobs, blob{path: string(path), mode: mode})
		ids.WriteString(fields[2] + "\n")
	}
	if len(blobs) == 0 {
		return
	}
	read := exec.Command("git", "-C", repository, "cat-file", "--batch")
	read.Stdin = &ids
	output, err := read.StdoutPipe()
	if err != nil || read.Start() != nil {
		return
	}
	defer read.Wait() //nolint:errcheck
	reader := bufio.NewReader(output)
	textBudget, restoreBudget := maxTextTotal, MaxRestoreTotal
	for _, b := range blobs {
		// <object> SP <type> SP <size> LF <contents> LF
		header, err := reader.ReadString('\n')
		fields := strings.Fields(header)
		if err != nil || len(fields) != 3 {
			return
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			return
		}
		data := make([]byte, size+1)
		if _, err := io.ReadFull(reader, data); err != nil {
			return
		}
		state := blobState(data[:size], b.mode, &textBudget, &restoreBudget)
		changes[index[b.path]].Before = &state
	}
}

// blobState mirrors readReviewEntry for contents that come from Git.
func blobState(data []byte, mode fs.FileMode, textBudget, restoreBudget *int) State {
	state := State{Size: int64(len(data)), Mode: uint32(mode), Digest: digest(data)}
	switch {
	case mode&fs.ModeSymlink != 0:
		state.RestoreOmitted = "Symbolic links and special files cannot be restored"
	case len(data) > MaxRestoreSize:
		state.RestoreOmitted = "Too large to restore (over 10 MB)"
	case len(data) > *restoreBudget:
		state.RestoreOmitted = RestoreBudgetReason
	default:
		state.RestoreData = base64.StdEncoding.EncodeToString(data)
		*restoreBudget -= len(data)
	}
	switch {
	case mode&fs.ModeSymlink != 0:
		state.Content = string(data)
	case len(data) > maxTextSize:
		state.Omitted = "File exceeds " + strconv.Itoa(maxTextSize) + " byte text preview limit"
	case !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0:
		state.Omitted = "Binary file"
	case len(data) > *textBudget:
		state.Omitted = "Text preview unavailable (file review memory limit)"
	default:
		state.Content = string(data)
		*textBudget -= len(data)
	}
	return state
}
