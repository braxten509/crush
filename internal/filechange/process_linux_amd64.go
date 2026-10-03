//go:build linux && amd64

package filechange

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A seccomp filter pauses a command only on the system calls that can change a
// file (and on exec, to match tool calls). Every other call runs at full speed,
// unlike ptrace, which stopped the command on every system call. The command
// is started from a locked thread that holds the filter, so it inherits it
// without a helper program, while Crush's other threads stay unfiltered. The
// thread exits with its goroutine and is never reused.
func startObservedProcess(cmd *exec.Cmd, review *ProcessReview) error {
	if cmd.Err != nil {
		return cmd.Start()
	}
	startKeeper()
	address := goSyscallAddress()
	type outcome struct {
		filtered bool
		err      error
	}
	result := make(chan outcome, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the filtered thread dies here
		listener, err := installMutationFilter(address)
		if address == 0 || err != nil {
			result <- outcome{}
			return
		}
		handOffToKeeper(listener)
		watch := &watcher{listener: listener, review: review, crush: os.Getpid(), processes: map[int]process{}}
		if review.observeRoot {
			watch.rootRecord = review.executed(nil, cmd.Args)
		}
		go watch.serve()
		result <- outcome{filtered: true, err: cmd.Start()}
	}()
	started := <-result
	if !started.filtered {
		// No filter (old kernel or a refused prctl): run without a review.
		return cmd.Start()
	}
	return started.err
}

// installMutationFilter applies the filter to the calling thread only.
func installMutationFilter(address uint64) (int, error) {
	if address == 0 {
		return -1, unix.ENOSYS
	}
	return installFilter(mutationFilter(address))
}

func installFilter(program []unix.SockFilter) (int, error) {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return -1, err
	}
	fprog := unix.SockFprog{Len: uint16(len(program)), Filter: &program[0]}
	listener, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_NEW_LISTENER, uintptr(unsafe.Pointer(&fprog)))
	if errno != 0 {
		return -1, errno
	}
	return int(listener), nil
}

// Write flags that make an open a possible mutation.
const writableOpen = unix.O_WRONLY | unix.O_RDWR | unix.O_CREAT | unix.O_TRUNC

// Go starts a command with vfork: the starting thread can't reach a garbage
// collection safe point until the child's exec returns. If that exec waited for
// Crush, a collection starting meanwhile would deadlock. Every Go system call,
// the child's exec included, runs the same instruction, so the filter lets an
// exec from that address through. Only Crush's own child (or an identical
// binary) runs code there.
var (
	syscallAddressOnce sync.Once
	syscallAddress     uint64
)

func goSyscallAddress() uint64 {
	syscallAddressOnce.Do(func() { syscallAddress = probeSyscallAddress() })
	return syscallAddress
}

// probeSyscallAddress filters getpid on a throwaway thread and reads the
// address the paused call came from. syscall.Syscall releases the processor
// while it waits, so this pause can't block a collection.
func probeSyscallAddress() uint64 {
	type probe struct {
		listener int
		err      error
	}
	ready := make(chan probe, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread dies with its filter
		listener, err := installFilter([]unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: unix.SYS_GETPID, Jf: 1},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_USER_NOTIF},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		})
		ready <- probe{listener, err}
		if err == nil {
			_, _, _ = syscall.Syscall(unix.SYS_GETPID, 0, 0, 0)
		}
	}()
	started := <-ready
	if started.err != nil {
		return 0
	}
	defer unix.Close(started.listener)
	var notification seccompNotification
	for {
		received, done := receive(started.listener, &notification)
		if done {
			return 0
		}
		if received {
			continueCall(started.listener, notification.ID)
			return notification.Data.Instruction
		}
	}
}

// Calls that always notify. Opens and mmap notify only with write flags.
var mutationCalls = []uint32{
	unix.SYS_OPENAT2,
	unix.SYS_CREAT, unix.SYS_TRUNCATE, unix.SYS_FTRUNCATE, unix.SYS_UNLINK, unix.SYS_UNLINKAT,
	unix.SYS_CHMOD, unix.SYS_FCHMOD, unix.SYS_FCHMODAT, unix.SYS_FCHMODAT2,
	unix.SYS_RENAME, unix.SYS_RENAMEAT, unix.SYS_RENAMEAT2,
	unix.SYS_LINK, unix.SYS_LINKAT, unix.SYS_SYMLINK, unix.SYS_SYMLINKAT,
}

// mutationFilter is a classic BPF program over struct seccomp_data: nr at 0,
// arch at 4, the instruction address at 8, the low word of args[i] at 16+8i.
func mutationFilter(address uint64) []unix.SockFilter {
	const (
		load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jeq  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		jge  = unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K
		jset = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
		ret  = unix.BPF_RET | unix.BPF_K
	)
	type step struct {
		code         uint16
		k            uint32
		match, other string // jump labels; "" falls through
		label        string
	}
	argument := func(index int) uint32 { return uint32(16 + 8*index) }
	steps := []step{
		{code: load, k: 4},
		{code: jeq, k: unix.AUDIT_ARCH_X86_64, other: "allow"},
		{code: load, k: 0},
		{code: jge, k: 0x40000000, match: "allow"}, // x32 calls
	}
	for _, call := range mutationCalls {
		steps = append(steps, step{code: jeq, k: call, match: "notify"})
	}
	steps = append(steps,
		step{code: jeq, k: unix.SYS_EXECVE, match: "exec"},
		step{code: jeq, k: unix.SYS_EXECVEAT, match: "exec"},
		step{code: jeq, k: unix.SYS_OPEN, match: "open"},
		step{code: jeq, k: unix.SYS_OPENAT, match: "openat"},
		step{code: jeq, k: unix.SYS_MMAP, match: "mmap"},
		step{code: ret, k: unix.SECCOMP_RET_ALLOW},
		step{label: "exec", code: load, k: 8},
		step{code: jeq, k: uint32(address), other: "notify"},
		step{code: load, k: 12},
		step{code: jeq, k: uint32(address >> 32), match: "allow", other: "notify"},
		step{label: "open", code: load, k: argument(1)},
		step{code: jset, k: writableOpen, match: "notify", other: "allow"},
		step{label: "openat", code: load, k: argument(2)},
		step{code: jset, k: writableOpen, match: "notify", other: "allow"},
		step{label: "mmap", code: load, k: argument(2)},
		step{code: jset, k: unix.PROT_WRITE, other: "allow"},
		step{code: load, k: argument(3)},
		step{code: jset, k: unix.MAP_SHARED, match: "notify", other: "allow"},
		// Jumps only go forward, so the shared returns come last.
		step{label: "allow", code: ret, k: unix.SECCOMP_RET_ALLOW},
		step{label: "notify", code: ret, k: unix.SECCOMP_RET_USER_NOTIF},
	)
	labels := map[string]int{}
	for index, s := range steps {
		if s.label != "" {
			labels[s.label] = index
		}
	}
	jump := func(from int, label string) uint8 {
		if label == "" {
			return 0
		}
		return uint8(labels[label] - from - 1)
	}
	program := make([]unix.SockFilter, len(steps))
	for index, s := range steps {
		program[index] = unix.SockFilter{Code: s.code, K: s.k, Jt: jump(index, s.match), Jf: jump(index, s.other)}
	}
	return program
}

type seccompNotification struct {
	ID    uint64
	Pid   uint32
	Flags uint32
	Data  struct {
		Number       int32
		Architecture uint32
		Instruction  uint64
		Arguments    [6]uint64
	}
}

type seccompResponse struct {
	ID    uint64
	Value int64
	Error int32
	Flags uint32
}

// continueCall lets the paused system call run unchanged.
func continueCall(listener int, id uint64) {
	response := seccompResponse{ID: id, Flags: unix.SECCOMP_USER_NOTIF_FLAG_CONTINUE}
	_, _, _ = unix.Syscall(unix.SYS_IOCTL, uintptr(listener), unix.SECCOMP_IOCTL_NOTIF_SEND, uintptr(unsafe.Pointer(&response)))
}

// receive waits for the next paused call. It reports false once no process
// uses the filter any more.
func receive(listener int, notification *seccompNotification) (received, done bool) {
	descriptors := []unix.PollFd{{Fd: int32(listener), Events: unix.POLLIN}}
	if _, err := unix.Poll(descriptors, -1); err != nil {
		return false, err != unix.EINTR
	}
	if descriptors[0].Revents&unix.POLLIN == 0 {
		return false, descriptors[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0
	}
	*notification = seccompNotification{}
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(listener), unix.SECCOMP_IOCTL_NOTIF_RECV, uintptr(unsafe.Pointer(notification)))
	return errno == 0, false
}

type process struct {
	start  string
	record *invocation
}

// watcher answers one filter's paused calls. Only its goroutine touches it.
type watcher struct {
	listener   int
	review     *ProcessReview
	crush      int
	root       int
	rootRecord *invocation
	processes  map[int]process
	threads    map[int]*watchedThread
	tick       uint64
}

func (w *watcher) serve() {
	defer unix.Close(w.listener)
	defer func() {
		for id := range w.threads {
			w.dropThread(id)
		}
	}()
	var notification seccompNotification
	for {
		received, done := receive(w.listener, &notification)
		if done {
			return
		}
		if !received {
			continue
		}
		w.handle(&notification)
		continueCall(w.listener, notification.ID)
	}
}

func (w *watcher) handle(notification *seccompNotification) {
	thread := int(notification.Pid)
	cached := w.thread(thread)
	if cached == nil {
		return
	}
	pid := cached.pid
	target := tracee{thread: thread, memory: cached.memory}
	number, args := uint64(notification.Data.Number), notification.Data.Arguments
	if number == unix.SYS_EXECVE || number == unix.SYS_EXECVEAT {
		path, argv := target.path(unix.AT_FDCWD, args[0]), args[1]
		if number == unix.SYS_EXECVEAT {
			path, argv = target.path(int64(int32(args[0])), args[1]), args[2]
			if args[4]&unix.AT_EMPTY_PATH != 0 {
				path = ""
			}
		}
		// Shells try each PATH folder; record only an exec that can succeed.
		if path != "" && unix.Access(path, unix.X_OK) != nil {
			return
		}
		record := w.review.executed(cached.record, target.strings(argv))
		w.processes[pid] = process{start: startTime(pid), record: record}
		w.dropProcess(pid)
		return
	}
	record := cached.record
	paths := mutationPaths(target, number, args)
	for _, path := range paths {
		w.review.before(record, path)
	}
	if (number == unix.SYS_RENAME || number == unix.SYS_RENAMEAT || number == unix.SYS_RENAMEAT2) && len(paths) == 2 {
		w.review.moving(record, paths[0], paths[1])
	}
}

// record finds the invocation a process belongs to: its own exec, or its
// nearest ancestor's. Forked children that never exec inherit their parent's.
func (w *watcher) record(pid int) *invocation {
	for current, depth := pid, 0; current > 1 && current != w.crush && depth < 128; depth++ {
		if known, ok := w.processes[current]; ok && known.start == startTime(current) {
			return known.record
		}
		parent := parentOf(current)
		if current == w.root || parent == w.crush { // the command Crush started
			w.root = current
			return w.rootRecord
		}
		current = parent
	}
	return nil
}

func procStat(pid int) []string {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil
	}
	// The command name may hold spaces and parentheses; fields follow the last ')'.
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return nil
	}
	return strings.Fields(string(data[end+1:]))
}

func parentOf(pid int) int {
	if fields := procStat(pid); len(fields) > 1 {
		parent, _ := strconv.Atoi(fields[1])
		return parent
	}
	return 0
}

func startTime(pid int) string {
	if fields := procStat(pid); len(fields) > 19 {
		return fields[19]
	}
	return ""
}

func threadGroup(thread int) int {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(thread) + "/status")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "Tgid:"); ok {
			pid, _ := strconv.Atoi(strings.TrimSpace(value))
			return pid
		}
	}
	return 0
}

// tracee reads a paused thread's memory and file table.
type tracee struct {
	thread int
	memory *os.File
}

const maxString = 1 << 20 // exec arguments can hold whole scripts

func (t tracee) string(address uint64) string {
	if address == 0 {
		return ""
	}
	var output []byte
	block := make([]byte, 4096)
	for len(output) < maxString {
		n, _ := t.memory.ReadAt(block, int64(address)+int64(len(output)))
		if n <= 0 {
			break
		}
		if end := bytes.IndexByte(block[:n], 0); end >= 0 {
			return string(append(output, block[:end]...))
		}
		output = append(output, block[:n]...)
	}
	return ""
}

func (t tracee) strings(address uint64) []string {
	var values []string
	pointer := make([]byte, 8)
	for index := 0; address != 0 && index < 4096; index++ {
		if n, _ := t.memory.ReadAt(pointer, int64(address)+int64(8*index)); n != 8 {
			break
		}
		value := binary.LittleEndian.Uint64(pointer)
		if value == 0 {
			break
		}
		values = append(values, t.string(value))
	}
	return values
}

func (t tracee) path(descriptor int64, address uint64) string {
	path := t.string(address)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	base := fmt.Sprintf("/proc/%d/cwd", t.thread)
	if descriptor != unix.AT_FDCWD {
		base = fmt.Sprintf("/proc/%d/fd/%d", t.thread, descriptor)
	}
	root, err := os.Readlink(base)
	if err != nil {
		return ""
	}
	return filepath.Join(root, path)
}

func (t tracee) descriptor(descriptor uint64) string {
	path, _ := os.Readlink("/proc/" + strconv.Itoa(t.thread) + "/fd/" + strconv.FormatUint(descriptor, 10))
	return path
}

// mutationPaths covers the calls mutationFilter notifies about. Writes through
// a descriptor need no pause: the open that made it writable was captured.
func mutationPaths(t tracee, number uint64, args [6]uint64) []string {
	path := func(index int) string { return t.path(unix.AT_FDCWD, args[index]) }
	at := func(descriptor, index int) string { return t.path(int64(int32(args[descriptor])), args[index]) }
	writable := func(flags uint64) bool { return flags&writableOpen != 0 }
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
		flags := make([]byte, 8)
		if n, _ := t.memory.ReadAt(flags, int64(args[2])); n == 8 && writable(binary.LittleEndian.Uint64(flags)) {
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
	case unix.SYS_FTRUNCATE, unix.SYS_FCHMOD:
		return []string{t.descriptor(args[0])}
	case unix.SYS_MMAP:
		if args[2]&unix.PROT_WRITE != 0 && args[3]&unix.MAP_SHARED != 0 {
			return []string{t.descriptor(args[4])}
		}
	}
	return nil
}

// The keeper is a small copy of Crush that holds every filter's listener. If
// Crush quits or crashes, programs a command left running (build daemons, dev
// servers) would otherwise get ENOSYS from every filtered call; the keeper
// lets their calls run until none is left.
const keeperArgument = "__crush-file-watch-keeper"

var (
	keeperOnce   sync.Once
	keeperSocket = -1
)

func init() {
	if len(os.Args) == 2 && os.Args[1] == keeperArgument {
		keep(3)
		os.Exit(0)
	}
}

func startKeeper() {
	keeperOnce.Do(func() {
		pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return
		}
		end := os.NewFile(uintptr(pair[1]), "keeper")
		defer end.Close()
		cmd := exec.Command("/proc/self/exe", keeperArgument)
		cmd.Env = []string{}
		cmd.ExtraFiles = []*os.File{end}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			unix.Close(pair[0])
			return
		}
		go func() { _ = cmd.Wait() }()
		keeperSocket = pair[0]
	})
}

func handOffToKeeper(listener int) {
	if keeperSocket >= 0 {
		_ = unix.Sendmsg(keeperSocket, []byte{0}, unix.UnixRights(listener), nil, 0)
	}
}

// keep holds listeners while Crush runs, and answers them once it is gone.
func keep(socket int) {
	crushRunning := true
	var listeners []int
	for crushRunning || len(listeners) > 0 {
		descriptors := []unix.PollFd{}
		if crushRunning {
			descriptors = append(descriptors, unix.PollFd{Fd: int32(socket), Events: unix.POLLIN})
		}
		for _, listener := range listeners {
			events := int16(0) // only hang-ups while Crush answers
			if !crushRunning {
				events = unix.POLLIN
			}
			descriptors = append(descriptors, unix.PollFd{Fd: int32(listener), Events: events})
		}
		if _, err := unix.Poll(descriptors, -1); err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		var added []int
		if crushRunning {
			if descriptors[0].Revents != 0 {
				message, control := make([]byte, 1), make([]byte, unix.CmsgSpace(4*8))
				n, controlLength, _, _, err := unix.Recvmsg(socket, message, control, 0)
				if err != nil || (n == 0 && controlLength == 0) {
					crushRunning = false
					unix.Close(socket)
				} else if messages, err := unix.ParseSocketControlMessage(control[:controlLength]); err == nil {
					for _, m := range messages {
						if fds, err := unix.ParseUnixRights(&m); err == nil {
							added = append(added, fds...)
						}
					}
				}
			}
			descriptors = descriptors[1:]
		}
		var open []int
		for index, listener := range listeners {
			revents := descriptors[index].Revents
			if revents&unix.POLLIN != 0 && !crushRunning {
				var notification seccompNotification
				if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(listener), unix.SECCOMP_IOCTL_NOTIF_RECV, uintptr(unsafe.Pointer(&notification))); errno == 0 {
					continueCall(listener, notification.ID)
				}
			}
			if revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 && revents&unix.POLLIN == 0 {
				unix.Close(listener)
				continue
			}
			open = append(open, listener)
		}
		listeners = append(open, added...)
	}
}
