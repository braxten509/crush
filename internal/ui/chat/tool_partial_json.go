package chat

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// partialJSONStringValue matches the raw remainder of a partial JSON
// object after the decoder stopped, when the value of the pending key
// is an in-flight string, e.g. `: "hel`.
var partialJSONStringValue = regexp.MustCompile(`^\s*:\s*"((?:[^"\\]|\\.)*\\?)$`)

// partialJSONFields extracts every complete top-level scalar field of a
// JSON object that may be cut off mid-stream, e.g. while a tool call
// input is still being streamed. The value currently streaming is also
// recovered as far as it is unambiguous: a string value is included
// while it grows (a cut escape at its end is dropped), while partial
// keys, numbers, booleans, and nested values are omitted. Nested
// objects and arrays are skipped: tool parameter schemas are flat.
func partialJSONFields(input string) map[string]any {
	fields := make(map[string]any)
	dec := json.NewDecoder(strings.NewReader(input))
	var key string
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
			} else {
				depth--
				if depth == 1 {
					// A nested value of the top-level object just closed;
					// drop its dangling key so the next key/value pair stays
					// in sync.
					key = ""
				}
			}
		case string:
			if depth != 1 {
				continue
			}
			if key == "" {
				key = v
			} else {
				fields[key] = v
				key = ""
			}
		default:
			if depth == 1 && key != "" {
				fields[key] = v
				key = ""
			}
		}
	}
	if key != "" && depth == 1 {
		offset := min(int(dec.InputOffset()), len(input))
		if m := partialJSONStringValue.FindStringSubmatch(input[offset:]); m != nil {
			fields[key] = unescapePartialString(m[1])
		}
	}
	return fields
}

// unescapePartialString unescapes a string value cut off mid-stream,
// dropping a trailing incomplete escape such as a lone backslash or a
// short \u sequence. Unescaping is best-effort: the value is a prefix
// of the final one and self-corrects as more input arrives.
func unescapePartialString(s string) string {
	// JSON allows \/ where Go string syntax does not.
	s = strings.ReplaceAll(s, `\/`, `/`)
	for {
		if v, err := strconv.Unquote(`"` + s + `"`); err == nil {
			return v
		}
		idx := strings.LastIndexByte(s, '\\')
		if idx < 0 || len(s)-idx > len(`\uXXXX`) {
			// No escape to trim: the failure is not a cut tail, so the
			// raw prefix is the best display we have.
			return s
		}
		s = s[:idx]
	}
}
