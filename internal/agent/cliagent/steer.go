package cliagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/crush/internal/message"
)

// pollSteer hands queued messages to send while the turn runs, whenever
// ready says the CLI can take one. The returned stop func is idempotent;
// once it returns, send is never called again.
func pollSteer(t Turn, ready func() bool, send func(text string)) (stop func()) {
	t.SteerInput = nil // Text-only drivers leave images queued, preserving their ordering.
	return pollSteerInput(t, ready, func(text string, _ []message.Attachment) { send(text) })
}

func pollSteerInput(t Turn, ready func() bool, send func(string, []message.Attachment)) (stop func()) {
	return pollSteerInputRetry(t, ready, func(text string, attachments []message.Attachment) bool {
		send(text, attachments)
		return true
	})
}

// pollSteerInputRetry retains input when a turn transition prevents delivery.
func pollSteerInputRetry(t Turn, ready func() bool, send func(string, []message.Attachment) bool) (stop func()) {
	if (t.Steer == nil && t.SteerInput == nil) || t.NoTools {
		return func() {}
	}
	var (
		mu      sync.Mutex
		stopped bool
	)
	done := make(chan struct{})
	go func() {
		var text string
		var attachments []message.Attachment
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.SteerReady:
			case <-tick.C:
			}
			mu.Lock()
			if !stopped && ready() {
				if text == "" && len(attachments) == 0 {
					if t.SteerInput != nil {
						text, attachments = t.SteerInput()
					} else {
						text = t.Steer()
					}
				}
				if (text != "" || len(attachments) != 0) && send(text, attachments) {
					text, attachments = "", nil
				}
			}
			mu.Unlock()
		}
	}()
	return func() {
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopped = true
			close(done)
		}
	}
}

// withImagePaths saves the prompt's images as Crush's own copies and lists
// their paths, for CLIs that take no image input but can open image files.
// The original may be a temporary file (a screenshot tool's) that is gone by
// the time the CLI looks.
func withImagePaths(prompt string, attachments []message.Attachment) string {
	return prompt + imagePathNote(attachments, "open them with your file-reading tool")
}

// withSavedImagePaths lists saved copies of images a CLI also gets inline,
// so the model can hand them on by path, as to a sub-agent.
func withSavedImagePaths(prompt string, attachments []message.Attachment) string {
	return prompt + imagePathNote(attachments, "they are also saved as files at")
}

// imageNoteStart begins every image path note, so echoes of a prompt can be
// matched back to what the user typed.
const imageNoteStart = "\n\n<system_info>The user attached these images; "

func imagePathNote(attachments []message.Attachment, how string) string {
	var sb strings.Builder
	for _, a := range attachments {
		if !a.IsImage() {
			continue
		}
		path, err := saveImage(a)
		if err != nil {
			slog.Error("Saving image attachment", "error", err)
			continue
		}
		fmt.Fprintf(&sb, "\n%s", path)
	}
	if sb.Len() == 0 {
		return ""
	}
	return imageNoteStart + how + ":</system_info>" + sb.String()
}

// withoutImagePaths drops an image path note from an echoed prompt.
func withoutImagePaths(text string) string {
	if i := strings.Index(text, imageNoteStart); i >= 0 {
		return text[:i]
	}
	return text
}

// saveImage writes an image attachment to Crush's attachment folder, named
// by its content so a resent image reuses the file.
func saveImage(a message.Attachment) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "crush", "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256(a.Content)
	ext := filepath.Ext(a.FileName)
	if ext == "" {
		ext = "." + strings.TrimPrefix(a.MimeType, "image/")
	}
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+ext)
	now := time.Now()
	defer pruneAttachments(dir, now)
	if _, err := os.Stat(path); err == nil {
		return path, os.Chtimes(path, now, now)
	}
	return path, os.WriteFile(path, a.Content, 0o600)
}

// steered tracks messages sent to a CLI mid-turn until it confirms them.
type steered struct {
	mu    sync.Mutex
	texts []string
}

func (s *steered) add(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
}

// take removes text if it was steered, reporting whether it was.
func (s *steered) take(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.texts {
		if t == text {
			s.texts = append(s.texts[:i], s.texts[i+1:]...)
			return true
		}
	}
	return false
}

func (s *steered) pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.texts)
}
