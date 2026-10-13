package message

import (
	"encoding/json"
	"fmt"
)

type partType string

const (
	reasoningType    partType = "reasoning"
	textType         partType = "text"
	imageURLType     partType = "image_url"
	binaryType       partType = "binary"
	toolCallType     partType = "tool_call"
	toolResultType   partType = "tool_result"
	finishType       partType = "finish"
	shellCommandType partType = "shell_command"
)

type partWrapper struct {
	Type partType    `json:"type"`
	Data ContentPart `json:"data"`
}

func marshalParts(parts []ContentPart) ([]byte, error) {
	wrappedParts := make([]partWrapper, len(parts))

	for i, part := range parts {
		var typ partType

		switch part.(type) {
		case ReasoningContent:
			typ = reasoningType
		case TextContent:
			typ = textType
		case ImageURLContent:
			typ = imageURLType
		case BinaryContent:
			typ = binaryType
		case ToolCall:
			typ = toolCallType
		case ToolResult:
			typ = toolResultType
		case Finish:
			typ = finishType
		case ShellCommand:
			typ = shellCommandType
		default:
			return nil, fmt.Errorf("unknown part type: %T", part)
		}

		wrappedParts[i] = partWrapper{
			Type: typ,
			Data: part,
		}
	}
	return json.Marshal(wrappedParts)
}

// rawPartWrapper is [partWrapper] for decoding: the payload stays encoded
// until the type tag says what to decode it into.
type rawPartWrapper struct {
	Type partType        `json:"type"`
	Data json.RawMessage `json:"data"`
}

func unmarshalParts(data []byte) ([]ContentPart, error) {
	// One pass gets every type tag and its still-encoded payload. Splitting
	// into []json.RawMessage first would walk the same bytes twice.
	var wrapped []rawPartWrapper
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}

	parts := make([]ContentPart, 0, len(wrapped))
	for _, w := range wrapped {
		part, err := unmarshalPart(w.Type, w.Data)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}

	return parts, nil
}

// unmarshalPart decodes a single part's payload according to its type tag.
func unmarshalPart(typ partType, data json.RawMessage) (ContentPart, error) {
	switch typ {
	case reasoningType:
		return decodePart[ReasoningContent](data)
	case textType:
		return decodePart[TextContent](data)
	case imageURLType:
		return decodePart[ImageURLContent](data)
	case binaryType:
		return decodePart[BinaryContent](data)
	case toolCallType:
		return decodePart[ToolCall](data)
	case toolResultType:
		return decodePart[ToolResult](data)
	case finishType:
		return decodePart[Finish](data)
	case shellCommandType:
		return decodePart[ShellCommand](data)
	default:
		return nil, fmt.Errorf("unknown part type: %s", typ)
	}
}

func decodePart[T ContentPart](data json.RawMessage) (ContentPart, error) {
	var part T
	if err := json.Unmarshal(data, &part); err != nil {
		return nil, err
	}
	return part, nil
}
