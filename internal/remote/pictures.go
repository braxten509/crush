package remote

import (
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/crush/internal/message"
)

// Crush Desktop draws the pictures a reply names in its Markdown
// ("![plan](out/plan.png)") where they stand. The phone gets the same: each
// reply item lists its picture files, and /v1/media/<message id>/p<N> serves
// the Nth picture mark of that reply. The path always comes from the reply
// the PC holds, never from the phone, and only a file whose content is a
// picture is sent.

// maxReplyPicture is Crush Desktop's limit for a picture read from a file
// (FigureView.svelte PICTURE_MAX_BYTES).
const maxReplyPicture = 5 << 20

// Picture is a picture file a reply names.
type Picture struct {
	ID string `json:"id"` // media id: <message id>/p<index>
	// Target is the address as the reply wrote it; the phone finds the
	// picture's place in the text by it.
	Target string `json:"target"`
}

// pictureMark is a Markdown picture: ![words](address "title"). The address
// may stand in <> to hold spaces.
var pictureMark = regexp.MustCompile(`!\[[^\]\n]*\]\(\s*(<[^>\n]*>|[^\s()]+)(?:\s+(?:"[^"\n]*"|'[^'\n]*'))?\s*\)`)

// pictureTargets returns the address of every picture mark in a reply, in
// order. Marks inside code (fenced blocks and `spans`) are text, not
// pictures.
func pictureTargets(text string) []string {
	if !strings.Contains(text, "![") {
		return nil
	}
	var out []string
	fence := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if mark := fenceMark(trimmed); mark != "" {
			switch fence {
			case "":
				fence = mark
			case mark:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, m := range pictureMark.FindAllStringSubmatchIndex(line, -1) {
			if strings.Count(line[:m[0]], "`")%2 == 1 {
				continue
			}
			target := line[m[2]:m[3]]
			if strings.HasPrefix(target, "<") {
				target = target[1 : len(target)-1]
			}
			out = append(out, target)
		}
	}
	return out
}

// fenceMark is the fence a line opens or closes ("```" or "~~~"), or "".
func fenceMark(trimmed string) string {
	for _, mark := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, mark) {
			return mark
		}
	}
	return ""
}

var schemeStart = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// pictureFile is the file a picture address names, or "" for one that names
// no file on this PC (a web picture, a data: picture, another scheme).
// Relative paths start at the chat's folder; "~/" is the home folder.
func pictureFile(target, workingDir string) string {
	path := target
	switch {
	case strings.HasPrefix(strings.ToLower(target), "file:"):
		u, err := url.Parse(target)
		if err != nil || (u.Host != "" && u.Host != "localhost") || u.Path == "" {
			return ""
		}
		path = u.Path
	case schemeStart.MatchString(target):
		return ""
	}
	if path == "" || strings.ContainsRune(path, 0) {
		return ""
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(path) {
		if workingDir == "" {
			return ""
		}
		path = filepath.Join(workingDir, path)
	}
	return filepath.Clean(path)
}

// replyPictures lists the picture files a reply names, for its item.
func replyPictures(msgID, text, workingDir string) []Picture {
	var out []Picture
	for i, target := range pictureTargets(text) {
		if pictureFile(target, workingDir) == "" {
			continue
		}
		out = append(out, Picture{ID: msgID + "/p" + strconv.Itoa(i), Target: target})
	}
	return out
}

// readPicture reads a picture file for the phone: a regular file of at most
// maxReplyPicture bytes whose content is a picture.
func readPicture(path string) (string, []byte, bool) {
	// Looked at before it is opened: opening a named pipe would wait for
	// its writer.
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > maxReplyPicture {
		return "", nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", nil, false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > maxReplyPicture {
		return "", nil, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxReplyPicture+1))
	if err != nil || len(data) > maxReplyPicture {
		return "", nil, false
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return "", nil, false
	}
	return mime, data, true
}

// queueMedia stands where the message id does in the media id of a waiting
// prompt's file: queue/<mark>.
const queueMedia = "queue"

// fileMark tells one waiting file from another by its name, size and both
// ends of its content. The place in the queue would not do: it moves as
// prompts leave, and the phone keeps pictures by their media id.
func fileMark(a message.Attachment) string {
	const edge = 4 << 10
	h := fnv.New64a()
	h.Write([]byte(a.FileName))
	h.Write([]byte{0})
	h.Write([]byte(strconv.Itoa(len(a.Content))))
	h.Write(a.Content[:min(edge, len(a.Content))])
	h.Write(a.Content[max(0, len(a.Content)-edge):])
	return strconv.FormatUint(h.Sum64(), 36)
}

// queuedAttachments are the files of one waiting prompt, as the phone's
// chips. Never nil: every waiting prompt has a list.
func queuedAttachments(files []message.Attachment) []Attachment {
	out := make([]Attachment, 0, len(files))
	for i, a := range files {
		name := a.FileName
		if name == "" {
			name = filepath.Base(a.FilePath)
		}
		if name == "" || name == "." {
			name = "attachment " + strconv.Itoa(i+1)
		}
		out = append(out, Attachment{ID: queueMedia + "/" + fileMark(a), Name: name, Mime: a.MimeType, Image: a.IsImage()})
	}
	return out
}

// queuedFile finds a waiting prompt's file by its mark.
func queuedFile(prompts []message.QueuedPrompt, mark string) (string, []byte, bool) {
	for _, q := range prompts {
		for _, a := range q.Attachments {
			if fileMark(a) == mark {
				return a.MimeType, a.Content, true
			}
		}
	}
	return "", nil, false
}
