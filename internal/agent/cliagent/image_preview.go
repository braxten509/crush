package cliagent

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/secretguard"
)

func imageFilename(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpeg", ".jpg", ".gif", ".webp", ".bmp", ".ico", ".avif":
		return true
	}
	return false
}

// AGY and Codex can report a successful view with only the local path.
// Capture it when the tool finishes so reopening the chat needs no file.
func imagePreviews(m *Model, t Turn) func(Event) error {
	var own string
	for _, env := range t.Env {
		if value, ok := strings.CutPrefix(env, secretguard.ChatFolderEnv+"="); ok {
			own = value
		}
	}
	return func(e Event) error {
		if e.Type == EventToolResult && e.Name == tools.ViewToolName && !e.IsError && e.Data == "" {
			var view tools.ViewResponseMetadata
			if json.Unmarshal([]byte(e.Metadata), &view) == nil && (m.Kind == config.TypeCodexCLI || imageFilename(view.FilePath)) {
				e.Data, e.MimeType = readPreview(view.FilePath, m.Dir, own)
			}
		}
		return t.Emit(e)
	}
}

func readPreview(path, dir, own string) (string, string) {
	if path == "" {
		return "", ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	blocked := func(name string) bool {
		return secretguard.BlockReasonInDir(tools.ViewToolName, []byte(marshal(map[string]string{"file_path": name})), own, dir) != ""
	}
	if blocked(path) {
		return "", ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || (resolved != path && blocked(resolved)) {
		return "", ""
	}
	const limit = 32 << 20
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", ""
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) > limit {
		return "", ""
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		return "", ""
	}
	return base64.StdEncoding.EncodeToString(data), mime
}
