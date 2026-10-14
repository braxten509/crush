//go:build linux

package memlimit

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

type unitProperty struct {
	Name  string
	Value dbus.Variant
}

type auxUnit struct {
	Name       string
	Properties []unitProperty
}

func systemd() (dbus.BusObject, *dbus.Conn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to the user's systemd: %w", err)
	}
	return conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1"), conn, nil
}

func ownCgroup() string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	return cgroupPath(bytes.NewReader(data))
}

// Join moves this process into its own scope in slice, so it and every
// program it starts from now on count toward the slice's limit. A process
// already in the slice stays where it is.
func Join(slice string) error {
	if slicePath(ownCgroup(), slice) != "" {
		return nil
	}
	obj, conn, err := systemd()
	if err != nil {
		return err
	}
	defer conn.Close()
	pid := os.Getpid()
	props := []unitProperty{
		{"Description", dbus.MakeVariant("Crush")},
		{"PIDs", dbus.MakeVariant([]uint32{uint32(pid)})},
		{"Slice", dbus.MakeVariant(slice)},
		// Without this, systemd stops the whole scope, Crush included,
		// when Linux stops one program in it for using too much memory.
		{"OOMPolicy", dbus.MakeVariant("continue")},
		{"CollectMode", dbus.MakeVariant("inactive-or-failed")},
	}
	name := fmt.Sprintf("crush-%d.scope", pid)
	var job dbus.ObjectPath
	if err := obj.Call("org.freedesktop.systemd1.Manager.StartTransientUnit", 0, name, "fail", props, []auxUnit{}).Store(&job); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	// The move happens in systemd's job; wait for it so programs started
	// next are born inside the box.
	for range 100 {
		if slicePath(ownCgroup(), slice) != "" {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("timed out moving Crush into " + slice)
}

// Apply sets slice's limit, in bytes of RAM and swap together; 0 removes
// it. The change reaches every program in the slice at once and lasts
// until the next reboot; each Crush applies the saved limit when it starts.
func Apply(slice string, limit uint64) error {
	ram, swap, limited := Split(limit)
	if !limited {
		ram, swap = math.MaxUint64, math.MaxUint64
	}
	obj, conn, err := systemd()
	if err != nil {
		return err
	}
	defer conn.Close()
	props := []unitProperty{
		{"MemoryMax", dbus.MakeVariant(ram)},
		{"MemorySwapMax", dbus.MakeVariant(swap)},
	}
	if err := obj.Call("org.freedesktop.systemd1.Manager.SetUnitProperties", 0, slice, true, props).Err; err != nil {
		return fmt.Errorf("setting the limit of %s: %w", slice, err)
	}
	return nil
}

// Usage is how much RAM and swap the slice this process is in uses now.
// ok is false when the process isn't in slice.
func Usage(slice string) (used uint64, ok bool) {
	path := slicePath(ownCgroup(), slice)
	if path == "" {
		return 0, false
	}
	dir := filepath.Join("/sys/fs/cgroup", path)
	for _, f := range []string{"memory.current", "memory.swap.current"} {
		data, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return 0, false
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
		if err != nil {
			return 0, false
		}
		used += n
	}
	return used, true
}
