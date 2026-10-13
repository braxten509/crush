package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/oauth"
)

// SetMCPServerDisabledConfig persists the disabled flag of a single MCP
// server in the given scope's config file. It does not touch the running
// client; callers coordinate the runtime state separately. It fails when
// the server is not configured, so a typo can never create an empty
// config entry carrying only the disabled flag.
func (s *ConfigStore) SetMCPServerDisabledConfig(scope Scope, name string, disabled bool) error {
	if _, ok := s.Config().MCP[name]; !ok {
		return fmt.Errorf("mcp %q is not configured", name)
	}
	return s.update(scope, func(c *Config) map[string]any {
		m := c.MCP[name]
		m.Disabled = disabled
		c.MCP[name] = m
		return map[string]any{"mcp." + name + ".disabled": disabled}
	})
}

type MCPType string

const (
	MCPStdio MCPType = "stdio"
	MCPSSE   MCPType = "sse"
	MCPHttp  MCPType = "http"
)

type MCPConfig struct {
	Command       string            `json:"command,omitempty" jsonschema:"description=Command to execute for stdio MCP servers,example=npx"`
	Env           map[string]string `json:"env,omitempty" jsonschema:"description=Environment variables to set for the MCP server"`
	Args          []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the MCP server command"`
	Type          MCPType           `json:"type" jsonschema:"required,description=Type of MCP connection,enum=stdio,enum=sse,enum=http,default=stdio"`
	URL           string            `json:"url,omitempty" jsonschema:"description=URL for HTTP or SSE MCP servers,format=uri,example=http://localhost:3000/mcp"`
	Disabled      bool              `json:"disabled,omitempty" jsonschema:"description=Whether this MCP server is disabled,default=false"`
	DisabledTools []string          `json:"disabled_tools,omitempty" jsonschema:"description=List of tools from this MCP server to disable,example=get-library-doc"`
	EnabledTools  []string          `json:"enabled_tools,omitempty" jsonschema:"description=Allow list of tools from this MCP server,example=get-library-doc"`
	Timeout       int               `json:"timeout,omitempty" jsonschema:"description=Timeout in seconds for MCP server connections,default=10,example=30,example=60,example=120"`

	// Sessionless marks a server that does not maintain an MCP session (it
	// never issues a Mcp-Session-Id). When true, Crush omits the
	// tools/prompts/resources list-changed handlers: the go-sdk opens a
	// SEP-2575 "subscriptions/listen" stream whenever any of those handlers
	// is set, and sessionless streamable-HTTP servers (e.g. GitHub MCP)
	// answer that POST with 404 ("session not found"), which the SDK treats
	// as fatal. The cost is no live list-changed notifications from this
	// server.
	//
	// When nil, Crush auto-detects a set of known sessionless servers (see
	// IsSessionless); set it explicitly to override that detection.
	Sessionless *bool `json:"sessionless,omitempty" jsonschema:"description=Mark a sessionless MCP server (no Mcp-Session-Id) so Crush skips the subscriptions/listen stream it would otherwise reject. Leave unset to auto-detect known sessionless servers (e.g. GitHub MCP),default=false"`

	// Headers are HTTP headers for HTTP/SSE MCP servers. Values run
	// through shell expansion at MCP startup, so $VAR and $(cmd)
	// work. A header whose value resolves to the empty string (unset
	// bare $VAR under lenient nounset, $(echo), or literal "") is
	// omitted from the outgoing request rather than sent as
	// "Header:".
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers for HTTP/SSE MCP servers"`

	// OAuth enables the MCP OAuth 2.1 authorization flow for HTTP
	// transport servers. When true, the client uses dynamic client
	// registration and opens a browser for the user to authorize.
	// Tokens are persisted automatically. Only supported for type=http.
	OAuth bool `json:"oauth,omitempty" jsonschema:"description=Enable OAuth 2.1 authorization flow for this MCP server (HTTP transport only),default=false"`

	// OAuthClientID is an optional pre-registered OAuth client ID. Set
	// it for servers that do not support dynamic client registration
	// (e.g. GitHub, Slack) and instead issue client credentials when you
	// register an OAuth app. Values run through shell expansion, so
	// $VAR and $(cmd) work.
	OAuthClientID string `json:"oauth_client_id,omitempty" jsonschema:"description=Pre-registered OAuth client ID for servers without dynamic client registration"`

	// OAuthClientSecret is the optional secret paired with
	// OAuthClientID for confidential clients. Values run through shell
	// expansion, so $VAR and $(cmd) work.
	OAuthClientSecret string `json:"oauth_client_secret,omitempty" jsonschema:"description=Pre-registered OAuth client secret paired with oauth_client_id"`

	// OAuthCallbackPort pins the localhost port used for the OAuth
	// redirect listener. Set this when the OAuth provider requires an
	// exact-match callback URL (e.g. GitHub OAuth Apps). When omitted,
	// Crush picks the first free port from its default range.
	OAuthCallbackPort int `json:"oauth_callback_port,omitempty" jsonschema:"description=Fixed localhost port for the OAuth callback, required by providers that enforce exact-match redirect URIs"`

	// OAuthToken is the persisted OAuth token for this server. It is
	// managed internally and stored in the global data config.
	OAuthToken *oauth.Token `json:"oauth_token,omitempty" jsonschema:"-"`

	// ChannelEnabled enables an MCP server as a channel directly from config,
	// equivalent to passing its name via --channels on the CLI. This lets
	// channels be declared persistently in crush.json without needing a CLI
	// flag on every launch.
	ChannelEnabled bool `json:"channel_enabled,omitempty" jsonschema:"description=Enable this MCP server as a channel (equivalent to --channels),default=false"`

	// ChannelReply, when set on a server enabled as a channel, makes Crush
	// route the final assistant response of every turn that originated from
	// this channel back through one of the server's own tools, so a message
	// received on the channel gets a reply on the channel even when the
	// model only produced terminal output.
	ChannelReply *MCPChannelReply `json:"channel_reply,omitempty" jsonschema:"description=Automatically route replies for turns originating from this channel back through one of the server's tools"`
}

// MCPChannelReply configures deterministic reply routing for an MCP server
// acting as a channel: which of the server's tools deliver a reply for
// direct and group pushes, and how the reply text and target are mapped
// onto tool arguments.
type MCPChannelReply struct {
	// User routes replies to direct (person-to-person) channel pushes.
	User *MCPChannelReplyRoute `json:"user,omitempty" jsonschema:"description=Reply route for direct messages"`
	// Group routes replies to group channel pushes. It is preferred over
	// User when the push carries the group route's meta attribute.
	Group *MCPChannelReplyRoute `json:"group,omitempty" jsonschema:"description=Reply route for group messages"`
	// MessageParam is the tool argument that receives the reply text.
	MessageParam string `json:"message_param,omitempty" jsonschema:"description=Tool argument name that receives the reply text,default=message"`
	// SuppressTools lists additional tool names (beyond the two route
	// tools) that count as the model having already replied on the channel
	// during the turn, e.g. an operator-shortcut send tool.
	SuppressTools []string `json:"suppress_tools,omitempty" jsonschema:"description=Additional tool names that suppress the automatic reply when the model already called one of them during the turn,example=send"`
}

// MCPChannelReplyRoute maps one kind of inbound channel push onto the MCP
// tool call that delivers a reply to it.
type MCPChannelReplyRoute struct {
	// Tool is the MCP tool (bare name, without the mcp_<server>_ prefix)
	// invoked to deliver the reply.
	Tool string `json:"tool" jsonschema:"required,description=MCP tool name that sends the reply,example=send_message_to_user"`
	// TargetParam is the tool argument that receives the reply target
	// (recipient or group ID).
	TargetParam string `json:"target_param" jsonschema:"required,description=Tool argument name that receives the reply target,example=user_id"`
	// TargetMeta is the <channel> meta attribute whose value identifies
	// the reply target. Defaults to "sender" for the user route and
	// "group" for the group route.
	TargetMeta string `json:"target_meta,omitempty" jsonschema:"description=Channel meta attribute carrying the reply target; defaults to sender (user route) or group (group route)"`
}

// isOrphanedToken reports whether this entry is a leftover OAuth token
// with no real server config.
func (m MCPConfig) isOrphanedToken() bool {
	return m.Type == "" && m.Command == "" && m.URL == "" && m.OAuthToken != nil
}

type MCPs map[string]MCPConfig

type MCP struct {
	Name string    `json:"name"`
	MCP  MCPConfig `json:"mcp"`
}

func (m MCPs) Sorted() []MCP {
	sorted := make([]MCP, 0, len(m))
	for k, v := range m {
		sorted = append(sorted, MCP{
			Name: k,
			MCP:  v,
		})
	}
	slices.SortFunc(sorted, func(a, b MCP) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}

// ResolvedEnv returns m.Env with every value expanded through the
// given resolver. The returned slice is of the form "KEY=value" sorted
// by key so callers get deterministic output; the receiver's Env map is
// not mutated. On the first resolution failure it returns nil and an
// error that identifies the offending key; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work. Callers are expected to surface it
// (for MCP, via StateError on the status card) rather than silently
// spawn the server with an empty credential.
//
// The resolver choice matters: in server mode pass the shell resolver
// so $VAR / $(cmd) expand; in client mode pass IdentityResolver so the
// template is forwarded verbatim and expansion happens on the server.
func (m MCPConfig) ResolvedEnv(r VariableResolver) ([]string, error) {
	return resolveEnvs(m.Env, r)
}

// ResolvedArgs returns m.Args with every element expanded through the
// given resolver. A fresh slice is allocated; m.Args is never mutated.
// On the first resolution failure it returns nil and an error
// identifying the offending positional index; the inner resolver error
// is already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work.
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedArgs(r VariableResolver) ([]string, error) {
	if len(m.Args) == 0 {
		return nil, nil
	}
	out := make([]string, len(m.Args))
	for i, a := range m.Args {
		v, err := r.ResolveValue(a)
		if err != nil {
			return nil, fmt.Errorf("arg %d: %w", i, err)
		}
		out[i] = v
	}
	return out, nil
}

// ResolvedURL returns m.URL expanded through the given resolver. The
// receiver is not mutated. Errors from the resolver are already
// sanitized by ResolveValue and are wrapped with %w for errors.Is/As.
//
// URLs run through the same shell-expansion pipeline as the other
// fields, so a literal '$' (e.g. OData query strings containing
// $filter/$select) must be escaped as '\$' or '${DOLLAR:-$}' to avoid
// being interpreted as a variable reference. Same constraint already
// applies to command, args, env, and headers.
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedURL(r VariableResolver) (string, error) {
	if m.URL == "" {
		return "", nil
	}
	v, err := r.ResolveValue(m.URL)
	if err != nil {
		return "", fmt.Errorf("url: %w", err)
	}
	return v, nil
}

// knownSessionlessMCPs is the set of MCP endpoint URLs (normalized, no
// trailing slash) that are known not to maintain an MCP session — they
// never issue a Mcp-Session-Id and reject the SEP-2575
// "subscriptions/listen" stream. Add an entry when a server is confirmed to
// behave this way.
var knownSessionlessMCPs = map[string]struct{}{
	"https://api.github.com/mcp":        {},
	"https://api.githubcopilot.com/mcp": {},
}

// IsSessionless reports whether the server should be treated as sessionless.
// An explicit Sessionless value wins; when unset, the resolved URL is matched
// against knownSessionlessMCPs (trailing slash ignored). The URL is resolved
// through r so $VAR-expanded endpoints are detected too; on a resolution
// error the explicit value (or false) is used.
func (m MCPConfig) IsSessionless(r VariableResolver) bool {
	if m.Sessionless != nil {
		return *m.Sessionless
	}
	url, err := m.ResolvedURL(r)
	if err != nil {
		return false
	}
	_, ok := knownSessionlessMCPs[strings.TrimSuffix(url, "/")]
	return ok
}

// ResolvedHeaders returns m.Headers with every value expanded through
// the given resolver. A fresh map is allocated; m.Headers is never
// mutated. On the first resolution failure it returns nil and an error
// identifying the offending header name; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w so
// errors.Is/As continues to work.
//
// A header whose value resolves to the empty string (unset bare $VAR
// under lenient nounset, $(echo), or literal "") is omitted from the
// returned map — sending "X-Auth:" with an empty value is rejected by
// some providers and the user's intent in "optional, env-gated
// header" is clearly "absent when the var isn't set."
//
// See ResolvedEnv for guidance on picking a resolver.
func (m MCPConfig) ResolvedHeaders(r VariableResolver) (map[string]string, error) {
	if len(m.Headers) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(m.Headers))
	// Sort keys so failures are reported deterministically when more
	// than one header would fail.
	keys := make([]string, 0, len(m.Headers))
	for k := range m.Headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v, err := r.ResolveValue(m.Headers[k])
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

// resolveEnvs expands every value in envs through the given resolver
// and returns a fresh "KEY=value" slice sorted by key. The input map is
// not mutated. On the first resolution failure it returns nil and an
// error identifying the offending variable; the inner resolver error is
// already sanitized by ResolveValue and is wrapped with %w.
func resolveEnvs(envs map[string]string, r VariableResolver) ([]string, error) {
	if len(envs) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(envs))
	for k := range envs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	res := make([]string, 0, len(envs))
	for _, k := range keys {
		v, err := r.ResolveValue(envs[k])
		if err != nil {
			return nil, fmt.Errorf("env %s: %w", k, err)
		}
		res = append(res, fmt.Sprintf("%s=%s", k, v))
	}
	return res, nil
}
