package config

import (
	"slices"
	"strings"

	"github.com/tidwall/gjson"
)

// CPUSettingChanged reports whether a config file's CPU setting
// (options.agent_cpus_enabled and options.agent_cpus) may read differently
// after an edit than before. Keys are matched as the loader's
// encoding/json decoder matches them, by Unicode case folding
// (strings.EqualFold, which is what encoding/json's foldName implements),
// and each matching spelling counts with its raw value and its place, so
// a change of value, presence or spelling is a change. A file that is not
// a JSON object, before or after, counts as changed.
func CPUSettingChanged(before, after []byte) bool {
	b, okBefore := cpuFingerprint(before)
	a, okAfter := cpuFingerprint(after)
	return !okBefore || !okAfter || !slices.Equal(a, b)
}

// cpuFingerprint lists every options member of data that can carry the
// CPU setting, in document order, with its spelling and raw value.
func cpuFingerprint(data []byte) ([]string, bool) {
	root := gjson.ParseBytes(data)
	if !root.IsObject() {
		return nil, false
	}
	var out []string
	root.ForEach(func(key, value gjson.Result) bool {
		if !strings.EqualFold(key.String(), "options") {
			return true
		}
		if !value.IsObject() {
			out = append(out, key.String()+"="+value.Raw)
			return true
		}
		value.ForEach(func(field, v gjson.Result) bool {
			if strings.EqualFold(field.String(), "agent_cpus_enabled") || strings.EqualFold(field.String(), "agent_cpus") {
				out = append(out, key.String()+"."+field.String()+"="+v.Raw)
			}
			return true
		})
		return true
	})
	return out, true
}
