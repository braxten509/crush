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
)

// openWait is how long Open waits for the password (PLAN §3).
var openWait = 5 * time.Minute

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

func (KDEVault) object() (dbus.BusObject, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("Plasma Vault is not reachable: %w", err) //nolint:staticcheck
	}
	return conn.Object(vaultService, vaultPath), nil
}

func (v KDEVault) device(ctx context.Context) (vaultDevice, bool, error) {
	obj, err := v.object()
	if err != nil {
		return vaultDevice{}, false, err
	}
	var devices []vaultDevice
	if err := obj.CallWithContext(ctx, vaultInterface+".availableDevices", 0).Store(&devices); err != nil {
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
	obj, err := v.object()
	if err != nil {
		return "", err
	}
	// openVault returns at once while KDE shows its password box.
	if err := obj.CallWithContext(ctx, vaultInterface+".openVault", 0, d.Device).Err; err != nil {
		return "", fmt.Errorf("Plasma Vault did not open the vault: %w", err) //nolint:staticcheck
	}
	ctx, cancel := context.WithTimeout(ctx, openWait)
	defer cancel()
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	for {
		if mounted(mount) {
			return mount, nil
		}
		if now, ok, err := v.device(ctx); err == nil && ok && now.Status == vaultOpened {
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
	obj, err := v.object()
	if err != nil {
		return err
	}
	// closeVault, never forceCloseVault (owner, 2026-11-03).
	return obj.CallWithContext(ctx, vaultInterface+".closeVault", 0, d.Device).Err
}

func (v KDEVault) Create(ctx context.Context) error {
	obj, err := v.object()
	if err != nil {
		return err
	}
	return obj.CallWithContext(ctx, vaultInterface+".requestNewVault", 0).Err
}

// DirVault is a plain folder standing in for the vault in tests: always
// there and open.
type DirVault struct {
	Dir string
	// Closed makes Open fail as a cancelled password box would.
	Closed bool
	Opens  int
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

func (v *DirVault) Close(context.Context) error  { return nil }
func (v *DirVault) Create(context.Context) error { return nil }
