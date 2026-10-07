//go:build linux

package cpuaffinity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// fakeSys writes a sysfs CPU tree: online CPUs, one L3 group per entry of
// caches and one sibling group per entry of cores.
func fakeSys(t *testing.T, online string, caches, cores []string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, data string) {
		path := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(data+"\n"), 0o644))
	}
	write("online", online)
	for _, groups := range []struct {
		lists []string
		file  string
	}{{caches, "cache/index3/shared_cpu_list"}, {cores, "topology/thread_siblings_list"}} {
		for _, list := range groups.lists {
			cpus, err := ParseList(list)
			require.NoError(t, err)
			for _, cpu := range cpus {
				write(filepath.Join("cpu"+strconv.Itoa(cpu), groups.file), list)
			}
		}
	}
	return dir
}

func TestResolveDetectsLastCacheGroup(t *testing.T) {
	t.Parallel()

	// Two-CCD Ryzen: each cache group holds 6 cores and their SMT siblings.
	sys := fakeSys(t, "0-23", []string{"0-5,12-17", "6-11,18-23"}, nil)
	cpus, err := resolve(sys, "")
	require.NoError(t, err)
	require.Equal(t, "6-11,18-23", FormatList(cpus))
}

func TestResolveSingleCacheTakesUpperHalfOfCores(t *testing.T) {
	t.Parallel()

	// One cache, 4 cores with 2 threads each (siblings n and n+4).
	sys := fakeSys(t, "0-7", []string{"0-7"}, []string{"0,4", "1,5", "2,6", "3,7"})
	cpus, err := resolve(sys, "")
	require.NoError(t, err)
	require.Equal(t, "2-3,6-7", FormatList(cpus))
}

func TestResolveUserList(t *testing.T) {
	t.Parallel()

	sys := fakeSys(t, "0-7", nil, nil)
	cpus, err := resolve(sys, "4-5")
	require.NoError(t, err)
	require.Equal(t, []int{4, 5}, cpus)

	_, err = resolve(sys, "6-9")
	require.ErrorContains(t, err, "not online")
}

func TestParentPIDHandlesOddNames(t *testing.T) {
	t.Parallel()

	proc := t.TempDir()
	stat := func(pid, ppid int, name string) {
		dir := filepath.Join(proc, strconv.Itoa(pid))
		require.NoError(t, os.MkdirAll(dir, 0o755))
		data := fmt.Sprintf("%d (%s) S %d 1 1 0 -1", pid, name, ppid)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte(data), 0o644))
	}
	stat(10, 1, "crush")
	stat(11, 10, "a) S 99 (b")
	stat(12, 11, "node worker")
	stat(13, 1, "other")

	ppid, ok := parentPID(proc, 11)
	require.True(t, ok)
	require.Equal(t, 10, ppid)
	require.ElementsMatch(t, []int{10, 11, 12}, descendants(proc, 10))
}

func TestPinTreeMovesChildProcess(t *testing.T) {
	t.Parallel()

	var current unix.CPUSet
	require.NoError(t, unix.SchedGetaffinity(0, &current))
	var allowed []int
	for cpu := range 1024 {
		if current.IsSet(cpu) {
			allowed = append(allowed, cpu)
		}
	}
	if len(allowed) < 2 {
		t.Skip("needs at least two CPUs")
	}

	child := exec.Command("sleep", "30")
	require.NoError(t, child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	target := allowed[len(allowed)-1:]
	require.NoError(t, pinTree("/proc", child.Process.Pid, target))

	var got unix.CPUSet
	require.NoError(t, unix.SchedGetaffinity(child.Process.Pid, &got))
	require.Equal(t, 1, got.Count())
	require.True(t, got.IsSet(target[0]))
}
