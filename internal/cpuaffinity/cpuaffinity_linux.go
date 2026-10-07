//go:build linux

package cpuaffinity

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	sysCPUDir = "/sys/devices/system/cpu"
	procDir   = "/proc"
)

// Supported reports whether Crush can move processes between CPUs here.
func Supported() bool { return true }

// Apply moves Crush and every process it has started onto cpus (a CPU list
// such as "6-11,18-23"; empty picks them with Detect). It returns the CPU
// list it applied.
func Apply(cpus string) (string, error) {
	set, err := resolve(sysCPUDir, cpus)
	if err != nil {
		return "", err
	}
	if err := pinTree(procDir, os.Getpid(), set); err != nil {
		return "", err
	}
	return FormatList(set), nil
}

// Reset lets Crush and every process it has started use all online CPUs
// again.
func Reset() error {
	online, err := readList(filepath.Join(sysCPUDir, "online"))
	if err != nil {
		return err
	}
	return pinTree(procDir, os.Getpid(), online)
}

// resolve returns the CPUs to use: the user's list (checked against the
// online CPUs) or, when empty, the detected default.
func resolve(sysDir, cpus string) ([]int, error) {
	online, err := readList(filepath.Join(sysDir, "online"))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cpus) == "" {
		return detect(sysDir, online), nil
	}
	set, err := ParseList(cpus)
	if err != nil {
		return nil, err
	}
	for _, cpu := range set {
		if !slices.Contains(online, cpu) {
			return nil, fmt.Errorf("CPU %d is not online (online: %s)", cpu, FormatList(online))
		}
	}
	return set, nil
}

// detect picks the last group of CPUs sharing a last-level (L3) cache, so
// agent work and the desktop don't evict each other's cache. With a single
// cache group it falls back to the upper half of the physical cores, with
// all of their hardware threads.
func detect(sysDir string, online []int) []int {
	if caches := groups(sysDir, online, "cache/index3/shared_cpu_list"); len(caches) >= 2 {
		return caches[len(caches)-1]
	}
	cores := groups(sysDir, online, "topology/thread_siblings_list")
	if len(cores) < 2 {
		return online
	}
	var set []int
	for _, core := range cores[len(cores)/2:] {
		set = append(set, core...)
	}
	slices.Sort(set)
	return set
}

// groups reads one sysfs CPU list per online CPU and returns the distinct
// groups (limited to online CPUs), ordered by their first CPU.
func groups(sysDir string, online []int, file string) [][]int {
	seen := map[string]bool{}
	var out [][]int
	for _, cpu := range online {
		group, err := readList(filepath.Join(sysDir, "cpu"+strconv.Itoa(cpu), file))
		if err != nil {
			continue
		}
		group = slices.DeleteFunc(group, func(c int) bool { return !slices.Contains(online, c) })
		key := FormatList(group)
		if len(group) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, group)
	}
	slices.SortFunc(out, func(a, b []int) int { return a[0] - b[0] })
	return out
}

func readList(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseList(string(data))
}

// pinTree sets the CPUs of every thread of root and of all its descendant
// processes. Only a failure on root itself is an error; other processes may
// exit or belong to someone else.
func pinTree(proc string, root int, cpus []int) error {
	var set unix.CPUSet
	set.Zero()
	for _, cpu := range cpus {
		set.Set(cpu)
	}
	for _, pid := range descendants(proc, root) {
		tasks, _ := os.ReadDir(filepath.Join(proc, strconv.Itoa(pid), "task"))
		for _, task := range tasks {
			tid, err := strconv.Atoi(task.Name())
			if err != nil {
				continue
			}
			if err := unix.SchedSetaffinity(tid, &set); err != nil && pid == root {
				return fmt.Errorf("set CPUs of Crush: %w", err)
			}
		}
	}
	return nil
}

// descendants returns root followed by every process below it.
func descendants(proc string, root int) []int {
	children := map[int][]int{}
	entries, _ := os.ReadDir(proc)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if ppid, ok := parentPID(proc, pid); ok {
			children[ppid] = append(children[ppid], pid)
		}
	}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}

// parentPID reads the parent PID from /proc/<pid>/stat. The command name
// may hold spaces or parentheses, so fields are counted after the last ')'.
func parentPID(proc string, pid int) (int, bool) {
	data, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}
