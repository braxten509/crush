// Package localmodel discovers downloaded GGUF weights and owns the llama.cpp
// process Crush starts for them. Discovery never loads weights or opens a vault.
package localmodel

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const ProviderID = "local-gguf"
const ContextSize = 32768

type Model struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Path  string `json:"-"`
}

func Roots() []string {
	if dirs := os.Getenv("CRUSH_LOCAL_MODEL_DIRS"); dirs != "" {
		return filepath.SplitList(dirs)
	}
	home, _ := os.UserHomeDir()
	cache, _ := os.UserCacheDir()
	return []string{filepath.Join(home, "models"), filepath.Join(cache, "huggingface", "hub"),
		filepath.Join(home, ".lmstudio", "models"), filepath.Join(cache, "lm-studio", "models"),
		filepath.Join(home, ".unsloth", "llama.cpp", "models")}
}

func outsideVault(path string) bool {
	parts := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
	for i := 1; i < len(parts); i++ {
		if parts[i-1] == ".unsloth" && parts[i] == "studio" {
			return false
		}
	}
	return true
}

func safePath(path string) (string, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil || !outsideVault(absolute) {
		return "", false
	}
	real, err := filepath.EvalSymlinks(absolute)
	return real, err == nil && outsideVault(real)
}

var splitName = regexp.MustCompile(`(?i)^(.+)-(\d{5})-of-(\d{5})\.gguf$`)

// Discover returns complete model files, deduplicating aliases and joining
// split weights. Paths stay private; public IDs are stable opaque hashes.
func Discover(roots []string) []Model {
	out := []Model{}
	seen := []fs.FileInfo{}
	for _, root := range roots {
		root, ok := safePath(root)
		if !ok {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if !outsideVault(path) || entry.Name() == ".git" || entry.Name() == ".locks" {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".gguf") || strings.Contains(lower, "mmproj") || strings.HasPrefix(lower, "ggml-vocab") {
				return nil
			}
			real, ok := safePath(path)
			if !ok {
				return nil
			}
			info, err := os.Stat(real)
			if err != nil || !info.Mode().IsRegular() || slices.ContainsFunc(seen, func(old fs.FileInfo) bool { return os.SameFile(old, info) }) {
				return nil
			}
			file, err := os.Open(real)
			if err != nil {
				return nil
			}
			var magic [4]byte
			_, err = io.ReadFull(file, magic[:])
			file.Close()
			if err != nil || string(magic[:]) != "GGUF" {
				return nil
			}
			size := info.Size()
			if match := splitName.FindStringSubmatch(name); match != nil {
				if match[2] != "00001" {
					return nil
				}
				count, _ := strconv.Atoi(match[3])
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil || count < 1 || count > len(entries) {
					return nil
				}
				size = 0
				for i := 1; i <= count; i++ {
					part, ok := safePath(filepath.Join(filepath.Dir(path), fmt.Sprintf("%s-%05d-of-%05d.gguf", match[1], i, count)))
					if !ok {
						return nil
					}
					partInfo, err := os.Stat(part)
					if err != nil || !partInfo.Mode().IsRegular() {
						return nil
					}
					size += partInfo.Size()
				}
			}
			seen = append(seen, info)
			hash := sha256.Sum256([]byte(real))
			out = append(out, Model{ID: fmt.Sprintf("gguf-%x", hash[:12]), Name: strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return -1
				}
				return r
			}, name), Bytes: size, Path: real})
			return nil
		})
	}
	slices.SortFunc(out, func(a, b Model) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

func ServerExecutable() (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{os.Getenv("CRUSH_LLAMA_SERVER"), filepath.Join(home, ".local", "bin", "llama-server"), filepath.Join(home, ".unsloth", "llama.cpp", "llama-server"), "/usr/bin/llama-server", "/usr/local/bin/llama-server"}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, ok := safePath(candidate)
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("llama-server is not installed; set CRUSH_LLAMA_SERVER to its executable")
}
