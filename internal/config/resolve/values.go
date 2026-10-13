package resolve

import (
	"fmt"
	"slices"
)

// Args expands every element of args through r into a fresh slice; args
// is never mutated. Empty results are kept. On the first failure it
// returns nil and an error naming the positional index, wrapping the
// (already sanitized) resolver error.
func Args(args []string, r VariableResolver) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]string, len(args))
	for i, a := range args {
		v, err := r.ResolveValue(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// EnvList expands every value of envs through r and returns a fresh
// "KEY=value" slice sorted by key; envs is never mutated. On the first
// failure it returns nil and an error naming the variable.
func EnvList(envs map[string]string, r VariableResolver) ([]string, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	res := make([]string, 0, len(envs))
	for _, k := range sortedKeys(envs) {
		v, err := r.ResolveValue(envs[k])
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", k, err)
		}
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res, nil
}

// EnvMap expands every value of envs through r into a fresh map; envs is
// never mutated. Empty results are kept. Keys are resolved in sorted order
// so the first failure, returned as an error naming the quoted key, is
// deterministic.
func EnvMap(envs map[string]string, r VariableResolver) (map[string]string, error) {
	if len(envs) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(envs))
	for _, k := range sortedKeys(envs) {
		v, err := r.ResolveValue(envs[k])
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// Headers expands every header value through r into a fresh map; headers
// is never mutated. A header that resolves to the empty string is
// omitted. Names are resolved in sorted order so the first failure,
// returned as an error naming the header, is deterministic.
func Headers(headers map[string]string, r VariableResolver) (map[string]string, error) {
	if len(headers) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(headers))
	for _, k := range sortedKeys(headers) {
		v, err := r.ResolveValue(headers[k])
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", k, err)
		}
		if v == "" {
			continue
		}
		out[k] = v
	}
	return out, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
