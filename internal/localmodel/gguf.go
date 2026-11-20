package localmodel

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Info is what Crush needs from a model file's header: how much memory one
// token of context costs, the longest context the model was trained for, and
// which reasoning switch its chat template reads.
type Info struct {
	// TokenBytes is the f16 KV cache size of one token across every layer.
	TokenBytes int64
	// FixedBytes is cache that doesn't grow with the context: the
	// sliding-window layers, which keep only their window.
	FixedBytes int64
	// TrainContext is the model's own longest context (0 when unknown).
	TrainContext int64
	// Reasoning is "effort" when the template reads reasoning_effort,
	// "thinking" when it reads enable_thinking, and "" otherwise.
	Reasoning string
	// Efforts are the reasoning_effort values the template accepts, in
	// rising order, and DefaultEffort the one it uses when none is sent.
	// Both are empty when the template doesn't name them.
	Efforts       []string
	DefaultEffort string
}

type infoKey struct {
	path string
	size int64
	time time.Time
}

var (
	infoMu    sync.Mutex
	infoCache = map[infoKey]Info{}
)

// ModelInfo reads a model's header once per file version; the tokenizer lists
// make the header several megabytes, too much to read on every status poll.
func ModelInfo(path string) (Info, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	key := infoKey{path, stat.Size(), stat.ModTime()}
	infoMu.Lock()
	info, ok := infoCache[key]
	infoMu.Unlock()
	if ok {
		return info, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer file.Close()
	info, err = readInfo(bufio.NewReaderSize(file, 1<<20))
	if err != nil {
		return Info{}, err
	}
	infoMu.Lock()
	infoCache[key] = info
	infoMu.Unlock()
	return info, nil
}

// GGUF metadata value types.
const (
	ggufUint8 = iota
	ggufInt8
	ggufUint16
	ggufInt16
	ggufUint32
	ggufInt32
	ggufFloat32
	ggufBool
	ggufString
	ggufArray
	ggufUint64
	ggufInt64
	ggufFloat64
)

type ggufReader struct {
	r   io.Reader
	err error
}

func (g *ggufReader) read(size int) []byte {
	if g.err != nil {
		return make([]byte, size)
	}
	buf := make([]byte, size)
	_, g.err = io.ReadFull(g.r, buf)
	return buf
}

func (g *ggufReader) u32() uint32 { return binary.LittleEndian.Uint32(g.read(4)) }
func (g *ggufReader) u64() uint64 { return binary.LittleEndian.Uint64(g.read(8)) }

func (g *ggufReader) str() string {
	n := g.u64()
	if g.err == nil && n > 64<<20 {
		g.err = fmt.Errorf("model header string is too long")
	}
	if g.err != nil {
		return ""
	}
	return string(g.read(int(n)))
}

// value reads one value. Numbers come back as int64, arrays of numbers as
// []int64, strings as string; other arrays are skipped and come back nil.
func (g *ggufReader) value(kind uint32) any {
	switch kind {
	case ggufUint8, ggufInt8, ggufBool:
		b := g.read(1)[0]
		if kind == ggufInt8 {
			return int64(int8(b))
		}
		return int64(b)
	case ggufUint16, ggufInt16:
		v := binary.LittleEndian.Uint16(g.read(2))
		if kind == ggufInt16 {
			return int64(int16(v))
		}
		return int64(v)
	case ggufUint32, ggufFloat32:
		v := g.u32()
		if kind == ggufFloat32 {
			return nil
		}
		return int64(v)
	case ggufInt32:
		return int64(int32(g.u32()))
	case ggufUint64, ggufInt64:
		return int64(g.u64())
	case ggufFloat64:
		g.u64()
		return nil
	case ggufString:
		return g.str()
	case ggufArray:
		inner := g.u32()
		count := g.u64()
		if g.err == nil && count > 1<<26 {
			g.err = fmt.Errorf("model header array is too long")
		}
		var numbers []int64
		for i := uint64(0); i < count && g.err == nil; i++ {
			if inner == ggufString {
				g.str()
				continue
			}
			if n, ok := g.value(inner).(int64); ok {
				numbers = append(numbers, n)
			}
		}
		if inner == ggufString {
			return nil
		}
		return numbers
	}
	if g.err == nil {
		g.err = fmt.Errorf("unknown model header value type %d", kind)
	}
	return nil
}

func readInfo(r io.Reader) (Info, error) {
	g := &ggufReader{r: r}
	if string(g.read(4)) != "GGUF" {
		return Info{}, fmt.Errorf("not a GGUF file")
	}
	if version := g.u32(); g.err == nil && version < 2 {
		return Info{}, fmt.Errorf("GGUF version %d is too old", version)
	}
	g.u64() // tensor count
	count := g.u64()
	values := map[string]any{}
	for i := uint64(0); i < count && g.err == nil; i++ {
		key := g.str()
		values[key] = g.value(g.u32())
	}
	if g.err != nil {
		return Info{}, g.err
	}
	return infoFrom(values), nil
}

// infoFrom works out the KV cache size the way llama.cpp lays it out: per
// layer, K and V rows for every KV head, in f16. Recurrent layers of hybrid
// models (every layer but each full_attention_interval-th, plus the MTP
// layers) keep no per-token cache; sliding-window layers keep only their
// window; MLA models store one compressed row. Anything unknown is counted
// as a full layer, which only overestimates.
func infoFrom(values map[string]any) Info {
	arch, _ := values["general.architecture"].(string)
	number := func(name string) int64 {
		n, _ := values[arch+"."+name].(int64)
		return n
	}
	layers := number("block_count")
	perLayer := func(name string) []int64 {
		switch v := values[arch+"."+name].(type) {
		case int64:
			out := make([]int64, layers)
			for i := range out {
				out[i] = v
			}
			return out
		case []int64:
			return v
		}
		return nil
	}
	heads := perLayer("attention.head_count")
	kvHeads := perLayer("attention.head_count_kv")
	if kvHeads == nil {
		kvHeads = heads
	}
	window := number("attention.sliding_window")
	// A per-layer list marks each sliding layer. A single number n is a period,
	// as llama.cpp reads it: every n-th layer is global, the rest slide. Gemma
	// 2 and 3 headers leave it out; llama.cpp hard-codes 2 and 6.
	var pattern []int64
	period := int64(0)
	switch v := values[arch+".attention.sliding_window_pattern"].(type) {
	case []int64:
		pattern = v
	case int64:
		period = v
	}
	if pattern == nil && period == 0 && window > 0 {
		period = map[string]int64{"gemma2": 2, "gemma3": 6}[arch]
	}
	if pattern == nil && period > 1 {
		pattern = make([]int64, layers)
		for i := range pattern {
			if (int64(i)+1)%period != 0 {
				pattern[i] = 1
			}
		}
	}
	interval := number("full_attention_interval")
	mtpFrom := layers - number("nextn_predict_layers")
	embedding := number("embedding_length")
	keyLength, valueLength := number("attention.key_length"), number("attention.value_length")
	info := Info{TrainContext: number("context_length")}
	if rank := number("attention.kv_lora_rank"); rank > 0 {
		info.TokenBytes = layers * (rank + number("rope.dimension_count")) * 2
	} else {
		for i, kv := range kvHeads {
			if kv <= 0 || (interval > 0 && (i+1)%int(interval) != 0 && int64(i) < mtpFrom) {
				continue
			}
			k, v := keyLength, valueLength
			if (k == 0 || v == 0) && i < len(heads) && heads[i] > 0 {
				k, v = max(k, embedding/heads[i]), max(v, embedding/heads[i])
			}
			if window > 0 && i < len(pattern) && pattern[i] != 0 {
				if swaK, swaV := number("attention.key_length_swa"), number("attention.value_length_swa"); swaK > 0 && swaV > 0 {
					k, v = swaK, swaV
				}
				// The window plus one 512-token batch, as llama.cpp sizes it.
				info.FixedBytes += kv * (k + v) * 2 * (window + 512)
				continue
			}
			info.TokenBytes += kv * (k + v) * 2
		}
	}
	template, _ := values["tokenizer.chat_template"].(string)
	reasoning := ""
	switch {
	case strings.Contains(template, "reasoning_effort"):
		reasoning = "effort"
	case strings.Contains(template, "enable_thinking"):
		reasoning = "thinking"
	}
	info.Reasoning = reasoning
	if reasoning == "effort" {
		info.Efforts, info.DefaultEffort = templateEfforts(template)
	}
	return info
}

var (
	effortQuoted = regexp.MustCompile(`'([a-z]+)'|"([a-z]+)"`)
	effortAlias  = regexp.MustCompile(`set\s+(\w+)\s*=.*\breasoning_effort\b`)
	// effortBranch is an == test on any effort variable: the levels the
	// template actually behaves differently for.
	effortBranch = regexp.MustCompile(`[\w.]*effort[\w.]*\s*==\s*['"]([a-z]+)['"]`)
	// effortDefault is the level used when none is sent.
	effortDefault = regexp.MustCompile(`reasoning_effort\s*\|\s*default\(\s*['"]([a-z]+)['"]|set\s+\w*default\w*effort\w*\s*=\s*['"]([a-z]+)['"]`)
	effortOrder   = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
)

// templateEfforts reads the reasoning_effort values a chat template accepts,
// so a model whose template takes low/medium/xhigh gets exactly those. A
// template that rejects other values ("not in (...)") is taken at its word;
// one that accepts anything gets the levels it branches on plus its default,
// which leaves out synonyms it folds into the same level.
func templateEfforts(template string) ([]string, string) {
	names := []string{"reasoning_effort"}
	for _, m := range effortAlias.FindAllStringSubmatch(template, -1) {
		names = append(names, regexp.QuoteMeta(m[1]))
	}
	strict := regexp.MustCompile(`\b(?:` + strings.Join(names, "|") + `)\b\s*not\s+in\s*[\(\[]([^\)\]]*)[\)\]]`)
	var levels []string
	add := func(level string) {
		if level != "" && !slices.Contains(levels, level) {
			levels = append(levels, level)
		}
	}
	if m := strict.FindStringSubmatch(template); m != nil {
		for _, q := range effortQuoted.FindAllStringSubmatch(m[1], -1) {
			add(q[1] + q[2])
		}
	} else {
		for _, m := range effortBranch.FindAllStringSubmatch(template, -1) {
			add(m[1])
		}
	}
	fallback := ""
	if m := effortDefault.FindStringSubmatch(template); m != nil {
		fallback = m[1] + m[2]
		if len(levels) > 0 {
			add(fallback)
		}
	}
	if !slices.Contains(levels, fallback) {
		fallback = ""
	}
	rank := func(level string) int {
		if i := slices.Index(effortOrder, level); i >= 0 {
			return i
		}
		return len(effortOrder)
	}
	slices.SortStableFunc(levels, func(a, b string) int { return rank(a) - rank(b) })
	return levels, fallback
}
