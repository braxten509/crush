//go:build !linux

package filechange

import "io/fs"

func changeTime(info fs.FileInfo) int64 { return info.ModTime().UnixNano() }
