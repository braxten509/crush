package tools

import (
	"bytes"
	"context"
	"html/template"
	"os/exec"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
)

type (
	sessionIDContextKey string
	messageIDContextKey string
	supportsImagesKey   string
	modelNameKey        string
	chatModelKey        string
	channelContextKey   string
	shellEnvContextKey  string
)

const (
	// SessionIDContextKey is the key for the session ID in the context.
	SessionIDContextKey sessionIDContextKey = "session_id"
	// MessageIDContextKey is the key for the message ID in the context.
	MessageIDContextKey messageIDContextKey = "message_id"
	// SupportsImagesContextKey is the key for the model's image support capability.
	SupportsImagesContextKey supportsImagesKey = "supports_images"
	// ModelNameContextKey is the key for the model name in the context.
	ModelNameContextKey modelNameKey = "model_name"
	// ChatModelContextKey is the key for the model choice the turn's chat
	// runs on ([config.SelectedModel]).
	ChatModelContextKey chatModelKey = "chat_model"
	// ChannelContextKey is the key for the channel that originated the turn.
	ChannelContextKey  channelContextKey  = "channel"
	ShellEnvContextKey shellEnvContextKey = "shell_env"
)

// GetContextValue is a generic helper that retrieves a typed value from context.
// If the value is not found or has the wrong type, it returns the default value.
func GetContextValue[T any](ctx context.Context, key any, defaultValue T) T {
	value := ctx.Value(key)
	if value == nil {
		return defaultValue
	}
	if typedValue, ok := value.(T); ok {
		return typedValue
	}
	return defaultValue
}

// GetSessionFromContext retrieves the session ID from the context.
func GetSessionFromContext(ctx context.Context) string {
	return GetContextValue(ctx, SessionIDContextKey, "")
}

// GetChannelFromContext retrieves the channel that originated the current turn.
func GetChannelFromContext(ctx context.Context) string {
	return GetContextValue(ctx, ChannelContextKey, "")
}

// GetMessageFromContext retrieves the message ID from the context.
func GetMessageFromContext(ctx context.Context) string {
	return GetContextValue(ctx, MessageIDContextKey, "")
}

// GetSupportsImagesFromContext retrieves whether the model supports images from the context.
func GetSupportsImagesFromContext(ctx context.Context) bool {
	return GetContextValue(ctx, SupportsImagesContextKey, false)
}

// GetModelNameFromContext retrieves the model name from the context.
func GetModelNameFromContext(ctx context.Context) string {
	return GetContextValue(ctx, ModelNameContextKey, "")
}

// GetChatModelFromContext retrieves the model choice the turn's chat runs on.
func GetChatModelFromContext(ctx context.Context) config.SelectedModel {
	return GetContextValue(ctx, ChatModelContextKey, config.SelectedModel{})
}

// NewPermissionDeniedResponse returns a tool response indicating the user
// denied permission, with StopTurn set so the agent loop does not retry.
func NewPermissionDeniedResponse() fantasy.ToolResponse {
	resp := fantasy.NewTextErrorResponse("User denied permission")
	resp.StopTurn = true
	return resp
}

// GHAvailable indicates whether the `gh` CLI is available on PATH.
var GHAvailable = func() bool {
	if testing.Testing() {
		return false
	}
	_, err := exec.LookPath("gh")
	return err == nil
}()

// toolDescriptionData is the common data structure for tool description templates.
type toolDescriptionData struct {
	GhAvailable bool
}

// RenderToolDescription renders a tool description template with the given data.
func RenderToolDescription(tmpl *template.Template) string {
	data := toolDescriptionData{
		GhAvailable: GHAvailable,
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		panic("failed to execute tool description template: " + err.Error())
	}
	return out.String()
}

// RenderTemplate renders a Go template with the given data.
func RenderTemplate(tmpl *template.Template, data any) string {
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		panic("failed to execute tool description template: " + err.Error())
	}
	return out.String()
}
