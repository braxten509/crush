package localmodel

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// header builds a GGUF header from key/value pairs; values are uint32,
// float32, string, []uint32 or []string.
func header(pairs ...any) []byte {
	var b bytes.Buffer
	put := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	text := func(s string) { put(uint64(len(s))); b.WriteString(s) }
	b.WriteString("GGUF")
	put(uint32(3))
	put(uint64(0))
	put(uint64(len(pairs) / 2))
	for i := 0; i < len(pairs); i += 2 {
		text(pairs[i].(string))
		switch v := pairs[i+1].(type) {
		case uint32:
			put(uint32(ggufUint32))
			put(v)
		case float32:
			put(uint32(ggufFloat32))
			put(v)
		case string:
			put(uint32(ggufString))
			text(v)
		case []uint32:
			put(uint32(ggufArray))
			put(uint32(ggufUint32))
			put(uint64(len(v)))
			put(v)
		case []string:
			put(uint32(ggufArray))
			put(uint32(ggufString))
			put(uint64(len(v)))
			for _, s := range v {
				text(s)
			}
		}
	}
	return b.Bytes()
}

// A hybrid model keeps a per-token cache only on every 4th layer, and the
// reader must step over the tokenizer list and float values to reach it.
func TestModelHeaderGivesCacheCostAndEffortSwitch(t *testing.T) {
	data := header(
		"general.architecture", "qwen35",
		"tokenizer.ggml.tokens", []string{"a", "bb", "ccc"},
		"qwen35.block_count", uint32(8),
		"qwen35.context_length", uint32(262144),
		"qwen35.rope.freq_base", float32(1e7),
		"qwen35.attention.head_count", uint32(16),
		"qwen35.attention.head_count_kv", []uint32{2, 2, 2, 2, 2, 2, 2, 2},
		"qwen35.attention.key_length", uint32(128),
		"qwen35.attention.value_length", uint32(128),
		"qwen35.full_attention_interval", uint32(4),
		"tokenizer.chat_template", "{% if enable_thinking %}<think>{% endif %}",
	)
	info, err := readInfo(bytes.NewReader(data))
	require.NoError(t, err)
	fullLayers := int64(2)
	require.Equal(t, fullLayers*2*(128+128)*2, info.TokenBytes)
	require.Equal(t, int64(262144), info.TrainContext)
	require.Equal(t, "thinking", info.Reasoning)

	_, err = readInfo(bytes.NewReader(data[:len(data)-10]))
	require.Error(t, err, "a cut-off header must fail, not size a model from half its fields")
}

func TestFitContextKeepsReserveFreeAndStaysWithinModelMax(t *testing.T) {
	const gib = int64(1) << 30
	info := Info{TokenBytes: 64 << 10, FixedBytes: gib / 2, TrainContext: 1 << 20}
	free, weights := 20*gib, 10*gib
	tokens := FitContext(free, weights, info)
	used := weights + info.FixedBytes + overheadBytes + tokens*info.TokenBytes
	require.LessOrEqual(t, used, free-ReserveBytes)
	require.Greater(t, used+1024*info.TokenBytes, free-ReserveBytes, "should be the biggest size that fits")
	require.Zero(t, tokens%1024)

	info.TrainContext = 32768
	require.Equal(t, int64(32768), FitContext(free, weights, info))
	require.Zero(t, FitContext(weights+ReserveBytes, weights, info), "nothing fits beside the reserve")
}
