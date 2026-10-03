package skills

import "golang.org/x/sys/unix"

// Never replace a destination created by another process between validation
// and publication, including an empty directory or dangling symlink.
func publishSkillDir(stage, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
