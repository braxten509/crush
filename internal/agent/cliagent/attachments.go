package cliagent

import (
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Saved image copies only let CLIs open images by path; chats keep their
// images in the database. So the folder holds at most attachmentKeep files,
// none unused for longer than attachmentMaxAge.
const (
	attachmentKeep   = 200
	attachmentMaxAge = 14 * 24 * time.Hour
)

// pruneAttachments deletes the attachment folder's files that are too old,
// then the least recently used beyond attachmentKeep. A file's modification
// time is its last use: saveImage touches it when an image is resent.
func pruneAttachments(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		used time.Time
	}
	var files []file
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	slices.SortFunc(files, func(a, b file) int { return b.used.Compare(a.used) })
	for i, f := range files {
		if i >= attachmentKeep || now.Sub(f.used) > attachmentMaxAge {
			_ = os.Remove(f.path)
		}
	}
}
