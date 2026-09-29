//go:build linux

package cliagent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/tidwall/jsonc"
	"github.com/tidwall/sjson"
)

// Instruction files have two readers: CLI prompt loaders and ordinary tools.
// Only the former see empty guidance. Git must see the real bytes and metadata,
// or a masked AGENTS.md would look modified and could be staged accidentally.
// Direct I/O prevents the kernel sharing file data between these readers.
type instructionView struct {
	mu     sync.Mutex
	root   fs.Inode
	server *fuse.Server
	dir    string
	native func(context.Context) bool
}

var sharedInstructionView struct {
	sync.Mutex
	view        *instructionView
	executables sync.Map
}

func getInstructionView() (*instructionView, error) {
	sharedInstructionView.Lock()
	defer sharedInstructionView.Unlock()
	// Refresh resolved paths after CLI upgrades, while retaining running old
	// versions until their processes finish.
	for _, name := range []string{"claude", "codex", "grok", "opencode", "agy"} {
		if bin, err := exec.LookPath(name); err == nil {
			if real, err := filepath.EvalSymlinks(bin); err == nil {
				sharedInstructionView.executables.Store(real, true)
			}
		}
	}
	if sharedInstructionView.view != nil {
		return sharedInstructionView.view, nil
	}
	view, err := newInstructionView(func(ctx context.Context) bool {
		caller, ok := fuse.FromContext(ctx)
		if !ok {
			return true
		}
		bin, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", caller.Pid))
		_, native := sharedInstructionView.executables.Load(strings.TrimSuffix(bin, " (deleted)"))
		return err != nil || native
	})
	if err == nil {
		sharedInstructionView.view = view
	}
	return view, err
}

func newInstructionView(native func(context.Context) bool) (*instructionView, error) {
	dir, err := os.MkdirTemp("", "crush-instruction-files-*")
	if err != nil {
		return nil, err
	}
	v := &instructionView{dir: dir, native: native}
	v.server, err = fs.Mount(dir, &v.root, &fs.Options{MountOptions: fuse.MountOptions{FsName: "crush-instructions", Name: "crush-instructions"}})
	if err != nil {
		_ = os.Remove(dir)
		return nil, fmt.Errorf("mount Crush instruction view (requires /dev/fuse and fusermount3): %w", err)
	}
	return v, nil
}

func (v *instructionView) source(path string, config bool) (string, error) {
	var err error
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(path)))
	if v.root.GetChild(key) == nil {
		var stat syscall.Stat_t
		if err := syscall.Stat(path, &stat); err != nil {
			return "", err
		}
		data := &fs.LoopbackRoot{Path: path, Dev: uint64(stat.Dev)}
		data.NewNode = func(root *fs.LoopbackRoot, _ *fs.Inode, _ string, _ *syscall.Stat_t) fs.InodeEmbedder {
			return &instructionNode{LoopbackNode: fs.LoopbackNode{RootData: root}, view: v, config: config}
		}
		node := data.NewNode(data, nil, "", &stat)
		data.RootNode = node
		child := v.root.NewPersistentInode(context.Background(), node, fs.StableAttr{Mode: stat.Mode})
		v.root.AddChild(key, child, true)
	}
	return filepath.Join(v.dir, key), nil
}

func (v *instructionView) close() error {
	if err := v.server.Unmount(); err != nil {
		return err
	}
	return os.Remove(v.dir)
}

// CloseInstructionViews releases the private instruction mount on exit.
func CloseInstructionViews() {
	sharedInstructionView.Lock()
	defer sharedInstructionView.Unlock()
	if sharedInstructionView.view != nil {
		// App exit closes these pipes anyway. Do it before unmounting so idle
		// CLI file watchers can release their references. Saved sessions stay.
		claudeSessions.mu.Lock()
		for _, live := range claudeSessions.m {
			live.p.closeInput()
		}
		claudeSessions.mu.Unlock()
		codexSessions.mu.Lock()
		for _, live := range codexSessions.m {
			live.p.closeInput()
		}
		codexSessions.mu.Unlock()
		view := sharedInstructionView.view
		finished := make(chan struct{})
		go func() { _ = view.close(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(time.Second):
			// A detached command can retain a bind mount. Do not hang quit.
		}
		sharedInstructionView.view = nil
	}
}

type instructionNode struct {
	fs.LoopbackNode
	view   *instructionView
	config bool
}

func (n *instructionNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	native := n.view.native(ctx)
	if native && flags&(syscall.O_ACCMODE|syscall.O_TRUNC) != syscall.O_RDONLY {
		// A CLI must never save its filtered view over the standalone config.
		return nil, 0, syscall.EROFS
	}
	if !native {
		path := filepath.Join(n.RootData.Path, n.Path(n.RootData.RootNode.EmbeddedInode()))
		fd, err := syscall.Open(path, int(flags), 0)
		if err != nil {
			return nil, 0, fs.ToErrno(err)
		}
		return fs.NewLoopbackFile(fd), fuse.FOPEN_DIRECT_IO, 0
	}
	var data []byte
	if n.config {
		var err error
		data, err = os.ReadFile(n.RootData.Path)
		if err == nil {
			data, err = sjson.SetBytes(jsonc.ToJSON(data), "instructions", []string{})
		}
		if err != nil {
			return nil, 0, syscall.EIO
		}
	}
	return &instructionContents{data: data}, fuse.FOPEN_DIRECT_IO, 0
}

func (n *instructionNode) Setattr(ctx context.Context, handle fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	if n.view.native(ctx) {
		return syscall.EROFS
	}
	return n.LoopbackNode.Setattr(ctx, handle, in, out)
}

type instructionContents struct{ data []byte }

func (f *instructionContents) Read(_ context.Context, dest []byte, offset int64) (fuse.ReadResult, syscall.Errno) {
	if offset >= int64(len(f.data)) {
		return fuse.ReadResultData(nil), 0
	}
	end := min(offset+int64(len(dest)), int64(len(f.data)))
	return fuse.ReadResultData(f.data[offset:end]), 0
}
