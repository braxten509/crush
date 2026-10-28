// Package sandbox runs commands read-only inside Codex's sandbox.
package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReadOnly wraps a command so it runs in dir with nothing writable but the
// temporary folder, ~/.cache and the existing writable folders, so it can
// read anything but can't change dir. It returns the folder to start the
// wrapped command in and the wrapped command. The sandbox makes its own
// working folder writable, so it starts in the temporary folder (writable
// anyway) and moves into dir itself.
func ReadOnly(dir string, writable []string, name string, args []string) (string, string, []string, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return "", "", nil, errors.New("read-only commands run inside Codex's sandbox, and codex isn't installed")
	}
	tmp := os.TempDir()
	if Within(dir, tmp) {
		return "", "", nil, fmt.Errorf("can't keep %s read-only: the sandbox always lets programs write in %s", dir, tmp)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", nil, err
	}
	var roots []string
	for _, root := range append([]string{filepath.Join(home, ".cache")}, writable...) {
		if fi, err := os.Stat(root); err == nil && fi.IsDir() && !Within(dir, root) {
			roots = append(roots, fmt.Sprintf("%q", root))
		}
	}
	wrapped := []string{
		"sandbox",
		"-c", `sandbox_mode="workspace-write"`,
		"-c", "sandbox_workspace_write.network_access=true",
		"-c", "sandbox_workspace_write.writable_roots=[" + strings.Join(roots, ",") + "]",
		"--", "/bin/sh", "-c", `cd "$0" && exec "$@"`, dir, name,
	}
	return tmp, "codex", append(wrapped, args...), nil
}

// Within reports whether path is dir or inside it.
func Within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
