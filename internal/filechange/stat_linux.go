//go:build linux

package filechange

import (
	"io/fs"
	"syscall"
)

func changeTime(info fs.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec)
	}
	return info.ModTime().UnixNano()
}
