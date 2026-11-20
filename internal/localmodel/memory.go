package localmodel

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	// ReserveBytes stays free for the user's other apps (owner, 2026-11-20).
	ReserveBytes = 4 << 30
	// overheadBytes covers llama.cpp's compute buffers beside weights and cache.
	overheadBytes = 512 << 20
	// MinContext is the smallest size worth loading and the slider's left end.
	MinContext = 4096
)

// FreeMemory is free graphics memory plus available main memory, in bytes.
// llama.cpp fills the graphics card first and keeps the rest in main memory,
// so the two add up to what a model and its context can use.
func FreeMemory() int64 { return freeVRAM() + availableRAM() }

func availableRAM() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemAvailable:" {
			kib, _ := strconv.ParseInt(fields[1], 10, 64)
			return kib << 10
		}
	}
	return 0
}

// freeVRAM reads AMD cards from sysfs and NVIDIA cards from nvidia-smi.
func freeVRAM() int64 {
	var free int64
	cards, _ := filepath.Glob("/sys/class/drm/card*/device/mem_info_vram_total")
	seen := map[string]bool{}
	for _, totalPath := range cards {
		device, err := filepath.EvalSymlinks(filepath.Dir(totalPath))
		if err != nil || seen[device] {
			continue
		}
		seen[device] = true
		total, used := readNumber(totalPath), readNumber(filepath.Join(filepath.Dir(totalPath), "mem_info_vram_used"))
		if total > used {
			free += total - used
		}
	}
	for _, tool := range []string{"/usr/bin/nvidia-smi", "/usr/local/bin/nvidia-smi"} {
		if _, err := os.Stat(tool); err != nil {
			continue
		}
		out, err := exec.Command(tool, "--query-gpu=memory.free", "--format=csv,noheader,nounits").Output()
		if err != nil {
			break
		}
		for line := range strings.Lines(string(out)) {
			mib, _ := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
			free += mib << 20
		}
		break
	}
	return free
}

func readNumber(path string) int64 {
	data, _ := os.ReadFile(path)
	n, _ := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	return n
}

// FitContext is the longest context, in steps of 1024 tokens, whose cache
// fits in free memory beside the weights while ReserveBytes stays free. It is
// never more than the model was trained for, and 0 when not even MinContext
// fits. A header Crush can't size gets the old fixed size when the weights
// alone fit.
func FitContext(free, weights int64, info Info) int64 {
	if info.TokenBytes <= 0 {
		if free-ReserveBytes-overheadBytes-weights < 0 {
			return 0
		}
		if info.TrainContext > 0 {
			return min(ContextSize, info.TrainContext)
		}
		return ContextSize
	}
	tokens := (free - ReserveBytes - overheadBytes - weights - info.FixedBytes) / info.TokenBytes
	if info.TrainContext > 0 {
		tokens = min(tokens, info.TrainContext)
	}
	tokens = tokens / 1024 * 1024
	if tokens < MinContext {
		return 0
	}
	return tokens
}

// Size is one model's slider: Max is the biggest context that fits now and
// Choice the saved size, 0 meaning "biggest that fits".
type Size struct {
	Max    int64 `json:"max"`
	Choice int64 `json:"choice"`
}

var choicesMu sync.Mutex

func choicesPath() string {
	if dir := os.Getenv("CRUSH_GLOBAL_DATA"); dir != "" {
		return filepath.Join(dir, "local-model-sizes.json")
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "crush", "local-model-sizes.json")
}

// Choices are the saved sizes by model ID; a missing model means biggest.
func Choices() map[string]int64 {
	choicesMu.Lock()
	defer choicesMu.Unlock()
	out := map[string]int64{}
	data, err := os.ReadFile(choicesPath())
	if err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

func saveChoice(id string, tokens int64) error {
	choicesMu.Lock()
	defer choicesMu.Unlock()
	path := choicesPath()
	all := map[string]int64{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	if tokens == 0 {
		delete(all, id)
	} else {
		all[id] = tokens
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
