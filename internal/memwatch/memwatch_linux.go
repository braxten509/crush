//go:build linux

package memwatch

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func readUsage() (Usage, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return Usage{}, err
	}
	defer f.Close()
	return parseMeminfo(f)
}

// topPrograms reads every process's RAM and swap. Processes that end or
// can't be read during the scan are skipped.
func topPrograms(n int) []Program {
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var procs []Program
	for _, d := range dirs {
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		if p, ok := readProcStatus(filepath.Join("/proc", d.Name(), "status")); ok {
			procs = append(procs, p)
		}
	}
	return biggest(procs, n)
}

func readProcStatus(path string) (Program, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Program{}, false
	}
	defer f.Close()
	var p Program
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch name {
		case "Name":
			p.Name = strings.TrimSpace(rest)
		case "VmRSS", "VmSwap":
			if b, err := parseKB(rest); err == nil {
				p.Bytes += b
			}
		}
	}
	return p, p.Name != "" && p.Bytes > 0
}

// ClaimNotify reports whether this Crush may show the desktop pop-up now.
// Every Crush window checks memory on its own, so the stamp file in dir
// lets only one of them show a pop-up per NotifyEvery.
func ClaimNotify(dir string) bool {
	return claimNotify(dir, time.Now(), NotifyEvery)
}

func claimNotify(dir string, now time.Time, every time.Duration) bool {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, "memory-warning"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return false
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:errcheck
	data, _ := os.ReadFile(f.Name())
	if last, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
		if now.Sub(time.Unix(last, 0)) < every {
			return false
		}
	}
	if err := f.Truncate(0); err != nil {
		return false
	}
	_, err = f.WriteAt([]byte(strconv.FormatInt(now.Unix(), 10)), 0)
	return err == nil
}
