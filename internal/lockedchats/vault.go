package lockedchats

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// VaultName is the one Plasma Vault every locked chat shares (owner,
// 2026-11-03).
const VaultName = "Crush locked chats"

// Vault is the encrypted folder locked chats live in. Tests use a plain
// folder behind it; Crush uses KDE's Plasma Vault.
type Vault interface {
	// Find reports whether the vault exists and, when it is open, where.
	Find(ctx context.Context) (exists bool, mount string, err error)
	// Open asks for the password (KDE's own box) and waits until the vault
	// is open, or ctx ends. It returns the mounted folder.
	Open(ctx context.Context) (string, error)
	// OpenWith opens the vault with a password typed in Crush (owner,
	// 2026-11-03: no KDE box to hunt for). ErrWrongPassword when it is
	// wrong. It returns the mounted folder.
	OpenWith(ctx context.Context, password string) (string, error)
	// Close closes the vault. It never forces it closed.
	Close(ctx context.Context) error
	// Create opens KDE's wizard for a new vault and returns at once.
	Create(ctx context.Context) error
}

var (
	// ErrNoVault: the vault doesn't exist yet; the locked page shows how
	// to make it in KDE's wizard.
	ErrNoVault = errors.New("the vault \"" + VaultName + "\" does not exist yet")
	// ErrVaultClosed: the password box was not answered in time, or was
	// cancelled.
	ErrVaultClosed = errors.New("The vault stayed closed") //nolint:staticcheck // shown to the owner as is
	// ErrWrongPassword: the typed vault password is wrong.
	ErrWrongPassword = errors.New("Wrong password") //nolint:staticcheck // shown to the owner as is
)

// openWait is how long Open waits for the password (PLAN §3).
var openWait = 5 * time.Minute

// openPoll is how often Open checks whether the vault has opened. Tests
// shorten it.
var openPoll = 300 * time.Millisecond

// Plasma Vault's DBus service (verified 2026-11-03, PLAN §11).
const (
	vaultService   = "org.kde.kded6"
	vaultPath      = "/modules/plasmavault"
	vaultInterface = "org.kde.plasmavault"
	// vaultOpened is VaultInfo::Status Opened; Closed is 2.
	vaultOpened = 1
)

// vaultDevice is one entry of availableDevices(): a(sssqsasb).
type vaultDevice struct {
	Name       string
	Device     string
	MountPoint string
	Status     uint16
	Message    string
	Activities []string
	Offline    bool
}

// KDEVault drives Plasma Vault over the session bus. Crush's shells run
// with no_new_privs, so it never runs gocryptfs or fusermount itself.
type KDEVault struct{}

// call owns the private connection until the synchronous reply arrives.
// In particular, polling a closed vault must not leave a connection behind
// each time: those connections can exhaust the desktop's D-Bus broker.
func (KDEVault) call(ctx context.Context, method string, args ...any) *dbus.Call {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return &dbus.Call{Err: fmt.Errorf("Plasma Vault is not reachable: %w", err)} //nolint:staticcheck
	}
	defer conn.Close()
	return conn.Object(vaultService, vaultPath).CallWithContext(ctx, vaultInterface+"."+method, 0, args...)
}

func (v KDEVault) device(ctx context.Context) (vaultDevice, bool, error) {
	var devices []vaultDevice
	if err := v.call(ctx, "availableDevices").Store(&devices); err != nil {
		return vaultDevice{}, false, fmt.Errorf("Plasma Vault did not list its vaults: %w", err) //nolint:staticcheck
	}
	for _, d := range devices {
		if d.Name == VaultName {
			return d, true, nil
		}
	}
	return vaultDevice{}, false, nil
}

// mountPoint is where the vault opens: the device entry's, else the one
// in plasmavaultrc, else the place the wizard instructions name.
func (v KDEVault) mountPoint(d vaultDevice) string {
	if d.MountPoint != "" {
		return d.MountPoint
	}
	if mount := rcMountPoint(d.Device); mount != "" {
		return mount
	}
	return DefaultMountPoint()
}

// DefaultMountPoint is the mount point the wizard instructions ask for.
func DefaultMountPoint() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "crush", "locked-chats")
}

// rcMountPoint reads the device's mountPoint from plasmavaultrc.
func rcMountPoint(device string) string {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(config, "plasmavaultrc"))
	if err != nil {
		return ""
	}
	defer f.Close()
	in := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			in = line == "["+device+"]"
			continue
		}
		if in && strings.HasPrefix(line, "mountPoint=") {
			return strings.TrimPrefix(line, "mountPoint=")
		}
	}
	return ""
}

// mounted reports whether a gocryptfs file system is mounted at dir.
func mounted(dir string) bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	want := filepath.Clean(dir)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		dash := -1
		for i, f := range fields {
			if f == "-" {
				dash = i
				break
			}
		}
		if dash < 5 || dash+1 >= len(fields) {
			continue
		}
		if fields[dash+1] == "fuse.gocryptfs" && unescapeMount(fields[4]) == want {
			return true
		}
	}
	return false
}

// unescapeMount undoes mountinfo's octal escapes (\040 for a space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var n int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%o", &n); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (v KDEVault) Find(ctx context.Context) (bool, string, error) {
	d, ok, err := v.device(ctx)
	if err != nil || !ok {
		return false, "", err
	}
	if mount := v.mountPoint(d); mounted(mount) {
		return true, mount, nil
	}
	return true, "", nil
}

func (v KDEVault) Open(ctx context.Context) (string, error) {
	d, ok, err := v.device(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoVault
	}
	mount := v.mountPoint(d)
	if mounted(mount) {
		return mount, nil
	}
	// openVault returns at once while KDE shows its password box.
	if err := v.call(ctx, "openVault", d.Device).Err; err != nil {
		return "", fmt.Errorf("Plasma Vault did not open the vault: %w", err) //nolint:staticcheck
	}
	ctx, cancel := context.WithTimeout(ctx, openWait)
	defer cancel()
	tick := time.NewTicker(openPoll)
	defer tick.Stop()
	for {
		if mounted(mount) {
			return mount, nil
		}
		now, ok, err := v.device(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return "", ErrVaultClosed
			}
			return "", err
		}
		if !ok {
			return "", ErrNoVault
		}
		if now.Status == vaultOpened {
			mount = v.mountPoint(now)
			if mounted(mount) {
				return mount, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ErrVaultClosed
		case <-tick.C:
		}
	}
}

// gocryptfsWrongPassword is gocryptfs's exit code for a wrong password.
const gocryptfsWrongPassword = 12

func (v KDEVault) OpenWith(ctx context.Context, password string) (string, error) {
	d, ok, err := v.device(ctx)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoVault
	}
	mount := v.mountPoint(d)
	if mounted(mount) {
		return mount, nil
	}
	if err := mountGocryptfs(ctx, d.Device, mount, password); err != nil {
		return "", err
	}
	// Let Plasma Vault see it open (its tray and closeVault).
	_ = v.call(ctx, "updateStatus").Err
	return mount, nil
}

// mountGocryptfs opens the gocryptfs folder device at mount with password.
func mountGocryptfs(ctx context.Context, device, mount, password string) error {
	if err := os.MkdirAll(mount, 0o700); err != nil {
		return err
	}
	if err := unlockMountPoint(mount); err != nil {
		return err
	}
	// gocryptfs reads the password from stdin (not a terminal), mounts,
	// and leaves its daemon running. The user's systemd starts it, so it
	// outlives this engine as KDE's own mount does, and the mount works
	// even when Crush runs with no_new_privs (fusermount3 is setuid).
	// KillMode=process keeps the daemon when gocryptfs's first process
	// exits; --pipe hands the password over on stdin.
	args := []string{"gocryptfs", "-q", "--", device, mount}
	if systemdRun, err := exec.LookPath("systemd-run"); err == nil {
		args = append([]string{systemdRun, "--user", "--quiet", "--collect", "--wait", "--pipe",
			"-p", "KillMode=process"}, args...)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == gocryptfsWrongPassword {
			return ErrWrongPassword
		}
		return fmt.Errorf("the vault did not open: %s", strings.TrimSpace(string(out)))
	}
	if !mounted(mount) {
		return errors.New("the vault did not open")
	}
	return nil
}

func (v KDEVault) Close(ctx context.Context) error {
	d, ok, err := v.device(ctx)
	if err != nil || !ok {
		return err
	}
	mount := v.mountPoint(d)
	if !mounted(mount) {
		return nil
	}
	// Close only when nobody uses the folder; without fuser, KDE's own
	// close refuses a busy vault anyway.
	if fuser, err := exec.LookPath("fuser"); err == nil {
		if exec.CommandContext(ctx, fuser, "-m", mount).Run() == nil {
			return nil // in use
		}
	}
	// closeVault, never forceCloseVault (owner, 2026-11-03).
	if err := v.call(ctx, "closeVault", d.Device).Err; err != nil {
		return err
	}
	// A vault Crush opened itself may be one KDE missed; then unmount it
	// the same gentle way (refused while busy).
	for range 10 {
		if !mounted(mount) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return unmountGocryptfs(ctx, mount)
}

// unmountGocryptfs closes a gocryptfs mount the gentle way (refused while
// busy), through the user's systemd for the same reason as the mount.
func unmountGocryptfs(ctx context.Context, mount string) error {
	args := []string{"fusermount3", "-u", mount}
	if systemdRun, err := exec.LookPath("systemd-run"); err == nil {
		args = append([]string{systemdRun, "--user", "--quiet", "--collect", "--wait", "--pipe"}, args...)
	}
	if out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
		return fmt.Errorf("the vault did not close: %s", strings.TrimSpace(string(out)))
	}
	lockMountPoint(mount)
	return nil
}

// kdeMountMarker is the .directory file Plasma Vault writes into a closed
// vault's mount point (folder icon plus this key).
const kdeMountMarker = "X-KDE-LockVaultMountPoint=true"

// unlockMountPoint undoes Plasma Vault's lock on a closed vault's mount
// point, as its own open does: KDE leaves the folder execute-only
// (d--x------) with a .directory marker inside, and gocryptfs refuses
// both ("permission denied", then "not empty").
func unlockMountPoint(mount string) error {
	if err := os.Chmod(mount, 0o700); err != nil {
		return err
	}
	marker := filepath.Join(mount, ".directory")
	if data, err := os.ReadFile(marker); err == nil && strings.Contains(string(data), kdeMountMarker) {
		if err := os.Remove(marker); err != nil {
			return err
		}
	}
	return nil
}

// lockMountPoint puts Plasma Vault's lock back after Crush closed the
// vault itself, so nothing gets written into the bare folder.
func lockMountPoint(mount string) {
	if mounted(mount) {
		return
	}
	_ = os.WriteFile(filepath.Join(mount, ".directory"),
		[]byte("[Desktop Entry]\nIcon=folder-encrypted\n"+kdeMountMarker+"\n"), 0o644)
	_ = os.Chmod(mount, 0o100)
}

func (v KDEVault) Create(ctx context.Context) error {
	return v.call(ctx, "requestNewVault").Err
}

// newVault makes the vault Crush uses; the desktop checks' test seam swaps
// in a DirVault (crushtest builds only).
var newVault = func() Vault { return KDEVault{} }

// NewVault is the vault Crush uses: KDE's Plasma Vault.
func NewVault() Vault { return newVault() }

// SetVaultForTest makes NewVault return v until restore is called.
func SetVaultForTest(v Vault) (restore func()) {
	old := newVault
	newVault = func() Vault { return v }
	return func() { newVault = old }
}

// DirVault is a plain folder standing in for the vault in tests: always
// there and open.
type DirVault struct {
	Dir string
	// Closed makes Open fail as a cancelled password box would.
	Closed bool
	Opens  int
	// Password, when set, is the one OpenWith takes; an open with it
	// clears Closed.
	Password string
}

func (v *DirVault) Find(context.Context) (bool, string, error) {
	if v.Closed {
		return true, "", nil
	}
	return true, v.Dir, nil
}

func (v *DirVault) Open(context.Context) (string, error) {
	v.Opens++
	if v.Closed {
		return "", ErrVaultClosed
	}
	return v.Dir, os.MkdirAll(v.Dir, 0o700)
}

func (v *DirVault) OpenWith(_ context.Context, password string) (string, error) {
	v.Opens++
	if v.Password != "" && password != v.Password {
		return "", ErrWrongPassword
	}
	v.Closed = false
	return v.Dir, os.MkdirAll(v.Dir, 0o700)
}

// Close closes a vault with a password, as KDE's does; without one it
// stays open (the tests' always-open folder).
func (v *DirVault) Close(context.Context) error {
	if v.Password != "" {
		v.Closed = true
	}
	return nil
}
func (v *DirVault) Create(context.Context) error { return nil }
