//go:build linux && amd64

package filechange

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A dedicated locked thread owns ptrace and waits only for its own tracees.
// The root is detached at its exit stop so exec.Cmd.Wait retains ownership of
// its exit status. Child command exits still reach their original CLI parent.
func startObservedProcess(cmd *exec.Cmd, review *ProcessReview) error {
	started := make(chan error, 1)
	review.detached = make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		detached := false
		defer func() {
			if !detached {
				close(review.detached)
			}
		}()
		if cmd.SysProcAttr == nil {
			cmd.SysProcAttr = &syscall.SysProcAttr{}
		}
		cmd.SysProcAttr.Ptrace = true
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		root := cmd.Process.Pid
		var status syscall.WaitStatus
		if _, err := syscall.Wait4(root, &status, 0, nil); err != nil {
			started <- err
			return
		}
		options := syscall.PTRACE_O_TRACEFORK | syscall.PTRACE_O_TRACEVFORK | syscall.PTRACE_O_TRACECLONE | syscall.PTRACE_O_TRACEEXEC | syscall.PTRACE_O_TRACEEXIT | syscall.PTRACE_O_TRACESYSGOOD
		if err := syscall.PtraceSetOptions(root, options); err != nil {
			_ = syscall.PtraceDetach(root)
			started <- err
			return
		}
		calls := map[int]*invocation{root: nil}
		if review.observeRoot {
			calls[root] = review.executed(nil, cmd.Args)
		}
		resume := func(pid int, signal int) {
			if calls[pid] != nil {
				_ = syscall.PtraceSyscall(pid, signal)
			} else {
				_ = syscall.PtraceCont(pid, signal)
			}
		}
		resume(root, 0)
		started <- nil
		for len(calls) > 0 {
			pid, err := syscall.Wait4(-1, &status, syscall.WALL|syscall.WNOTHREAD, nil)
			if err == syscall.EINTR {
				continue
			}
			if err != nil {
				break
			}
			if status.Exited() || status.Signaled() {
				delete(calls, pid)
				continue
			}
			if !status.Stopped() {
				continue
			}
			signal := status.StopSignal()
			switch status.TrapCause() {
			case syscall.PTRACE_EVENT_FORK, syscall.PTRACE_EVENT_VFORK, syscall.PTRACE_EVENT_CLONE:
				child, err := syscall.PtraceGetEventMsg(pid)
				if err == nil {
					calls[int(child)] = calls[pid]
				}
				signal = 0
			case syscall.PTRACE_EVENT_EXEC:
				previous, _ := syscall.PtraceGetEventMsg(pid)
				if int(previous) != pid && previous != 0 {
					calls[pid] = calls[int(previous)]
					delete(calls, int(previous))
				}
				if pid != root || review.observeRoot {
					data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
					calls[pid] = review.executed(calls[pid], strings.Split(strings.TrimRight(string(data), "\x00"), "\x00"))
				}
				signal = 0
			case syscall.PTRACE_EVENT_EXIT:
				if pid == root {
					_ = syscall.PtraceDetach(pid)
					close(review.detached)
					detached = true
					delete(calls, pid)
					continue
				}
				signal = 0
			default:
				if signal == syscall.SIGTRAP|0x80 {
					var info syscallInfo
					_, _, err := syscall.Syscall6(syscall.SYS_PTRACE, unix.PTRACE_GET_SYSCALL_INFO, uintptr(pid), unsafe.Sizeof(info), uintptr(unsafe.Pointer(&info)), 0, 0)
					if err == 0 && info.Operation == 1 { // syscall entry, before truncation/writes
						for _, path := range mutationPaths(pid, info.Number, info.Arguments) {
							review.before(calls[pid], path)
						}
					}
					signal = 0
				} else if signal == syscall.SIGSTOP { // initial stop of a newly traced child
					signal = 0
				}
			}
			resume(pid, int(signal))
		}
	}()
	return <-started
}

type syscallInfo struct {
	Operation          uint8
	Padding            [3]uint8
	Architecture       uint32
	InstructionPointer uint64
	StackPointer       uint64
	Number             uint64
	Arguments          [6]uint64
}

func traceString(pid int, address uint64) string {
	var output []byte
	for len(output) < 4096 {
		block := make([]byte, 64)
		n, err := syscall.PtracePeekData(pid, uintptr(address)+uintptr(len(output)), block)
		if n > 0 {
			if end := bytes.IndexByte(block[:n], 0); end >= 0 {
				return string(append(output, block[:end]...))
			}
			output = append(output, block[:n]...)
		}
		if err != nil || n == 0 {
			break
		}
	}
	return ""
}

func tracePath(pid int, descriptor int64, address uint64) string {
	path := traceString(pid, address)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	base := fmt.Sprintf("/proc/%d/cwd", pid)
	if descriptor != unix.AT_FDCWD {
		base = fmt.Sprintf("/proc/%d/fd/%d", pid, descriptor)
	}
	root, err := os.Readlink(base)
	if err != nil {
		return ""
	}
	return filepath.Join(root, path)
}

func descriptorPath(pid int, descriptor uint64) string {
	path, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/fd/" + strconv.FormatUint(descriptor, 10))
	return path
}

func mutationPaths(pid int, number uint64, args [6]uint64) []string {
	path := func(index int) string { return tracePath(pid, unix.AT_FDCWD, args[index]) }
	at := func(descriptor, index int) string { return tracePath(pid, int64(int32(args[descriptor])), args[index]) }
	writable := func(flags uint64) bool { return flags&(unix.O_WRONLY|unix.O_RDWR|unix.O_CREAT|unix.O_TRUNC) != 0 }
	switch number {
	case unix.SYS_OPEN:
		if writable(args[1]) {
			return []string{path(0)}
		}
	case unix.SYS_OPENAT:
		if writable(args[2]) {
			return []string{at(0, 1)}
		}
	case unix.SYS_OPENAT2:
		var flags uint64
		buffer := unsafe.Slice((*byte)(unsafe.Pointer(&flags)), 8)
		if _, err := syscall.PtracePeekData(pid, uintptr(args[2]), buffer); err == nil && writable(flags) {
			return []string{at(0, 1)}
		}
	case unix.SYS_CREAT, unix.SYS_TRUNCATE, unix.SYS_UNLINK, unix.SYS_CHMOD:
		return []string{path(0)}
	case unix.SYS_UNLINKAT, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2:
		return []string{at(0, 1)}
	case unix.SYS_RENAME:
		return []string{path(0), path(1)}
	case unix.SYS_RENAMEAT, unix.SYS_RENAMEAT2:
		return []string{at(0, 1), at(2, 3)}
	case unix.SYS_LINK, unix.SYS_SYMLINK:
		return []string{path(1)}
	case unix.SYS_LINKAT:
		return []string{at(2, 3)}
	case unix.SYS_SYMLINKAT:
		return []string{at(1, 2)}
	case unix.SYS_WRITE, unix.SYS_WRITEV, unix.SYS_PWRITE64, unix.SYS_PWRITEV, unix.SYS_PWRITEV2, unix.SYS_FTRUNCATE, unix.SYS_FCHMOD:
		return []string{descriptorPath(pid, args[0])}
	case unix.SYS_COPY_FILE_RANGE:
		return []string{descriptorPath(pid, args[2])}
	case unix.SYS_SENDFILE:
		return []string{descriptorPath(pid, args[0])}
	case unix.SYS_MMAP:
		if args[2]&unix.PROT_WRITE != 0 && args[3]&unix.MAP_SHARED != 0 {
			return []string{descriptorPath(pid, args[4])}
		}
	}
	return nil
}
