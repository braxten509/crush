package model

import (
	"crypto/sha256"
	"fmt"
	"image"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/util"
)

var linkLineSuffix = regexp.MustCompile(`:[0-9]+(?::[0-9]+)?$`)

// linkTarget turns Markdown file destinations into desktop file URLs. Never
// interpret a destination as a shell command or an arbitrary application URI.
func linkTarget(destination, workingDir string) (string, error) {
	u, err := url.Parse(destination)
	if err != nil {
		return "", fmt.Errorf("cannot open link: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http", "mailto":
		return destination, nil
	case "file":
		if u.Host != "" && u.Host != "localhost" {
			return "", fmt.Errorf("cannot open a file on another computer")
		}
	case "":
		if u.Host != "" || u.Path == "" {
			return "", fmt.Errorf("link does not name a local file")
		}
	default:
		return "", fmt.Errorf("cannot open links of type %q", u.Scheme)
	}
	path := u.Path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDir, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Keep actual filenames containing colons. Only remove an editor line/column
	// suffix when that exact path does not exist.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = linkLineSuffix.ReplaceAllString(path, "")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("cannot open %s: %w", path, err)
	}
	return (&url.URL{Scheme: "file", Path: path}).String(), nil
}

func openChatLink(destination, workingDir string) tea.Cmd {
	return func() tea.Msg {
		target, err := linkTarget(destination, workingDir)
		if err == nil {
			err = openDesktopLink(target)
		}
		if err != nil {
			return util.ReportError(err)()
		}
		return nil
	}
}

func (m *UI) takeChatLink(c *Chat) tea.Cmd {
	if c.openImage != nil {
		attachment := *c.openImage
		c.openImage = nil
		return func() tea.Msg { return openImageMsg{attachment} }
	}
	destination := c.openLink
	c.openLink = ""
	if destination == "" {
		return nil
	}
	return openChatLink(destination, m.com.Workspace.WorkingDir())
}

type openImageMsg struct{ Attachment message.Attachment }
type imagePress struct {
	attachment message.Attachment
	at         image.Point
	dragged    bool
}

// imageFile preserves the image the user actually sent, even if its source
// has since changed or came from the clipboard. No work happens at render.
func imageFile(attachment message.Attachment) (string, error) {
	if len(attachment.Content) == 0 {
		if attachment.FilePath != "" {
			return attachment.FilePath, nil
		}
		return attachment.FileName, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	extension := ".png"
	if extensions, _ := mime.ExtensionsByType(attachment.MimeType); len(extensions) > 0 {
		extension = extensions[0]
	}
	directory := filepath.Join(cache, "crush", "image-viewer")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, fmt.Sprintf("%x%s", sha256.Sum256(attachment.Content), extension))
	if err := os.WriteFile(path, attachment.Content, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func openAttachmentImage(attachment message.Attachment, workingDir string) tea.Cmd {
	return func() tea.Msg {
		path, err := imageFile(attachment)
		if err != nil {
			return util.ReportError(err)()
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		return openChatLink((&url.URL{Scheme: "file", Path: path}).String(), workingDir)()
	}
}
