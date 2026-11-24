// Added AGY accounts use a private Secret Service view and a temporary
// .gemini directory, so AGY can never persist its fallback login files.
package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/crush/internal/config"
)

const agyProfileFile = "agy-keyring-id"

// AGYBin finds the CLI itself, without any shell wrapper.
func AGYBin() string {
	if home, err := os.UserHomeDir(); err == nil {
		bin := filepath.Join(home, ".local", "bin", "agy")
		if info, err := os.Stat(bin); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return bin
		}
	}
	return "agy"
}

func prepareAGY(dir string) error {
	if err := agyTools(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, agyProfileFile)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte(rand.Text()), 0o600)
}

func agyTools() error {
	if runtime.GOOS != "linux" {
		return errors.New("additional AGY accounts require Linux")
	}
	for _, name := range []string{"bwrap", "dbus-daemon"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("additional AGY accounts require %s: %w", name, err)
		}
	}
	return nil
}

// AGYCommand routes logins, turns and usage reads through the same profile.
func AGYCommand(id, name string, args []string, login bool) (string, []string, error) {
	if id == "" {
		return name, args, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	mode := "run"
	if login {
		mode = "login"
	}
	return exe, append([]string{"__agy-account", "--", id, mode, name}, args...), nil
}

func agyKeyringID(dir string) (string, error) {
	if dir == "" {
		return "antigravity", nil
	}
	value, err := os.ReadFile(filepath.Join(dir, agyProfileFile))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(value))
	if id == "" {
		return "", errors.New("AGY account has no keyring ID")
	}
	return "crush-agy-" + id, nil
}

func agySignedIn(id string) bool {
	if testing.Testing() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	ring, err := openAGYKeyring(ctx, config.AccountDir(id))
	if err != nil {
		return false
	}
	defer ring.host.Close()
	items, err := ring.search(ctx)
	return err == nil && len(items) > 0
}

func removeAGYLogin(dir string) error {
	if testing.Testing() {
		return nil
	}
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, agyProfileFile)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ring, err := openAGYKeyring(ctx, dir)
	if err != nil {
		return err
	}
	defer ring.host.Close()
	items, err := ring.search(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := ring.delete(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// RunAGYProfile starts one AGY process with access to only its profile entry
// through Secret Service. The existing guarded/read-only command, if any,
// runs inside this filesystem view with its own restrictions.
func RunAGYProfile(ctx context.Context, id, name string, args []string, login bool) error {
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return errors.New("invalid AGY account")
	}
	if err := agyTools(); err != nil {
		return err
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	ring, err := openAGYKeyring(check, config.AccountDir(id))
	if err == nil && !login {
		var count int
		count, err = ring.loginCount(check)
		if err == nil && count == 0 {
			err = errors.New("sign in to this AGY account in Settings first")
		}
	}
	cancel()
	if ring != nil {
		defer ring.host.Close()
	}
	if err != nil {
		return err
	}
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	address, busDir, closeBus, err := ring.startBus(ctx, stop)
	if err != nil {
		return err
	}
	defer closeBus()

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	gemini := filepath.Join(home, ".gemini")
	cli := filepath.Join(gemini, "antigravity-cli")
	wrap := []string{"--die-with-parent", "--bind", "/", "/", "--dev-bind", "/dev", "/dev", "--tmpfs", gemini,
		"--setenv", "DBUS_SESSION_BUS_ADDRESS", address,
		"--unsetenv", "AGY_ADC_AUTH", "--unsetenv", "GEMINI_API_KEY", "--unsetenv", "GOOGLE_API_KEY"}
	wrap = append(wrap, agyOpener(busDir)...)
	// Share only known non-login state. Bind whole conversation directories
	// so SQLite can open and rename its sidecar files normally.
	for _, path := range []string{"conversations", "brain", "annotations", "mcp", "knowledge"} {
		path = filepath.Join(cli, path)
		wrap = append(wrap, "--bind-try", path, path)
	}
	skills := filepath.Join(gemini, "skills")
	wrap = append(wrap, "--ro-bind-try", skills, skills, "--dir", cli)
	// AGY saves its settings (the terms answer too) and setup steps by
	// replacing the file, which a shared read-only file refuses. These start
	// as copies of account 1's; what AGY writes stays in the sandbox.
	var copies []*os.File
	for _, path := range []string{filepath.Join(cli, "settings.json"), filepath.Join(cli, "jetski_state.pbtxt")} {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		defer file.Close()
		wrap = append(wrap, "--perms", "0600", "--file", strconv.Itoa(3+len(copies)), path)
		copies = append(copies, file)
	}
	cmd := exec.CommandContext(ctx, "bwrap", append(append(wrap, "--", name), args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.ExtraFiles = copies
	err = cmd.Run()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if !login {
		return err
	}
	// The user quits AGY once signed in, however they like; the saved login
	// decides, not AGY's exit status.
	check, cancel = context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	count, countErr := ring.loginCount(check)
	if countErr != nil {
		return countErr
	}
	if count == 0 {
		if err != nil {
			return err
		}
		return errors.New("AGY did not save a login in the keyring")
	}
	return nil
}
