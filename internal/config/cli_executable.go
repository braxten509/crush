package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func findCLI(path, bin string) string {
	return findCLIForPlatform(path, bin, runtime.GOOS, os.Getenv("PATHEXT"))
}

// findCLIForPlatform keeps lookup on the supplied PATH and allows Windows
// executable rules to be tested without running a Windows binary.
func findCLIForPlatform(path, bin, platform, pathExt string) string {
	if path == "" {
		return ""
	}
	dirs := filepath.SplitList(path)
	names := []string{bin}
	if platform == "windows" {
		dirs = strings.Split(path, ";")
		if pathExt == "" {
			pathExt = ".COM;.EXE;.BAT;.CMD"
		}
		if filepath.Ext(bin) == "" {
			names = nil
		}
		for _, ext := range strings.Split(strings.ToLower(pathExt), ";") {
			if strings.HasPrefix(ext, ".") {
				names = append(names, bin+ext)
			}
		}
	}
	for _, dir := range dirs {
		if platform == "windows" {
			dir = strings.Trim(dir, `"`)
		}
		if platform == "windows" && dir == "" {
			continue
		}
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() &&
				(platform == "windows" || info.Mode()&0o111 != 0) {
				if absolute, err := filepath.Abs(candidate); err == nil {
					return absolute
				}
			}
		}
	}
	return ""
}
