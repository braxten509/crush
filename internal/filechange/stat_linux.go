//go:build linux

package filechange

import (
	"io/fs"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func changeTime(info fs.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec)
	}
	return info.ModTime().UnixNano()
}

// kernelFilesystems are virtual filesystems whose files report kernel state
// rather than user content.
var kernelFilesystems = map[int64]bool{
	unix.PROC_SUPER_MAGIC:    true,
	unix.SYSFS_MAGIC:         true,
	unix.CGROUP_SUPER_MAGIC:  true,
	unix.CGROUP2_SUPER_MAGIC: true,
	unix.DEVPTS_SUPER_MAGIC:  true,
	unix.DEBUGFS_MAGIC:       true,
	unix.TRACEFS_MAGIC:       true,
	unix.SECURITYFS_MAGIC:    true,
	unix.BPF_FS_MAGIC:        true,
}

// onKernelFilesystem reports whether path, or its folder when the file does
// not exist yet, lives on a kernel virtual filesystem.
func onKernelFilesystem(path string) bool {
	var stat unix.Statfs_t
	if unix.Statfs(path, &stat) != nil && unix.Statfs(filepath.Dir(path), &stat) != nil {
		return false
	}
	return kernelFilesystems[int64(stat.Type)]
}
