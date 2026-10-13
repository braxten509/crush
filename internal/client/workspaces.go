package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/proto"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/rpc"
)

// ListWorkspaces retrieves all workspaces from the server.
func (c *Client) ListWorkspaces(ctx context.Context) ([]proto.Workspace, error) {
	return newCall(c, rpc.ListWorkspaces, "list workspaces", "workspaces").get(ctx)
}

// CreateWorkspace creates a new workspace on the server.
func (c *Client) CreateWorkspace(ctx context.Context, ws proto.Workspace) (*proto.Workspace, error) {
	ws.ClientID = c.clientID
	created, err := newCall(c, rpc.CreateWorkspace, "create workspace", "workspace").do(ctx, ws)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// GetWorkspace retrieves a workspace from the server.
func (c *Client) GetWorkspace(ctx context.Context, id string) (*proto.Workspace, error) {
	ws, err := newCall(c, rpc.GetWorkspace, "get workspace", "workspace").at(id).get(ctx)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// DeleteWorkspace releases this client's creation hold on a workspace.
func (c *Client) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.DeleteWorkspace, "delete workspace", "").
		at(id).with(url.Values{"client_id": []string{c.clientID}}).get(ctx)
	return err
}

// SetCurrentSession reports the client's current-session selection
// for the named workspace. An empty sessionID clears the entry. The
// request carries the process-scoped client ID minted in [NewClient]
// as a query parameter so the server can route the update to the
// correct client entry.
func (c *Client) SetCurrentSession(ctx context.Context, workspaceID, sessionID string) error {
	_, err := newCall(c, rpc.SetCurrentSession, "set current session", "").
		at(workspaceID).with(url.Values{"client_id": []string{c.clientID}}).
		do(ctx, proto.CurrentSession{SessionID: sessionID})
	return err
}

// GetConfig retrieves the workspace-specific configuration.
func (c *Client) GetConfig(ctx context.Context, id string) (*config.Config, error) {
	return newCall(c, rpc.WorkspaceConfig, "get config", "config").at(id).get(ctx)
}

// GitBranch returns the current Git branch of the workspace's working
// directory as reported by the server. It returns an empty string when
// the directory is not a Git repository or HEAD is detached.
func (c *Client) GitBranch(ctx context.Context, id string) (string, error) {
	result, err := newCall(c, rpc.GitBranch, "get git branch", "git branch response").at(id).get(ctx)
	return result.Branch, err
}

// ProjectNeedsInitialization checks if the project needs
// initialization.
func (c *Client) ProjectNeedsInitialization(ctx context.Context, id string) (bool, error) {
	result, err := newCall(c, rpc.ProjectNeedsInit, "check project init", "project init response").at(id).get(ctx)
	return result.NeedsInit, err
}

// MarkProjectInitialized marks the project as initialized on the
// server.
func (c *Client) MarkProjectInitialized(ctx context.Context, id string) error {
	_, err := newCall(c, rpc.ProjectInit, "mark project initialized", "").at(id).get(ctx)
	return err
}

// GetInitializePrompt retrieves the initialization prompt from the
// server.
func (c *Client) GetInitializePrompt(ctx context.Context, id string) (string, error) {
	result, err := newCall(c, rpc.ProjectInitPrompt, "get init prompt", "init prompt response").at(id).get(ctx)
	return result.Prompt, err
}

// SubscribeEvents subscribes to server-sent events for a workspace.
func (c *Client) SubscribeEvents(ctx context.Context, id string) (<-chan any, error) {
	events := make(chan any, 100)
	q := url.Values{"client_id": []string{c.clientID}}
	//nolint:bodyclose
	rsp, err := c.get(ctx, strings.TrimPrefix(rpc.WorkspaceEvents.URL(id), apiPrefix), q, http.Header{
		"Accept":        []string{"text/event-stream"},
		"Cache-Control": []string{"no-cache"},
		"Connection":    []string{"keep-alive"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to events: %w", err)
	}

	if err := checkStatus(rsp); err != nil {
		rsp.Body.Close()
		return nil, fmt.Errorf("failed to subscribe to events: %w", err)
	}

	go func() {
		defer rsp.Body.Close()
		defer close(events)
		readEvents(ctx, rsp.Body, events)
	}()

	return events, nil
}

// readEvents decodes the SSE stream into typed events until the stream
// ends or ctx is cancelled.
func readEvents(ctx context.Context, body io.Reader, events chan any) {
	scr := bufio.NewReader(body)
	for {
		line, err := scr.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("Reading from events stream", "error", err)
			select {
			case <-time.After(time.Second * 2):
			case <-ctx.Done():
				return
			}
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		data, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			slog.Warn("Invalid event format", "line", string(line))
			continue
		}

		var p pubsub.Payload
		if err := json.Unmarshal(bytes.TrimSpace(data), &p); err != nil {
			slog.Error("Unmarshaling event envelope", "error", err)
			continue
		}

		ev, ok := decodeEvent(p)
		if !ok {
			slog.Warn("Unknown event type", "type", p.Type)
			continue
		}
		if !sendEvent(ctx, events, ev) {
			return
		}
	}
}

// eventDecoders maps each SSE payload type to the decoder of its typed
// pubsub event.
var eventDecoders = map[pubsub.PayloadType]func(json.RawMessage) any{
	pubsub.PayloadTypeLSPEvent:               decodeEventAs[proto.LSPEvent],
	pubsub.PayloadTypeMCPEvent:               decodeEventAs[proto.MCPEvent],
	pubsub.PayloadTypePermissionRequest:      decodeEventAs[proto.PermissionRequest],
	pubsub.PayloadTypePermissionNotification: decodeEventAs[proto.PermissionNotification],
	pubsub.PayloadTypeQuestionRequest:        decodeEventAs[proto.QuestionRequest],
	pubsub.PayloadTypeQuestionNotification:   decodeEventAs[proto.QuestionNotification],
	pubsub.PayloadTypeMessage:                decodeEventAs[proto.Message],
	pubsub.PayloadTypeSession:                decodeEventAs[proto.Session],
	pubsub.PayloadTypeFile:                   decodeEventAs[proto.File],
	pubsub.PayloadTypeAgentEvent:             decodeEventAs[proto.AgentEvent],
	pubsub.PayloadTypeConfigChanged:          decodeEventAs[proto.ConfigChanged],
	pubsub.PayloadTypeSkillsEvent:            decodeEventAs[proto.SkillsEvent],
	pubsub.PayloadTypeRunComplete:            decodeEventAs[proto.RunComplete],
	pubsub.PayloadTypeUpdateAvailable:        decodeEventAs[proto.UpdateAvailable],
}

// decodeEvent decodes an SSE envelope into its typed pubsub event. It
// reports false for payload types this client does not know.
func decodeEvent(p pubsub.Payload) (any, bool) {
	decode, ok := eventDecoders[p.Type]
	if !ok {
		return nil, false
	}
	return decode(p.Payload), true
}

// decodeEventAs decodes a pubsub.Event[T]. A payload that does not decode
// still yields an event (with whatever fields did decode), as the client
// has always done.
func decodeEventAs[T any](raw json.RawMessage) any {
	var e pubsub.Event[T]
	_ = json.Unmarshal(raw, &e)
	return e
}

func sendEvent(ctx context.Context, evc chan any, ev any) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case evc <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}
