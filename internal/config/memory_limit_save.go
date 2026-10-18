package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/crush/internal/lock"
	"github.com/charmbracelet/crush/internal/memlimit"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The memory-limit dialog's parse, save and apply, shared with the engine's
// memory-limit route (engine-design.md 5.9 c). The terminal keeps its own
// copy until phase 7.

// memoryLimitKey is where the limit is saved in the global config.
const memoryLimitKey = "options.memory_limit_gb"

// memoryLimitApply puts a limit on the box. Tests replace it
// (SetMemoryLimitApplyForTest); unreplaced, it refuses inside a test so no
// test ever changes the box the user's own Crush windows run in.
var memoryLimitApply struct {
	sync.Mutex
	fn func(slice string, limit uint64) error
}

// ParseMemoryLimit reads a limit as typed in the memory-limit dialog: "",
// "0", "8", "8 GB", "7.5". Empty is 0, no limit. Its errors are the
// dialog's texts.
func ParseMemoryLimit(text string) (float64, error) {
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ToUpper(text)), "GB"))
	if value == "" {
		return 0, nil
	}
	gb, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("Enter a number of GB, or leave it empty for no limit")
	}
	if err := ValidateMemoryLimitGB(gb); err != nil {
		return 0, err
	}
	return gb, nil
}

// SaveMemoryLimit does what the terminal's memory-limit dialog does: it
// parses value as typed (ParseMemoryLimit), saves the limit to the global
// config under the lock every Crush shares, then puts it on the box every
// Crush window shares. err is a parse or save failure, and nothing was
// applied; applyErr is a limit that was saved but could not be applied.
func SaveMemoryLimit(value string) (limitBytes uint64, applyErr error, err error) {
	gb, err := ParseMemoryLimit(value)
	if err != nil {
		return 0, nil, err
	}
	if err := saveGlobalField(memoryLimitKey, gb); err != nil {
		return 0, nil, err
	}
	limitBytes = GBToBytes(gb)
	memoryLimitApply.Lock()
	apply := memoryLimitApply.fn
	memoryLimitApply.Unlock()
	if apply == nil {
		apply = applyMemoryLimit
	}
	return limitBytes, apply(memlimit.Slice, limitBytes), nil
}

func applyMemoryLimit(slice string, limit uint64) error {
	if testing.Testing() {
		return errors.New("tests never change the memory box")
	}
	return memlimit.Apply(slice, limit)
}

// SetMemoryLimitApplyForTest makes SaveMemoryLimit apply limits with fn
// until restore runs.
func SetMemoryLimitApplyForTest(fn func(slice string, limit uint64) error) (restore func()) {
	memoryLimitApply.Lock()
	defer memoryLimitApply.Unlock()
	saved := memoryLimitApply.fn
	memoryLimitApply.fn = fn
	return func() {
		memoryLimitApply.Lock()
		defer memoryLimitApply.Unlock()
		memoryLimitApply.fn = saved
	}
}

// SavedMemoryLimitGB reads the limit last saved to the global config. ok is
// false when none was saved there.
func SavedMemoryLimitGB() (gb float64, ok bool) {
	data, err := readFile(GlobalConfigData())
	if err != nil {
		return 0, false
	}
	v := gjson.GetBytes(data, memoryLimitKey)
	if !v.Exists() || v.Type != gjson.Number {
		return 0, false
	}
	return v.Float(), true
}

// saveGlobalField sets key in the global config file, as
// SetConfigField(ScopeGlobal, …) does, under the cross-process lock.
func saveGlobalField(key string, value any) error {
	path := GlobalConfigData()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), configLockDeadline)
	defer cancel()
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		return fmt.Errorf("acquire config lock: %w", err)
	}
	defer release()
	data, err := readFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read config file: %w", err)
		}
		data = []byte("{}")
	}
	updated, err := sjson.SetBytes(data, key, value)
	if err != nil {
		return fmt.Errorf("failed to set config field %s: %w", key, err)
	}
	return atomicWriteFile(path, updated, 0o600)
}
