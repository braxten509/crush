package filechange

import (
	"path/filepath"
	"strings"
)

// generatedArtifact recognizes binary build products in conventional output
// folders. Source files and existing-file overwrites retain normal snapshots.
func generatedArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".class", ".jar", ".apk", ".dex", ".o", ".obj", ".a", ".so", ".dll", ".exe", ".pyc", ".pdb", ".wasm":
	default:
		return false
	}
	for _, part := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
		switch part {
		case "build", "dist", "target", "node_modules", ".gradle", "__pycache__":
			return true
		}
	}
	return false
}
