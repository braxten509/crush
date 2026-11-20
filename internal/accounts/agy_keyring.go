// Bridges AGY's fixed Secret Service entry to one account-specific entry
// in the desktop keyring. Credentials stay in Secret Service messages.
package accounts

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretName       = "org.freedesktop.secrets"
	secretRoot       = dbus.ObjectPath("/org/freedesktop/secrets")
	secretCollection = secretRoot + "/collection/login"
	secretAlias      = secretRoot + "/aliases/default"
	secretItem       = secretCollection + "/profile"
	noPrompt         = dbus.ObjectPath("/")
	serviceInterface = "org.freedesktop.Secret.Service"
	collectionIface  = "org.freedesktop.Secret.Collection"
	itemInterface    = "org.freedesktop.Secret.Item"
	sessionInterface = "org.freedesktop.Secret.Session"
	propertiesIface  = "org.freedesktop.DBus.Properties"
)

type agyKeyring struct {
	host       *dbus.Conn
	collection dbus.ObjectPath
	username   string
}

func openAGYKeyring(ctx context.Context, dir string) (*agyKeyring, error) {
	username, err := agyKeyringID(dir)
	if err != nil {
		return nil, err
	}
	host, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("AGY keyring: %w", err)
	}
	var collections dbus.Variant
	root := host.Object(secretName, secretRoot)
	err = root.CallWithContext(ctx, propertiesIface+".Get", 0, serviceInterface, "Collections").Store(&collections)
	if err != nil {
		host.Close()
		return nil, fmt.Errorf("AGY keyring: %w", err)
	}
	path := secretAlias
	if paths, ok := collections.Value().([]dbus.ObjectPath); ok && slices.Contains(paths, secretCollection) {
		path = secretCollection
	}
	ring := &agyKeyring{host: host, collection: path, username: username}
	if err := ring.unlocked(ctx, path, collectionIface); err != nil {
		host.Close()
		return nil, err
	}
	return ring, nil
}

func (ring *agyKeyring) unlocked(ctx context.Context, path dbus.ObjectPath, iface string) error {
	var locked dbus.Variant
	err := ring.host.Object(secretName, path).CallWithContext(ctx, propertiesIface+".Get", 0, iface, "Locked").Store(&locked)
	if err != nil {
		return fmt.Errorf("AGY keyring: %w", err)
	}
	if locked.Value() != false {
		return errors.New("unlock your keyring to use this AGY account")
	}
	return nil
}

func (ring *agyKeyring) attributes() map[string]string {
	return map[string]string{"service": "gemini", "username": ring.username}
}

func (ring *agyKeyring) search(ctx context.Context) ([]dbus.ObjectPath, error) {
	var items []dbus.ObjectPath
	err := ring.host.Object(secretName, ring.collection).
		CallWithContext(ctx, collectionIface+".SearchItems", 0, ring.attributes()).Store(&items)
	return items, err
}

func (ring *agyKeyring) loginCount(ctx context.Context) (int, error) {
	items, err := ring.search(ctx)
	if err == nil && len(items) > 0 {
		err = ring.unlocked(ctx, items[0], itemInterface)
	}
	return len(items), err
}

func (ring *agyKeyring) delete(ctx context.Context, path dbus.ObjectPath) error {
	var prompt dbus.ObjectPath
	err := ring.host.Object(secretName, path).CallWithContext(ctx, itemInterface+".Delete", 0).Store(&prompt)
	if err == nil && prompt != noPrompt {
		err = errors.New("unlock your keyring to remove this AGY login")
	}
	return err
}

// agyBusConfig is a bus with no service files: it holds only the keyring
// stand-in, so nothing AGY asks for starts a second copy of a desktop
// service (portals, KWallet, Flatpak) on it.
const agyBusConfig = `<!DOCTYPE busconfig PUBLIC "-//freedesktop//DTD D-Bus Bus Configuration 1.0//EN"
 "http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd">
<busconfig>
  <type>session</type>
  <listen>%s</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow send_destination="*" eavesdrop="true"/>
    <allow eavesdrop="true"/>
    <allow own="*"/>
  </policy>
</busconfig>
`

// startBus returns the private bus's address and its folder, which also
// holds the sandbox's xdg-open (agyOpener).
func (ring *agyKeyring) startBus(ctx context.Context, fail context.CancelCauseFunc) (string, string, func(), error) {
	// The user's runtime folder, not /tmp: temp cleaners leave it alone.
	dir, err := os.MkdirTemp(os.Getenv("XDG_RUNTIME_DIR"), "crush-agy-")
	if err != nil {
		return "", "", nil, err
	}
	address := "unix:path=" + filepath.Join(dir, "bus")
	config := filepath.Join(dir, "bus.conf")
	err = os.WriteFile(config, []byte(fmt.Sprintf(agyBusConfig, html.EscapeString(address))), 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", "", nil, err
	}
	cmd := exec.CommandContext(ctx, "bwrap", "--die-with-parent", "--bind", "/", "/", "--dev-bind", "/dev", "/dev", "--",
		"dbus-daemon", "--config-file="+config, "--nofork", "--print-address=1")
	// The bus's log stays out of the sign-in terminal.
	cmd.Stderr = nil
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", "", nil, err
	}
	stop := func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = os.RemoveAll(dir)
	}
	ready := make(chan bool, 1)
	go func() { ready <- bufio.NewScanner(out).Scan() }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case ok := <-ready:
		if !ok {
			stop()
			return "", "", nil, errors.New("could not start AGY's private keyring bus")
		}
	case <-ctx.Done():
		stop()
		return "", "", nil, ctx.Err()
	case <-timer.C:
		stop()
		return "", "", nil, errors.New("AGY's private keyring bus did not start")
	}
	client, err := dbus.Connect(address)
	if err != nil {
		stop()
		return "", "", nil, err
	}
	closeBus := func() {
		_ = client.Close()
		stop()
	}
	bridge := agySecretService{ring: ring, bus: client, ctx: ctx, fail: fail}
	if err := bridge.export(); err != nil {
		closeBus()
		return "", "", nil, err
	}
	return address, dir, closeBus, nil
}

// agyOpener puts an xdg-open in dir that opens links over the desktop's
// own bus, not the private one, and returns the sandbox's PATH and
// settings for it. AGY opens its sign-in page with xdg-open; on the
// private bus the browser could not be reached.
func agyOpener(dir string) []string {
	open, err := exec.LookPath("xdg-open")
	if err != nil {
		return nil
	}
	desktop := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if desktop == "" {
		desktop = "unix:path=" + filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "bus")
	}
	bin := filepath.Join(dir, "bin")
	script := "#!/bin/sh\nDBUS_SESSION_BUS_ADDRESS=\"$CRUSH_AGY_DESKTOP_BUS\" exec \"$CRUSH_AGY_XDG_OPEN\" \"$@\"\n"
	if os.Mkdir(bin, 0o700) != nil || os.WriteFile(filepath.Join(bin, "xdg-open"), []byte(script), 0o700) != nil {
		return nil
	}
	return []string{"--setenv", "PATH", bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"--setenv", "CRUSH_AGY_DESKTOP_BUS", desktop, "--setenv", "CRUSH_AGY_XDG_OPEN", open}
}

type agySecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

type agySecretService struct {
	ring *agyKeyring
	bus  *dbus.Conn
	ctx  context.Context
	fail context.CancelCauseFunc
}

func (s *agySecretService) export() error {
	for _, endpoint := range []struct {
		path    dbus.ObjectPath
		iface   string
		methods map[string]any
	}{
		{secretRoot, propertiesIface, map[string]any{"Get": s.property}},
		{secretRoot, serviceInterface, map[string]any{"OpenSession": s.openSession, "Unlock": s.unlock}},
		{secretCollection, collectionIface, map[string]any{"SearchItems": s.search, "CreateItem": s.create}},
		{secretAlias, collectionIface, map[string]any{"SearchItems": s.search, "CreateItem": s.create}},
		{secretItem, itemInterface, map[string]any{"GetSecret": s.get, "Delete": s.delete}},
	} {
		if err := s.bus.ExportMethodTable(endpoint.methods, endpoint.path, endpoint.iface); err != nil {
			return err
		}
	}
	reply, err := s.bus.RequestName(secretName, dbus.NameFlagDoNotQueue)
	if err == nil && reply != dbus.RequestNameReplyPrimaryOwner {
		err = errors.New("could not register AGY's private keyring")
	}
	return err
}

func (s *agySecretService) property(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface == serviceInterface && name == "Collections" {
		return dbus.MakeVariant([]dbus.ObjectPath{secretCollection}), nil
	}
	return dbus.Variant{}, dbus.MakeFailedError(errors.New("unknown keyring property"))
}

func (s *agySecretService) openSession(algorithm string, input dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	var output dbus.Variant
	var path dbus.ObjectPath
	err := s.ring.host.Object(secretName, secretRoot).
		CallWithContext(s.ctx, serviceInterface+".OpenSession", 0, algorithm, input).Store(&output, &path)
	if err == nil {
		err = s.bus.ExportMethodTable(map[string]any{"Close": func() *dbus.Error {
			_ = s.bus.Export(nil, path, sessionInterface)
			return agyBusError(s.ring.host.Object(secretName, path).CallWithContext(s.ctx, sessionInterface+".Close", 0).Err)
		}}, path, sessionInterface)
	}
	return output, path, s.keyringError(err)
}

func (s *agySecretService) unlock(paths []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	for _, path := range paths {
		var err error
		switch path {
		case secretCollection, secretAlias:
			err = s.ring.unlocked(s.ctx, s.ring.collection, collectionIface)
		case secretItem:
			_, err = s.ring.loginCount(s.ctx)
		default:
			err = errors.New("unknown AGY keyring item")
		}
		if err != nil {
			return nil, noPrompt, s.keyringError(err)
		}
	}
	return paths, noPrompt, nil
}

func agyLoginAttributes(attrs map[string]string) bool {
	return attrs["service"] == "gemini" && (attrs["username"] == "antigravity" || attrs["username"] == "")
}

func (s *agySecretService) search(attrs map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	if !agyLoginAttributes(attrs) {
		return []dbus.ObjectPath{}, nil
	}
	paths, err := s.ring.search(s.ctx)
	if len(paths) == 0 {
		return []dbus.ObjectPath{}, s.keyringError(err)
	}
	return []dbus.ObjectPath{secretItem}, s.keyringError(err)
}

func (s *agySecretService) create(properties map[string]dbus.Variant, secret agySecret, _ bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	attrs, _ := properties[itemInterface+".Attributes"].Value().(map[string]string)
	if !agyLoginAttributes(attrs) {
		return noPrompt, noPrompt, agyBusError(errors.New("unknown AGY keyring item"))
	}
	props := map[string]dbus.Variant{
		itemInterface + ".Label":      dbus.MakeVariant("Crush Antigravity (" + strings.TrimPrefix(s.ring.username, "crush-agy-") + ")"),
		itemInterface + ".Attributes": dbus.MakeVariant(s.ring.attributes()),
	}
	var path, prompt dbus.ObjectPath
	err := s.ring.host.Object(secretName, s.ring.collection).
		CallWithContext(s.ctx, collectionIface+".CreateItem", 0, props, secret, true).Store(&path, &prompt)
	if err == nil && prompt != noPrompt {
		err = errors.New("unlock your keyring to save this AGY login")
	}
	return secretItem, noPrompt, s.keyringError(err)
}

func (s *agySecretService) get(session dbus.ObjectPath) (agySecret, *dbus.Error) {
	var secret agySecret
	items, err := s.ring.search(s.ctx)
	if err == nil {
		if len(items) == 0 {
			err = errors.New("AGY keyring item no longer exists")
		} else {
			err = s.ring.host.Object(secretName, items[0]).
				CallWithContext(s.ctx, itemInterface+".GetSecret", 0, session).Store(&secret)
		}
	}
	return secret, s.keyringError(err)
}

func (s *agySecretService) delete() (dbus.ObjectPath, *dbus.Error) {
	items, err := s.ring.search(s.ctx)
	if err == nil && len(items) != 0 {
		err = s.ring.delete(s.ctx, items[0])
	}
	return noPrompt, s.keyringError(err)
}

// Stop the CLI if the real keyring fails mid-run. AGY otherwise falls
// back to a local token file and may start spending the wrong login.
func (s *agySecretService) keyringError(err error) *dbus.Error {
	if err != nil {
		s.fail(fmt.Errorf("AGY keyring: %w", err))
	}
	return agyBusError(err)
}

func agyBusError(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbus.MakeFailedError(err)
}
