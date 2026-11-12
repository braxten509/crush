//go:build linux

package lockedchats

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stretchr/testify/require"
)

// privateVaultBus has no service directories: tests cannot activate KDE or
// touch a real vault, even if a method or destination name is wrong.
func privateVaultBus(t *testing.T, methods map[string]any) *dbus.Conn {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for the private vault bus")
	}
	config := filepath.Join(t.TempDir(), "bus.conf")
	require.NoError(t, os.WriteFile(config, []byte(`<busconfig>
  <type>session</type>
  <listen>unix:tmpdir=/tmp</listen>
  <auth>EXTERNAL</auth>
  <policy context="default">
    <allow user="*"/>
    <allow own="*"/>
    <allow send_destination="*"/>
    <allow receive_sender="*"/>
  </policy>
</busconfig>`), 0o600))
	cmd := exec.Command(daemon, "--config-file="+config, "--nofork", "--nopidfile", "--print-address=1")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})
	address, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err, "private D-Bus startup")
	address = strings.TrimSpace(address)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", address)
	conn, err := dbus.Connect(address)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	reply, err := conn.RequestName(vaultService, dbus.NameFlagDoNotQueue)
	require.NoError(t, err)
	require.Equal(t, dbus.RequestNameReplyPrimaryOwner, reply)
	require.NoError(t, conn.ExportMethodTable(methods, vaultPath, vaultInterface))
	return conn
}

func busConnections(t *testing.T, conn *dbus.Conn) int {
	t.Helper()
	var names []string
	require.NoError(t, conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names))
	count := 0
	for _, name := range names {
		if strings.HasPrefix(name, ":") {
			count++
		}
	}
	return count
}

func TestKDEVaultReleasesConnections(t *testing.T) {
	for _, name := range []string{"find", "missing", "method error", "create", "open error", "open polling"} {
		t.Run(name, func(t *testing.T) {
			var polls atomic.Int32
			device := vaultDevice{
				Name: VaultName, Device: "/unused/encrypted-vault",
				MountPoint: t.TempDir(), Status: 2, Activities: []string{},
			}
			conn := privateVaultBus(t, map[string]any{
				"availableDevices": func() ([]vaultDevice, *dbus.Error) {
					polls.Add(1)
					if name == "method error" {
						return nil, dbus.MakeFailedError(errors.New("vault unavailable"))
					}
					if name == "missing" {
						return []vaultDevice{}, nil
					}
					return []vaultDevice{device}, nil
				},
				"openVault": func(string) *dbus.Error {
					if name == "open error" {
						return dbus.MakeFailedError(errors.New("opening failed"))
					}
					return nil
				},
				"requestNewVault": func() *dbus.Error { return nil },
			})
			before := busConnections(t, conn)
			vault := KDEVault{}
			if name == "open polling" {
				previous := openPoll
				openPoll = 10 * time.Millisecond
				t.Cleanup(func() { openPoll = previous })
				ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
				defer cancel()
				_, err := vault.Open(ctx)
				require.ErrorIs(t, err, ErrVaultClosed)
				require.GreaterOrEqual(t, polls.Load(), int32(3), "exercise repeated closed-vault polling")
			} else {
				for range 20 {
					switch name {
					case "create":
						require.NoError(t, vault.Create(t.Context()))
					case "open error":
						_, err := vault.Open(t.Context())
						require.ErrorContains(t, err, "opening failed")
					default:
						exists, mount, err := vault.Find(t.Context())
						if name == "method error" {
							require.ErrorContains(t, err, "vault unavailable")
						} else {
							require.NoError(t, err)
							require.Equal(t, name == "find", exists)
							require.Empty(t, mount)
						}
					}
				}
			}
			// The broker processes the closed sockets asynchronously.
			require.Eventually(t, func() bool { return busConnections(t, conn) == before },
				time.Second, 10*time.Millisecond, "vault requests leaked D-Bus connections")
		})
	}
}

func TestKDEVaultReleasesCanceledCall(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	conn := privateVaultBus(t, map[string]any{
		"availableDevices": func() ([]vaultDevice, *dbus.Error) {
			close(started)
			<-release
			return []vaultDevice{}, nil
		},
	})
	defer close(release)
	before := busConnections(t, conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := (KDEVault{}).Find(ctx)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("vault request did not reach the private bus")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("vault request did not stop after cancellation")
	}
	require.Eventually(t, func() bool { return busConnections(t, conn) == before },
		time.Second, 10*time.Millisecond, "canceled vault request leaked its D-Bus connection")
}

func TestKDEVaultOpenReportsPollingFailure(t *testing.T) {
	for _, name := range []string{"unreachable", "removed"} {
		t.Run(name, func(t *testing.T) {
			var polls atomic.Int32
			device := vaultDevice{
				Name: VaultName, Device: "/unused/encrypted-vault",
				MountPoint: t.TempDir(), Status: 2, Activities: []string{},
			}
			privateVaultBus(t, map[string]any{
				"availableDevices": func() ([]vaultDevice, *dbus.Error) {
					if polls.Add(1) == 1 {
						return []vaultDevice{device}, nil
					}
					if name == "unreachable" {
						return nil, dbus.MakeFailedError(errors.New("vault unavailable"))
					}
					return []vaultDevice{}, nil
				},
				"openVault": func(string) *dbus.Error { return nil },
			})
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := (KDEVault{}).Open(ctx)
			if name == "unreachable" {
				require.ErrorContains(t, err, "vault unavailable")
			} else {
				require.ErrorIs(t, err, ErrNoVault)
			}
		})
	}
}
