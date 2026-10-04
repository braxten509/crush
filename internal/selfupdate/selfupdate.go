// Package selfupdate keeps this fork of Crush on upstream's stable releases.
// A release is merged into the fork's checkout, built and tested there, and
// installed only after the user agrees. Clashes and failing tests are left
// to an AI agent (see [FixPrompt]); everything else is mechanical.
package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/cliupdate"
	"github.com/charmbracelet/crush/internal/update"
	"github.com/charmbracelet/crush/internal/version"
)

const module = "module github.com/charmbracelet/crush"

// ErrDirty means the checkout has unsaved changes, which an update must
// never touch.
var ErrDirty = errors.New("the Crush checkout has uncommitted changes")

// Release is a stable upstream release newer than the running fork.
type Release struct {
	Tag     string // e.g. v0.97.1
	Current string // the upstream release the running build is based on
	Dir     string // the fork checkout
}

// Dir returns the fork checkout this build came from, or "" when there is
// none (an upstream build, or the checkout is gone).
func Dir() string {
	for _, dir := range []string{version.SourceDir, defaultDir()} {
		if dir != "" && isFork(dir) {
			return dir
		}
	}
	return ""
}

func defaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "dev", "crush")
}

func isFork(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.HasPrefix(string(data), module+"\n") {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Check returns the latest stable upstream release when it is newer than
// the running build. Upstream's "latest" release never includes nightly or
// pre-release builds.
func Check(ctx context.Context, dir string) (*Release, error) {
	latest, err := update.Default.Latest(ctx)
	if err != nil {
		return nil, err
	}
	current, err := currentBase(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !cliupdate.Newer(latest.TagName, current) {
		return nil, nil
	}
	return &Release{Tag: latest.TagName, Current: current, Dir: dir}, nil
}

// currentBase is the upstream release the running build is based on. Fork
// builds carry it in their version ("v0.97.1-local.20261003-…"); other
// builds use the newest release tag in the checkout's history.
func currentBase(ctx context.Context, dir string) (string, error) {
	if base, _, ok := strings.Cut(version.Version, "-local"); ok && strings.HasPrefix(base, "v") {
		return base, nil
	}
	out, err := git(ctx, dir, "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "--exclude", "*-*", "HEAD")
	if err != nil {
		return "", fmt.Errorf("cannot tell which release the fork is on: %w", err)
	}
	return out, nil
}

// Merge brings the release into the checkout's current branch. It returns
// the files that clash; the merge is then left in progress for an agent to
// finish. A release that is already merged is not merged again.
func Merge(ctx context.Context, dir, tag string) (clashes []string, err error) {
	if MergeInProgress(ctx, dir) {
		return unmerged(ctx, dir), nil
	}
	if status, err := git(ctx, dir, "status", "--porcelain"); err != nil {
		return nil, err
	} else if status != "" {
		return nil, ErrDirty
	}
	// Start from the fork's latest published state.
	if _, err := git(ctx, dir, "pull", "--rebase=merges", "--quiet"); err != nil {
		return nil, fmt.Errorf("cannot update the checkout from GitHub: %w", err)
	}
	if _, err := git(ctx, dir, "fetch", "--quiet", "--no-tags", "origin", "tag", tag); err != nil {
		return nil, fmt.Errorf("cannot download %s: %w", tag, err)
	}
	if _, err := git(ctx, dir, "merge-base", "--is-ancestor", tag, "HEAD"); err == nil {
		return nil, nil
	}
	if _, err := git(ctx, dir, "merge", "--no-ff", "-m", "Merge upstream Crush "+tag, tag); err != nil {
		if clashes := unmerged(ctx, dir); len(clashes) > 0 {
			return clashes, nil
		}
		return nil, err
	}
	return nil, nil
}

// MergeInProgress reports whether a merge waits for its clashes to be fixed.
func MergeInProgress(ctx context.Context, dir string) bool {
	_, err := git(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

func unmerged(ctx context.Context, dir string) []string {
	out, _ := git(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// state records a build that passed its tests and waits to be installed.
type state struct {
	Tag     string    `json:"tag"`
	Version string    `json:"version"`
	Head    string    `json:"head"`
	Binary  string    `json:"binary"`
	BuiltAt time.Time `json:"built_at"`
}

// Build compiles the merged checkout and runs every test. The output of a
// failed step is returned with the error. A passing build is staged for
// [Install].
func Build(ctx context.Context, dir, tag string, log io.Writer) error {
	if MergeInProgress(ctx, dir) {
		return errors.New("the merge still has unresolved clashes")
	}
	if _, err := git(ctx, dir, "merge-base", "--is-ancestor", tag, "HEAD"); err != nil {
		return fmt.Errorf("%s is not merged into the checkout yet", tag)
	}
	if status, err := git(ctx, dir, "status", "--porcelain"); err != nil {
		return err
	} else if status != "" {
		return ErrDirty
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	stateDir, err := updateDir()
	if err != nil {
		return err
	}
	binary := filepath.Join(stateDir, "crush")
	ver := tag + "-local." + time.Now().Format("20060102") + "-fork"
	ldflags := fmt.Sprintf("-X github.com/charmbracelet/crush/internal/version.Version=%s -X github.com/charmbracelet/crush/internal/version.SourceDir=%s", ver, dir)
	fmt.Fprintf(log, "Building %s…\n", ver)
	if err := run(ctx, dir, log, "go", "build", "-buildvcs=false", "-ldflags", ldflags, "-o", binary, "."); err != nil {
		return fmt.Errorf("the build failed: %w", err)
	}
	fmt.Fprintln(log, "Running the tests…")
	if err := run(ctx, dir, log, testCommand(dir)...); err != nil {
		return fmt.Errorf("tests failed: %w", err)
	}
	return saveState(state{Tag: tag, Version: ver, Head: head, Binary: binary, BuiltAt: time.Now()})
}

// testCommand runs the suite through systemd-run when it is available:
// Crush's file watcher refuses nested watchers, so tests that start one fail
// inside a command Crush is watching.
func testCommand(dir string) []string {
	test := []string{"timeout", "1500", "go", "test", "./..."}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return test
	}
	cmd := []string{"systemd-run", "--user", "--wait", "--pipe", "--collect", "--quiet", "-p", "WorkingDirectory=" + dir}
	for _, name := range []string{"PATH", "HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "GOFLAGS"} {
		if value, ok := os.LookupEnv(name); ok {
			cmd = append(cmd, "-E", name+"="+value)
		}
	}
	return append(cmd, test...)
}

// Staged returns the version of a tested build of tag that is ready to
// install, or "" when there is none for the checkout's current state.
func Staged(ctx context.Context, dir, tag string) string {
	s, err := loadState()
	if err != nil || s.Tag != tag {
		return ""
	}
	head, err := git(ctx, dir, "rev-parse", "HEAD")
	if err != nil || head != s.Head {
		return ""
	}
	if _, err := os.Stat(s.Binary); err != nil {
		return ""
	}
	return s.Version
}

// Install replaces the running Crush with the staged build, keeping a copy
// of the old one, then publishes the merge to the fork on GitHub. A failed
// upload doesn't undo the install; it is returned as pushErr.
func Install(ctx context.Context, dir string) (installed string, pushErr, err error) {
	s, err := loadState()
	if err != nil {
		return "", nil, errors.New("there is no tested build to install")
	}
	if Staged(ctx, dir, s.Tag) == "" {
		return "", nil, errors.New("the checkout changed after the tested build; build it again")
	}
	target, err := installedBinary()
	if err != nil {
		return "", nil, err
	}
	if home, err := os.UserHomeDir(); err == nil {
		backups := filepath.Join(home, ".local", "state", "crush", "binary-backups")
		if err := os.MkdirAll(backups, 0o755); err == nil {
			_ = copyFile(target, filepath.Join(backups, "crush-before-"+s.Tag+"-"+time.Now().Format("20060102-150405")))
		}
	}
	// Replace by renaming, so running Crush processes keep their binary.
	next := target + ".update"
	if err := copyFile(s.Binary, next); err != nil {
		return "", nil, err
	}
	if err := os.Rename(next, target); err != nil {
		_ = os.Remove(next)
		return "", nil, err
	}
	_ = os.Remove(statePath())
	if _, err := git(ctx, dir, "push", "--quiet"); err != nil {
		pushErr = fmt.Errorf("couldn't upload to GitHub: %w", err)
	}
	return s.Version, pushErr, nil
}

func installedBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe = strings.TrimSuffix(exe, " (deleted)")
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// FixPrompt asks an AI agent to finish an update that needs judgment:
// resolving clashes, or making a failing build or test suite pass.
func FixPrompt(tag string, clashes []string, failure string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This checkout is a fork of Crush. It is being updated to upstream's stable release %s so the fork gets upstream's changes while keeping every fork feature.\n\n", tag)
	if len(clashes) > 0 {
		fmt.Fprintf(&b, "`git merge %s` stopped with clashes in: %s.\n\n", tag, strings.Join(clashes, ", "))
		b.WriteString("1. Resolve every clash so both the fork's behavior and upstream's change survive. Read both sides and the surrounding code; never drop a fork feature. Then `git add` the files and `git commit --no-edit` to finish the merge.\n")
	} else {
		fmt.Fprintf(&b, "The merge is done, but `crush self-update build` failed:\n\n```\n%s\n```\n\n", lastLines(failure, 60))
		b.WriteString("1. Find the cause and fix it, keeping both the fork's behavior and upstream's change. Commit the fixes.\n")
	}
	b.WriteString("2. Run `crush self-update build`. It builds Crush and runs every test. If anything fails, fix it, commit, and run it again until it passes.\n\n")
	b.WriteString("Never discard work, reset, rebase, force-push or rewrite history, and leave the working tree clean. Do not install or push: Crush asks the user once you are done. End with a short, plain summary of what you changed.")
	return b.String()
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func updateDir() (string, error) {
	dir := filepath.Dir(statePath())
	return dir, os.MkdirAll(dir, 0o755)
}

func statePath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "crush", "update", "state.json")
}

func loadState() (state, error) {
	var s state
	data, err := os.ReadFile(statePath())
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func saveState(s state) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), data, 0o644)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		return text, fmt.Errorf("git %s: %w: %s", args[0], err, lastLines(text, 5))
	}
	return text, nil
}

func run(ctx context.Context, dir string, log io.Writer, args ...string) error {
	var tail bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdout = io.MultiWriter(log, &tail)
	cmd.Stderr = cmd.Stdout
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, lastLines(tail.String(), 80))
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// FixModel is the agent that resolves clashes and failing tests. Set
// CRUSH_SELF_UPDATE_MODEL ("provider/model") to use another one.
const FixModel = "codex-cli/gpt-6.1-sol"

// Fix runs an AI agent in the checkout, headless, until it has resolved the
// clashes or failure and staged a tested build (or given up). Its output is
// written to log.
func Fix(ctx context.Context, dir, tag string, clashes []string, failure string, log io.Writer) error {
	crush, err := installedBinary()
	if err != nil {
		return err
	}
	model := FixModel
	if custom := os.Getenv("CRUSH_SELF_UPDATE_MODEL"); custom != "" {
		model = custom
	}
	args := []string{crush, "run", "--quiet", "--cwd", dir, "--model", model}
	if model == FixModel {
		args = append(args, "--reasoning-effort", "high", "--fast")
	}
	args = append(args, FixPrompt(tag, clashes, failure))
	if err := run(ctx, dir, log, args...); err != nil {
		return fmt.Errorf("the agent stopped: %w", err)
	}
	if Staged(ctx, dir, tag) == "" {
		return errors.New("the agent finished without a passing build")
	}
	return nil
}

// LogPath is where the last update's build, test and agent output goes.
func LogPath() string {
	return filepath.Join(filepath.Dir(statePath()), "update.log")
}

// CreateLog starts a new update log, creating its folder when needed.
func CreateLog() (*os.File, error) {
	if _, err := updateDir(); err != nil {
		return nil, err
	}
	return os.Create(LogPath())
}
