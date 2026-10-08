package sessionhost

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/home"
)

// pickerRows is how many matching folders, and how many recent ones, the
// new-session box shows at most.
const pickerRows = 5

// picker is the box "+ New session" opens to choose the session's folder:
// a typed path, folders matching it, and folders Crush was used in lately.
type picker struct {
	// base is the folder a relative path starts from: the shown session's.
	base   string
	input  []rune
	cursor int // in input
	// sel is the picked row of matches followed by recent; -1 is the typed
	// path.
	sel     int
	matches []string
	recent  []string
	err     string
}

// newPicker opens the box with dir typed in, ready for a folder inside it.
func newPicker(dir string, recent []string) *picker {
	p := &picker{base: dir, sel: -1}
	p.setInput(withSlash(home.Short(dir)))
	for _, r := range recent {
		if len(p.recent) == pickerRows {
			break
		}
		if fi, err := os.Stat(r); err == nil && fi.IsDir() && !slices.Contains(p.recent, r) {
			p.recent = append(p.recent, r)
		}
	}
	return p
}

func withSlash(path string) string {
	if strings.HasSuffix(path, string(filepath.Separator)) {
		return path
	}
	return path + string(filepath.Separator)
}

func (p *picker) setInput(text string) {
	p.input = []rune(text)
	p.cursor = len(p.input)
	p.edited()
}

// edited follows a change to the typed path.
func (p *picker) edited() {
	p.sel = -1
	p.err = ""
	p.matches = matchFolders(p.expand(string(p.input)))
}

// expand turns a typed path into a full one: ~ is the home folder and a
// relative path starts from base.
func (p *picker) expand(typed string) string {
	switch {
	case typed == "~":
		return home.Dir()
	case strings.HasPrefix(typed, "~"+string(filepath.Separator)):
		return withSlash(home.Dir()) + typed[2:]
	case typed == "" || filepath.IsAbs(typed):
		return typed
	}
	return withSlash(p.base) + typed
}

// matchFolders lists the folders a typed path could go on to: those in its
// folder whose names start with its last part, ignoring case. Hidden ones
// show only once a dot is typed.
func matchFolders(path string) []string {
	if path == "" {
		return nil
	}
	parent, part := filepath.Dir(path), filepath.Base(path)
	if strings.HasSuffix(path, string(filepath.Separator)) {
		parent, part = path, ""
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(part, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(part)) {
			continue
		}
		full := filepath.Join(parent, name)
		if !e.IsDir() {
			// A link to a folder counts as one.
			if fi, err := os.Stat(full); err != nil || !fi.IsDir() {
				continue
			}
		}
		out = append(out, full)
		if len(out) == pickerRows {
			break
		}
	}
	return out
}

// rows returns every row that can be picked: the matches, then the recent
// folders.
func (p *picker) rows() []string {
	return append(slices.Clip(p.matches), p.recent...)
}

func (p *picker) move(by int) {
	n := len(p.rows())
	if n == 0 {
		return
	}
	// The typed path is the stop before the first row.
	p.sel = (p.sel+1+by+n+1)%(n+1) - 1
}

// complete puts the picked row, or else the first match, into the typed
// path, to go on into it.
func (p *picker) complete() {
	rows := p.rows()
	switch {
	case p.sel >= 0 && p.sel < len(rows):
		p.setInput(withSlash(home.Short(rows[p.sel])))
	case len(p.matches) > 0:
		p.setInput(withSlash(home.Short(p.matches[0])))
	}
}

// choice returns the folder to start in: the picked row or the typed path.
// It reports false, with err set, when there is no such folder.
func (p *picker) choice() (string, bool) {
	dir := ""
	if rows := p.rows(); p.sel >= 0 && p.sel < len(rows) {
		dir = rows[p.sel]
	} else {
		typed := strings.TrimSpace(string(p.input))
		if typed == "" {
			p.err = "Type a folder."
			return "", false
		}
		dir = filepath.Clean(p.expand(typed))
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		p.err = "No folder at " + home.Short(dir)
		return "", false
	}
	return dir, true
}

func (p *picker) insert(text string) {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, text)
	if text == "" {
		return
	}
	add := []rune(text)
	p.input = slices.Insert(p.input, p.cursor, add...)
	p.cursor += len(add)
	p.edited()
}

func (p *picker) backspace() {
	if p.cursor == 0 {
		return
	}
	p.input = slices.Delete(p.input, p.cursor-1, p.cursor)
	p.cursor--
	p.edited()
}

func (p *picker) deleteForward() {
	if p.cursor >= len(p.input) {
		return
	}
	p.input = slices.Delete(p.input, p.cursor, p.cursor+1)
	p.edited()
}

// deleteWord removes the folder name before the cursor, with its slash.
func (p *picker) deleteWord() {
	i := p.cursor
	for i > 0 && p.input[i-1] == filepath.Separator {
		i--
	}
	for i > 0 && p.input[i-1] != filepath.Separator {
		i--
	}
	if i == p.cursor {
		return
	}
	p.input = slices.Delete(p.input, i, p.cursor)
	p.cursor = i
	p.edited()
}
